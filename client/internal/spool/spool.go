package spool

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const (
	PriorityNormal = 0
	PriorityAlert  = 10
)

type Batch struct {
	ID            int64
	BatchID       string
	AgentID       string
	BootID        string
	Sequence      uint64
	CreatedAt     time.Time
	NextAttemptAt time.Time
	Attempts      uint
	Priority      int
	Payload       []byte
}

type QueueStats struct {
	Bytes      int64
	Dropped    uint64
	Capacity   int64
	Usage      float64
	AlertLevel string
}

type Store struct {
	db       *sql.DB
	maxBytes int64
	maxAge   time.Duration
	agentID  string
	bootID   string
}

func Open(stateDir string, maxBytes int64) (*Store, error) {
	return OpenWithIdentity(stateDir, maxBytes, "", newID())
}

func OpenWithIdentity(stateDir string, maxBytes int64, agentID, bootID string) (*Store, error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(stateDir, "spool.db"))
	if err != nil {
		return nil, fmt.Errorf("open sqlite spool: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS batches (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 batch_id TEXT NOT NULL UNIQUE,
 agent_id TEXT NOT NULL DEFAULT '',
 boot_id TEXT NOT NULL DEFAULT '',
 sequence INTEGER NOT NULL,
 created_at INTEGER NOT NULL,
 next_attempt_at INTEGER NOT NULL,
 attempts INTEGER NOT NULL DEFAULT 0,
 priority INTEGER NOT NULL DEFAULT 0,
 payload BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS metadata (key TEXT PRIMARY KEY, value INTEGER NOT NULL);
INSERT OR IGNORE INTO metadata(key, value) VALUES ('last_sequence', 0);
INSERT OR IGNORE INTO metadata(key, value) VALUES ('dropped_batches', 0);`); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize spool: %w", err)
	}
	// Existing local installations used the smaller schema. Add fields in place.
	if err := migrateColumns(db); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS batches_due ON batches(priority DESC, next_attempt_at, id)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create spool index: %w", err)
	}
	return &Store{db: db, maxBytes: maxBytes, maxAge: 24 * time.Hour, agentID: agentID, bootID: bootID}, nil
}

func migrateColumns(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(batches)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		columns[name] = true
	}
	add := []string{
		`ALTER TABLE batches ADD COLUMN batch_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE batches ADD COLUMN agent_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE batches ADD COLUMN boot_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE batches ADD COLUMN next_attempt_at INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE batches ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE batches ADD COLUMN priority INTEGER NOT NULL DEFAULT 0`,
	}
	names := []string{"batch_id", "agent_id", "boot_id", "next_attempt_at", "attempts", "priority"}
	for i, statement := range add {
		if !columns[names[i]] {
			if _, err := db.Exec(statement); err != nil {
				return fmt.Errorf("migrate spool column %s: %w", names[i], err)
			}
		}
	}
	_, _ = db.Exec(`UPDATE batches SET batch_id = 'legacy-' || id WHERE batch_id = ''`)
	_, _ = db.Exec(`UPDATE batches SET next_attempt_at = created_at WHERE next_attempt_at = 0`)
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) SetMaxAge(maxAge time.Duration) {
	if maxAge > 0 {
		s.maxAge = maxAge
	}
}

func (s *Store) Enqueue(sequence uint64, payload []byte) error {
	return s.EnqueueWithPriority(sequence, payload, PriorityNormal)
}

func (s *Store) EnqueueWithPriority(sequence uint64, payload []byte, priority int) error {
	if int64(len(payload)) > s.maxBytes {
		return fmt.Errorf("batch exceeds spool capacity")
	}
	now := time.Now().UTC()
	if _, err := s.db.Exec(`INSERT INTO batches
		(batch_id, agent_id, boot_id, sequence, created_at, next_attempt_at, priority, payload)
		VALUES(?,?,?,?,?,?,?,?)`, newID(), s.agentID, s.bootID, sequence, now.UnixNano(), now.UnixNano(), priority, payload); err != nil {
		return fmt.Errorf("enqueue batch: %w", err)
	}
	_, _ = s.db.Exec(`UPDATE metadata SET value = MAX(value, ?) WHERE key = 'last_sequence'`, sequence)
	return s.EnforceLimits(now)
}

func (s *Store) EnforceLimits(now time.Time) error {
	cutoff := now.Add(-s.maxAge).UnixNano()
	_, err := s.db.Exec(`DELETE FROM batches WHERE created_at < ? AND priority < ?`, cutoff, PriorityAlert)
	if err != nil {
		return err
	}
	for {
		stats, err := s.StatsDetailed()
		if err != nil {
			return err
		}
		if stats.Bytes <= s.maxBytes {
			return nil
		}
		result, err := s.db.Exec(`DELETE FROM batches WHERE id = (
			SELECT id FROM batches ORDER BY CASE WHEN priority >= ? THEN 1 ELSE 0 END, id LIMIT 1)`, PriorityAlert)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return nil
		}
		_, _ = s.db.Exec(`UPDATE metadata SET value = value + 1 WHERE key = 'dropped_batches'`)
	}
}

func (s *Store) Stats() (bytes int64, dropped uint64, err error) {
	stats, err := s.StatsDetailed()
	return stats.Bytes, stats.Dropped, err
}

func (s *Store) StatsDetailed() (QueueStats, error) {
	var stats QueueStats
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(length(payload)), 0) FROM batches`).Scan(&stats.Bytes); err != nil {
		return stats, err
	}
	if err := s.db.QueryRow(`SELECT value FROM metadata WHERE key = 'dropped_batches'`).Scan(&stats.Dropped); err != nil {
		return stats, err
	}
	stats.Capacity = s.maxBytes
	if s.maxBytes > 0 {
		stats.Usage = float64(stats.Bytes) / float64(s.maxBytes) * 100
	}
	switch {
	case stats.Usage >= 90:
		stats.AlertLevel = "critical"
	case stats.Usage >= 70:
		stats.AlertLevel = "warning"
	default:
		stats.AlertLevel = "normal"
	}
	return stats, nil
}

