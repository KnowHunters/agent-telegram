package gateway

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestJournalResumeIsolationRetentionAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Append(context.Background(), "alice", "new_message", map[string]any{"text": "one"}); err != nil {
		t.Fatal(err)
	}
	if err = s.Append(context.Background(), "bob", "new_message", map[string]any{"text": "private"}); err != nil {
		t.Fatal(err)
	}
	events, next, gap, changed, err := s.Read(context.Background(), "alice", 0)
	if err != nil || gap || next != 2 || len(events) != 1 {
		t.Fatalf("read: %v %d %v %v", events, next, gap, err)
	}
	if err = s.Append(context.Background(), "alice", "edit_message", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Fatal("reader not notified")
	}
	_ = s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	events, next, gap, _, err = s.Read(context.Background(), "alice", 2)
	if err != nil || gap || next != 3 || len(events) != 1 {
		t.Fatalf("resume: %v %d %v %v", events, next, gap, err)
	}
	if _, err = s.db.ExecContext(context.Background(), "UPDATE events SET created=0"); err != nil {
		t.Fatal(err)
	}
	if err = s.Append(context.Background(), "alice", "new_message", nil); err != nil {
		t.Fatal(err)
	}
	_, _, gap, _, err = s.Read(context.Background(), "alice", 1)
	if err != nil || !gap {
		t.Fatalf("expired cursor must signal gap: %v %v", gap, err)
	}
	_, _, gap, _, _ = s.Read(context.Background(), "alice", 100)
	if !gap {
		t.Fatal("future cursor must signal gap")
	}
}

func TestReceiptSurvivesCrashAndRejectsChangedPayload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, fresh, err := s.Begin(context.Background(), "alice", "request", []byte("payload"))
	if err != nil || !fresh {
		t.Fatal(err)
	}
	_ = s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if _, _, err = s.Begin(context.Background(), "alice", "request", []byte("payload")); !errors.Is(err, ErrUnknown) {
		t.Fatalf("incomplete receipt: %v", err)
	}
	if err = s.Complete(context.Background(), "alice", "request", []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	cached, fresh, err := s.Begin(context.Background(), "alice", "request", []byte("payload"))
	if err != nil || fresh || string(cached) != `{"ok":true}` {
		t.Fatalf("cached: %s %v %v", cached, fresh, err)
	}
	if _, _, err = s.Begin(context.Background(), "alice", "request", []byte("changed")); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, fresh, err = s.Begin(context.Background(), "bob", "request", []byte("payload")); err != nil || !fresh {
		t.Fatalf("account isolation: %v", err)
	}
}
