package rollout

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/blueberrycongee/wuu/internal/securefs"
	"github.com/blueberrycongee/wuu/internal/statepath"
)

// rolloutSubdir is the subdirectory under sessions/ where per-thread
// JSONL rollouts live. Kept distinct from the sqlite session DB so the
// two coexist (sqlite for index/preview/search, JSONL for the business
// event stream).
const rolloutSubdir = "rollouts"

// Path returns the JSONL rollout file path for a thread ID. The parent
// directory is NOT created here; callers (New, Rebuild) handle that.
func Path(threadID string) (string, error) {
	home, err := statepath.Home("")
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "sessions", rolloutSubdir, threadID+".jsonl"), nil
}

// Writer owns the per-thread JSONL file and a background goroutine that
// drains an item channel. Emit is non-blocking (bounded buffer + short
// timeout). FlushIfMaterialized is synchronous and MUST be called before
// tool execution so tool_call lands before the tool runs — the
// recovery-critical ordering. Close drains remaining items, syncs, and
// closes the file.
//
// The lifecycle is: New → Emit* → (FlushIfMaterialized)* → Close. Emit
// after Close returns an error. FlushIfMaterialized after Close returns
// an error. Close is idempotent.
type Writer struct {
	file    *os.File
	path    string
	items   chan RolloutItem
	flush   chan chan error
	errCh   chan error
	done    chan struct{}
	closeMu sync.Mutex
	closed  bool
}

// New opens (or creates) the per-thread JSONL file at 0o600 via
// securefs and starts the background writer goroutine. The parent
// directory is created at 0o700 (securefs.Mkdir inside securefs.OpenFile).
func New(threadID string) (*Writer, error) {
	path, err := Path(threadID)
	if err != nil {
		return nil, err
	}
	f, err := securefs.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, securefs.FileMode)
	if err != nil {
		return nil, err
	}
	w := &Writer{
		file:  f,
		path:  path,
		items: make(chan RolloutItem, 256),
		flush: make(chan chan error, 16),
		errCh: make(chan error, 16),
		done:  make(chan struct{}),
	}
	go w.loop()
	return w, nil
}

// Path returns the file path the writer owns. Useful for tests that
// want to inspect the on-disk state directly.
func (w *Writer) Path() string {
	return w.path
}

// loop is the background goroutine. Each iteration handles either an
// item or a flush request. When items is closed and drained, it syncs
// the file once more and exits.
func (w *Writer) loop() {
	defer close(w.done)
	for {
		var item RolloutItem
		var ok bool
		select {
		case item, ok = <-w.items:
		case reply := <-w.flush:
			w.drainAndSync(reply)
			continue
		}
		if !ok {
			_ = w.file.Sync()
			return
		}
		w.writeItem(item)
	}
}

// drainAndSync pulls every pending item off the items channel, then
// syncs the file and replies on ack. Used by FlushIfMaterialized and by
// the final flush before Close.
func (w *Writer) drainAndSync(ack chan<- error) {
	for {
		select {
		case item := <-w.items:
			w.writeItem(item)
		default:
			ack <- w.file.Sync()
			return
		}
	}
}

// writeItem marshals and writes one item as a single JSONL line. Errors
// are routed to errCh with non-blocking send (the channel is sized; if
// it's full the error is dropped — tests rely on FlushIfMaterialized's
// reply to surface sync errors).
func (w *Writer) writeItem(item RolloutItem) {
	data, err := json.Marshal(item)
	if err != nil {
		w.recordErr(fmt.Errorf("rollout: marshal: %w", err))
		return
	}
	data = append(data, '\n')
	if _, err := w.file.Write(data); err != nil {
		w.recordErr(fmt.Errorf("rollout: write: %w", err))
	}
}

func (w *Writer) recordErr(err error) {
	select {
	case w.errCh <- err:
	default:
	}
}

// Emit enqueues one RolloutItem for asynchronous persistence. Returns
// an error if the writer has been closed or if the items channel is
// full after a short wait. Emit holds closeMu during the channel send
// so that Close cannot close the items channel concurrently with an
// in-flight Emit (which would panic on send to closed channel).
func (w *Writer) Emit(item RolloutItem) error {
	w.closeMu.Lock()
	if w.closed {
		w.closeMu.Unlock()
		return errors.New("rollout: writer closed")
	}
	select {
	case w.items <- item:
		w.closeMu.Unlock()
		return nil
	case <-time.After(100 * time.Millisecond):
		w.closeMu.Unlock()
		return errors.New("rollout: emit timeout (channel full)")
	}
}

// FlushIfMaterialized drains all pending items and fsyncs the file.
// Must be called BEFORE tool execution so tool_call lands before the
// tool runs — the recovery-critical ordering. Mirrors Codex
// recorder.rs:1583 flush_if_materialized.
func (w *Writer) FlushIfMaterialized() error {
	reply := make(chan error, 1)
	select {
	case w.flush <- reply:
	case <-w.done:
		return errors.New("rollout: writer closed")
	case <-time.After(5 * time.Second):
		return errors.New("rollout: flush request timeout")
	}
	select {
	case err := <-reply:
		return err
	case <-time.After(5 * time.Second):
		return errors.New("rollout: flush ack timeout")
	}
}

// Close drains any remaining items, syncs, and closes the file. Safe to
// call multiple times. After Close, Emit and FlushIfMaterialized return
// errors.
func (w *Writer) Close() error {
	w.closeMu.Lock()
	if w.closed {
		w.closeMu.Unlock()
		return nil
	}
	w.closed = true
	w.closeMu.Unlock()

	// Send a final flush to drain any pending items before closing the
	// channel. Loop picks it up, drains items, syncs, replies.
	reply := make(chan error, 1)
	select {
	case w.flush <- reply:
		<-reply
	case <-time.After(5 * time.Second):
		// Loop may have already exited (shouldn't happen because items
		// is still open, but be defensive).
	}

	close(w.items)
	<-w.done
	return w.file.Close()
}