func (s *Store) Next() (*Batch, error) {
	return s.NextDue(time.Now().UTC())
}

func (s *Store) NextDue(now time.Time) (*Batch, error) {
	return s.nextDue(now, 0)
}

func (s *Store) NextDueAfter(now time.Time, afterID int64) (*Batch, error) {
	return s.nextDue(now, afterID)
}

func (s *Store) nextDue(now time.Time, afterID int64) (*Batch, error) {
	row := s.db.QueryRow(`SELECT id, batch_id, agent_id, boot_id, sequence, created_at,
		next_attempt_at, attempts, priority, payload
		FROM batches WHERE next_attempt_at <= ? AND id > ? ORDER BY priority DESC, id LIMIT 1`, now.UnixNano(), afterID)
	var b Batch
	var created, next int64
	if err := row.Scan(&b.ID, &b.BatchID, &b.AgentID, &b.BootID, &b.Sequence, &created, &next,
		&b.Attempts, &b.Priority, &b.Payload); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("read batch: %w", err)
	}
	b.CreatedAt = time.Unix(0, created).UTC()
	b.NextAttemptAt = time.Unix(0, next).UTC()
	return &b, nil
}

func (s *Store) Ack(id int64) error {
	_, err := s.db.Exec(`DELETE FROM batches WHERE id = ?`, id)
	return err
}

func (s *Store) Retry(id int64, base, max time.Duration) error {
	var attempts uint
	if err := s.db.QueryRow(`SELECT attempts FROM batches WHERE id = ?`, id).Scan(&attempts); err != nil {
		return err
	}
	attempts++
	delay := base
	for i := uint(1); i < attempts && delay < max; i++ {
		delay *= 2
	}
	if delay > max {
		delay = max
	}
	jitter := time.Duration(time.Now().UnixNano()%int64(delay/2+1)) - delay/4
	next := time.Now().UTC().Add(delay + jitter)
	_, err := s.db.Exec(`UPDATE batches SET attempts = ?, next_attempt_at = ? WHERE id = ?`,
		attempts, next.UnixNano(), id)
	return err
}

func (s *Store) Bytes() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COALESCE(SUM(length(payload)), 0) FROM batches`).Scan(&n)
	return n, err
}

func (s *Store) LastSequence() (uint64, error) {
	var sequence uint64
	if err := s.db.QueryRow(`SELECT value FROM metadata WHERE key = 'last_sequence'`).Scan(&sequence); err != nil {
		return 0, err
	}
	if current := uint64(time.Now().UTC().UnixNano()); sequence < current {
		sequence = current
	}
	return sequence, nil
}

func newID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw)
}
