package config

import (
	"crypto/rand"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

// placeholderAgentID reports ids that are not a stable host identity.
// ponytail: a copied yaml that still has one of these is rewritten once; a real id is never replaced.
func placeholderAgentID(id string) bool {
	switch strings.TrimSpace(id) {
	case "", "replace-with-stable-ulid", "local-windows-002", "local-linux-001":
		return true
	default:
		return false
	}
}

func ensureIdentity(path, agentID, agentVersion, configVersion string, labels map[string]string) (string, string, string, map[string]string, error) {
	if labels == nil {
		labels = map[string]string{}
	}
	patch := identityPatch{}
	if placeholderAgentID(agentID) {
		id, err := newULID()
		if err != nil {
			return "", "", "", nil, err
		}
		agentID = id
		patch.agentID = id
	}
	if strings.TrimSpace(agentVersion) == "" {
		agentVersion = "dev"
		patch.agentVersion = agentVersion
	}
	if strings.TrimSpace(configVersion) == "" {
		configVersion = "local-1"
		patch.configVersion = configVersion
	}
	if strings.TrimSpace(labels["environment"]) == "" {
		labels["environment"] = "test"
		patch.environment = labels["environment"]
	}
	if strings.TrimSpace(labels["site"]) == "" {
		labels["site"] = "local"
		patch.site = labels["site"]
	}
	if strings.TrimSpace(labels["role"]) == "" {
		labels["role"] = runtime.GOOS
		patch.role = labels["role"]
	}
	if patch.empty() {
		return agentID, agentVersion, configVersion, labels, nil
	}
	if err := patchIdentityFile(path, patch); err != nil {
		return "", "", "", nil, err
	}
	return agentID, agentVersion, configVersion, labels, nil
}

type identityPatch struct {
	agentID, agentVersion, configVersion string
	environment, site, role              string
}

func (p identityPatch) empty() bool {
	return p.agentID == "" && p.agentVersion == "" && p.configVersion == "" &&
		p.environment == "" && p.site == "" && p.role == ""
}

func patchIdentityFile(path string, patch identityPatch) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config for identity write: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	text := string(data)
	newline := "\n"
	if strings.Contains(text, "\r\n") {
		newline = "\r\n"
		text = strings.ReplaceAll(text, "\r\n", "\n")
	}
	lines := strings.Split(text, "\n")
	inLabels := false
	labelsAt := -1
	found := map[string]bool{}
	for i, line := range lines {
		key, top := yamlKey(line)
		if key == "" {
			continue
		}
		if inLabels && top {
			inLabels = false
		}
		if !inLabels && key == "labels" {
			inLabels = true
			labelsAt = i
			continue
		}
		if !inLabels {
			switch key {
			case "agent_id":
				if patch.agentID != "" {
					lines[i] = rewriteScalar(line, "agent_id", patch.agentID)
				}
				found["agent_id"] = true
			case "agent_version":
				if patch.agentVersion != "" {
					lines[i] = rewriteScalar(line, "agent_version", patch.agentVersion)
				}
				found["agent_version"] = true
			case "config_version":
				if patch.configVersion != "" {
					lines[i] = rewriteScalar(line, "config_version", patch.configVersion)
				}
				found["config_version"] = true
			}
			continue
		}
		switch key {
		case "environment":
			if patch.environment != "" {
				lines[i] = rewriteScalar(line, "environment", patch.environment)
			}
			found["environment"] = true
		case "site":
			if patch.site != "" {
				lines[i] = rewriteScalar(line, "site", patch.site)
			}
			found["site"] = true
		case "role":
			if patch.role != "" {
				lines[i] = rewriteScalar(line, "role", patch.role)
			}
			found["role"] = true
		}
	}
	lines = insertMissingIdentity(lines, labelsAt, found, patch)
	out := strings.Join(lines, newline)
	if err := os.WriteFile(path, []byte(out), info.Mode()); err != nil {
		return fmt.Errorf("write identity: %w", err)
	}
	return nil
}

func insertMissingIdentity(lines []string, labelsAt int, found map[string]bool, patch identityPatch) []string {
	if patch.agentID != "" && !found["agent_id"] {
		lines = append([]string{rewriteScalar("", "agent_id", patch.agentID)}, lines...)
		if labelsAt >= 0 {
			labelsAt++
		}
	}
	missing := []string{}
	if patch.environment != "" && !found["environment"] {
		missing = append(missing, rewriteScalar("  ", "environment", patch.environment))
	}
	if patch.site != "" && !found["site"] {
		missing = append(missing, rewriteScalar("  ", "site", patch.site))
	}
	if patch.role != "" && !found["role"] {
		missing = append(missing, rewriteScalar("  ", "role", patch.role))
	}
	if len(missing) == 0 {
		return lines
	}
	if labelsAt < 0 {
		block := append([]string{"labels:"}, missing...)
		return append(lines, block...)
	}
	insertAt := labelsAt + 1
	tail := append([]string{}, lines[insertAt:]...)
	return append(append(lines[:insertAt], missing...), tail...)
}

func yamlKey(line string) (key string, top bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	indent := len(line) - len(strings.TrimLeft(line, " \t"))
	i := strings.Index(trimmed, ":")
	if i <= 0 {
		return "", false
	}
	return strings.TrimSpace(trimmed[:i]), indent == 0
}

func rewriteScalar(line, key, value string) string {
	indent := ""
	if strings.TrimSpace(line) == "" {
		indent = line
	} else {
		indent = line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	}
	return indent + key + `: "` + yamlEscape(value) + `"`
}

func yamlEscape(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}

func newULID() (string, error) {
	var id [16]byte
	ms := uint64(time.Now().UnixMilli())
	id[0] = byte(ms >> 40)
	id[1] = byte(ms >> 32)
	id[2] = byte(ms >> 24)
	id[3] = byte(ms >> 16)
	id[4] = byte(ms >> 8)
	id[5] = byte(ms)
	if _, err := rand.Read(id[6:]); err != nil {
		return "", fmt.Errorf("agent id: %w", err)
	}
	return encodeULID(id), nil
}

func encodeULID(id [16]byte) string {
	const enc = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	return string([]byte{
		enc[(id[0]&224)>>5],
		enc[id[0]&31],
		enc[(id[1]&248)>>3],
		enc[((id[1]&7)<<2)|((id[2]&192)>>6)],
		enc[(id[2]&62)>>1],
		enc[((id[2]&1)<<4)|((id[3]&240)>>4)],
		enc[((id[3]&15)<<1)|((id[4]&128)>>7)],
		enc[(id[4]&124)>>2],
		enc[((id[4]&3)<<3)|((id[5]&224)>>5)],
		enc[id[5]&31],
		enc[(id[6]&248)>>3],
		enc[((id[6]&7)<<2)|((id[7]&192)>>6)],
		enc[(id[7]&62)>>1],
		enc[((id[7]&1)<<4)|((id[8]&240)>>4)],
		enc[((id[8]&15)<<1)|((id[9]&128)>>7)],
		enc[(id[9]&124)>>2],
		enc[((id[9]&3)<<3)|((id[10]&224)>>5)],
		enc[id[10]&31],
		enc[(id[11]&248)>>3],
		enc[((id[11]&7)<<2)|((id[12]&192)>>6)],
		enc[(id[12]&62)>>1],
		enc[((id[12]&1)<<4)|((id[13]&240)>>4)],
		enc[((id[13]&15)<<1)|((id[14]&128)>>7)],
		enc[(id[14]&124)>>2],
		enc[((id[14]&3)<<3)|((id[15]&224)>>5)],
		enc[id[15]&31],
	})
}
