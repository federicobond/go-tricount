package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// A cancelled context must end a command that is waiting on the API, which
// is what Ctrl-C does in main.
func TestCancelledContextEndsAWaitingCommand(t *testing.T) {
	a, _, stderr := newTestApp(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	done := make(chan int, 1)
	go func() { done <- a.run(ctx, []string{"list"}) }()
	select {
	case code := <-done:
		if code == 0 {
			t.Error("exit code = 0, want non-zero after cancellation")
		}
		if !strings.Contains(stderr.String(), "context canceled") {
			t.Errorf("stderr should say why: %q", stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the command did not return after its context was cancelled")
	}
}
