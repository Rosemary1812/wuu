package tools

import (
	"path/filepath"
	"testing"
	"time"
)

func TestFileMutationQueueSerializesSamePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan struct{})
	secondEntered := make(chan struct{})

	go func() {
		defer close(firstDone)
		_, _ = withFileMutationQueue(path, func() (struct{}, error) {
			close(firstEntered)
			<-releaseFirst
			return struct{}{}, nil
		})
	}()
	<-firstEntered
	go func() {
		_, _ = withFileMutationQueue(path, func() (struct{}, error) {
			close(secondEntered)
			return struct{}{}, nil
		})
	}()

	select {
	case <-secondEntered:
		t.Fatal("second mutation entered before the first released the file")
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseFirst)
	<-firstDone
	select {
	case <-secondEntered:
	case <-time.After(time.Second):
		t.Fatal("second mutation did not enter after the first released the file")
	}
}
