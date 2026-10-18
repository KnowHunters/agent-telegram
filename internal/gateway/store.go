// Package gateway stores resumable events and durable request receipts.
package gateway

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var ErrConflict = errors.New("request id already used with different parameters")
var ErrUnknown = errors.New("request outcome unknown; reconcile before sending again")

type Event struct {
	Type    string          `json:"type"`
	Cursor  int64           `json:"cursor"`
	Event   string          `json:"event"`
	Account string          `json:"account"`
	Data    json.RawMessage `json:"data"`
}

type Store struct {
	db      *sql.DB
	mu      sync.Mutex
	changed chan struct{}
	closed  bool
}

// Open limits retained events by both age and count. Receipts are kept for seven days.
func Open(path string) (*Store, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.ExecContext(ctx, `PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS events(id INTEGER PRIMARY KEY AUTOINCREMENT, account TEXT NOT NULL, kind TEXT NOT NULL, data BLOB NOT NULL, created INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS receipts(account TEXT NOT NULL, id TEXT NOT NULL, hash TEXT NOT NULL, response BLOB, created INTEGER NOT NULL, PRIMARY KEY(account,id));`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	_ = os.Chmod(path, 0600)
	return &Store{db: db, changed: make(chan struct{})}, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.changed)
	}
	return s.db.Close()
}

// Append persists before notifying readers; SQLite IDs remain stable across restarts.
func (s *Store) Append(parent context.Context, account, kind string, data any) error {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("event store closed")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, "INSERT INTO events(account,kind,data,created) VALUES(?,?,?,?)", account, kind, b, time.Now().Unix()); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM events WHERE created < ? OR id <= (SELECT COALESCE(MAX(id),0)-10000 FROM events)", time.Now().Add(-24*time.Hour).Unix()); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	close(s.changed)
	s.changed = make(chan struct{})
	return nil
}

// Read returns a global cursor and a wake channel captured under the same lock.
// Consumers advance across filtered events too, so quiet subscriptions do not stall.
func (s *Store) Read(parent context.Context, account string, after int64) ([]Event, int64, bool, <-chan struct{}, error) {
	// Finish this bounded local snapshot even if a socket disconnects. Cancelling
	// SQLite while rows are being drained can leave Windows handles outstanding.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.ExecContext(ctx, "DELETE FROM events WHERE created<?", time.Now().Add(-24*time.Hour).Unix()); err != nil {
		return nil, after, false, s.changed, err
	}
	var oldest, latest int64
	if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MIN(id),0),COALESCE(MAX(id),0) FROM events").Scan(&oldest, &latest); err != nil {
		return nil, after, false, s.changed, err
	}
	// sqlite_sequence retains the high-water mark even if all events expire.
	_ = s.db.QueryRowContext(ctx, "SELECT COALESCE(seq,0) FROM sqlite_sequence WHERE name='events'").Scan(&latest)
	gap := after > latest || (oldest > 0 && after > 0 && after < oldest-1) || (oldest == 0 && after > 0 && after < latest)
	rows, err := s.db.QueryContext(ctx, "SELECT id,account,kind,data FROM events WHERE id>? ORDER BY id LIMIT 100", after)
	if err != nil {
		return nil, after, gap, s.changed, err
	}
	defer func() { _ = rows.Close() }()
	events := []Event{}
	next := after
	for rows.Next() {
		var e Event
		e.Type = "event"
		if err = rows.Scan(&e.Cursor, &e.Account, &e.Event, &e.Data); err != nil {
			return nil, after, gap, s.changed, err
		}
		next = e.Cursor
		if e.Account == account {
			events = append(events, e)
		}
	}
	return events, next, gap, s.changed, rows.Err()
}

func (s *Store) Latest(parent context.Context) (int64, error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	var n int64
	err := s.db.QueryRowContext(ctx, "SELECT COALESCE((SELECT seq FROM sqlite_sequence WHERE name='events'),0)").Scan(&n)
	return n, err
}

// Begin reserves a request BEFORE a side effect. An incomplete receipt never retries automatically.
func (s *Store) Begin(parent context.Context, account, id string, request []byte) (json.RawMessage, bool, error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	sum := sha256.Sum256(request)
	hash := hex.EncodeToString(sum[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	var previous string
	var response []byte
	err := s.db.QueryRowContext(ctx, "SELECT hash,response FROM receipts WHERE account=? AND id=?", account, id).Scan(&previous, &response)
	if err == nil {
		if previous != hash {
			return nil, false, ErrConflict
		}
		if response == nil {
			return nil, false, ErrUnknown
		}
		return response, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	_, err = s.db.ExecContext(ctx, "DELETE FROM receipts WHERE created<?", time.Now().Add(-7*24*time.Hour).Unix())
	if err != nil {
		return nil, false, err
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO receipts(account,id,hash,created) VALUES(?,?,?,?)", account, id, hash, time.Now().Unix())
	return nil, err == nil, err
}
func (s *Store) Complete(parent context.Context, account, id string, response []byte) error {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	_, err := s.db.ExecContext(ctx, "UPDATE receipts SET response=? WHERE account=? AND id=?", response, account, id)
	if err != nil {
		return fmt.Errorf("save receipt: %w", err)
	}
	return nil
}
