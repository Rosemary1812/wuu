package host

import "testing"

func TestSpoolReplayAndAck(t *testing.T) {
	s := newSpool()
	s.append(1, []byte("a"))
	s.append(2, []byte("b"))
	s.append(3, []byte("c"))

	entries, ok := s.replayFrom(0)
	if !ok || len(entries) != 3 {
		t.Fatalf("replayFrom(0) = %v, %v", entries, ok)
	}
	entries, ok = s.replayFrom(2)
	if !ok || len(entries) != 1 || entries[0].seq != 3 {
		t.Fatalf("replayFrom(2) = %v, %v", entries, ok)
	}

	s.ackTo(2)
	if _, ok := s.replayFrom(0); ok {
		t.Fatalf("replay before acked base should fail")
	}
	entries, ok = s.replayFrom(2)
	if !ok || len(entries) != 1 {
		t.Fatalf("replayFrom(2) after ack = %v, %v", entries, ok)
	}
	// Acking everything leaves an empty but resumable spool.
	s.ackTo(3)
	entries, ok = s.replayFrom(3)
	if !ok || len(entries) != 0 {
		t.Fatalf("replayFrom(3) after full ack = %v, %v", entries, ok)
	}
}

func TestSpoolOverflowBreaks(t *testing.T) {
	s := newSpool()
	s.maxEntries = 4
	for i := uint64(1); i <= 5; i++ {
		s.append(i, []byte("x"))
	}
	if !s.broken {
		t.Fatalf("spool should be broken after overflow")
	}
	if _, ok := s.replayFrom(0); ok {
		t.Fatalf("broken spool must refuse replay")
	}
	if got := len(s.entries); got != 0 {
		t.Fatalf("broken spool retained %d entries", got)
	}
}
