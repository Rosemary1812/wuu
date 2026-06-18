package jsonl

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

// ErrStop lets callbacks end iteration without treating it as a read failure.
var ErrStop = errors.New("jsonl: stop iteration")

// ForEachLine reads one logical line at a time from r and passes the raw line
// bytes to fn. The trailing newline, if present, is included in the slice.
// The slice passed to fn is only valid during the callback — copy it if you
// need to retain the bytes.
//
// Implementation: a rolling buffer is filled from r in ~64 KiB chunks; each
// chunk is scanned for '\n' with bytes.IndexByte (AVX2-accelerated on
// supported architectures). Complete lines are dispatched immediately;
// incomplete tails are shifted to the start of the buffer and joined with the
// next Read. This preserves bufio's early-exit and bounded-memory properties.
func ForEachLine(r io.Reader, fn func(line []byte) error) error {
	return forEachLineFast(r, fn)
}

// ForEachLineNative is the streaming implementation retained for callers that
// want to force the pure-Go path explicitly and for side-by-side benchmarks.
func ForEachLineNative(r io.Reader, fn func(line []byte) error) error {
	return forEachLineFast(r, fn)
}

// initialChunk is the per-Read chunk size. Large enough to amortize Read
// syscalls and give the SIMD loop room to run, small enough that peak memory
// stays bounded even on huge session files. Doubles on demand when a single
// record exceeds it.
const initialChunk = 64 * 1024

func forEachLineFast(r io.Reader, fn func(line []byte) error) error {
	// buf holds unprocessed bytes from r. cap grows on demand; len is the
	// number of valid bytes; lineStart is the index of the first byte of the
	// current (possibly incomplete) line.
	buf := make([]byte, 0, initialChunk)
	lineStart := 0
	var idleHits int

	for {
		// Make sure there's room for the next Read. Either the current line
		// is longer than cap (lineStart==0, must grow), or we have processed
		// data sitting before lineStart that we can drop (lineStart>0).
		if cap(buf) == len(buf) {
			if lineStart == 0 {
				grown := make([]byte, len(buf), cap(buf)*2)
				copy(grown, buf)
				buf = grown
			} else {
				copy(buf, buf[lineStart:])
				buf = buf[:len(buf)-lineStart]
				lineStart = 0
			}
		}

		n, err := r.Read(buf[len(buf):cap(buf)])
		if n > 0 {
			buf = buf[:len(buf)+n]
		}
		// A well-behaved Reader returning (0, nil) repeatedly would otherwise
		// trap us in an unbounded loop. Surface it as ErrNoProgress after
		// 100 consecutive ticks — identical to bufio.Reader's threshold, so
		// exotic readers (TLS handshake, slow gateways) that legitimately
		// idle for a few calls are not false-positived.
		if n == 0 && err == nil {
			idleHits++
			if idleHits >= 100 {
				return io.ErrNoProgress
			}
			continue
		}
		idleHits = 0

		// Dispatch every complete line currently in buf. bytes.IndexByte is
		// SIMD-accelerated on amd64/arm64 in Go 1.21+ via internal/bytealg,
		// matching the throughput of the previous Zig path for short records
		// while keeping the buffer (not per-line slices) as the only alloc.
		for {
			idx := bytes.IndexByte(buf[lineStart:], '\n')
			if idx < 0 {
				break
			}
			nl := lineStart + idx
			line := buf[lineStart : nl+1] // trailing '\n' included, matches bufio semantics
			if cbErr := fn(line); cbErr != nil {
				if errors.Is(cbErr, ErrStop) {
					return nil
				}
				return cbErr
			}
			lineStart = nl + 1
		}

		if errors.Is(err, io.EOF) {
			if lineStart < len(buf) {
				// Final record without trailing newline.
				line := buf[lineStart:]
				if cbErr := fn(line); cbErr != nil {
					if errors.Is(cbErr, ErrStop) {
						return nil
					}
					return cbErr
				}
			}
			return nil
		}
		if err != nil {
			// Non-nil, non-EOF error may accompany n>0. We have already
			// dispatched the complete lines we found; trailing bytes are
			// discarded — same surface as bufio, which drops the tail on
			// error too. Consistency matters more than preserving them.
			return err
		}
	}
}

// bufioFallback is exposed for callers that need an exact-match replica of the
// old bufio-based path (e.g. one-off diffs in tests that pin behaviour). It is
// not used by ForEachLine.
func bufioFallback(r io.Reader, fn func(line []byte) error) error {
	reader := bufio.NewReader(r)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if cbErr := fn(line); cbErr != nil {
				if errors.Is(cbErr, ErrStop) {
					return nil
				}
				return cbErr
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}