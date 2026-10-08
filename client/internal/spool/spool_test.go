package spool

import (
	"testing"
	"time"
)

func TestQueueTracksIdentityAndRetry(t *testing.T) {
	store, err := OpenWithIdentity(t.TempDir(), 1024, "agent-1", "boot-1")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.EnqueueWithPriority(1, []byte("cpu value=1i"), PriorityNormal); err != nil {
		t.Fatal(err)
	}
	batch, err := store.Next()
	if err != nil || batch == nil {
		t.Fatalf("batch = %#v, err = %v", batch, err)
	}
	if batch.AgentID != "agent-1" || batch.BootID != "boot-1" || batch.BatchID == "" {
		t.Fatalf("identity = %#v", batch)
	}
	if err := store.Retry(batch.ID, 10*time.Millisecond, time.Second); err != nil {
		t.Fatal(err)
	}
	retried, err := store.NextDue(time.Now().Add(20 * time.Millisecond))
	if err != nil || retried == nil || retried.Attempts != 1 {
		t.Fatalf("retried = %#v, err = %v", retried, err)
	}
}

func TestQueueDropsNormalBeforeAlert(t *testing.T) {
	store, err := OpenWithIdentity(t.TempDir(), 20, "agent-1", "boot-1")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.EnqueueWithPriority(1, []byte("1234567890"), PriorityNormal); err != nil {
		t.Fatal(err)
	}
	if err := store.EnqueueWithPriority(2, []byte("abcdefghij"), PriorityAlert); err != nil {
		t.Fatal(err)
	}
	if err := store.EnqueueWithPriority(3, []byte("normal-more"), PriorityNormal); err != nil {
		t.Fatal(err)
	}
	batch, err := store.Next()
	if err != nil || batch == nil || batch.Priority != PriorityAlert {
		t.Fatalf("batch = %#v, err = %v", batch, err)
	}
}
