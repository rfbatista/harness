//go:build !windows

package app

import (
	"context"
	"testing"
	"time"
)

// A critic launch is tied to the server's lifetime: stopping the server while
// one is in flight ends it and waits for it, so it never touches a closed
// store.
func TestStoppingTheServerDuringACriticLaunchEndsItWithoutAPanic(t *testing.T) {
	e, author := criticWorld(t)
	if _, err := e.callTool(author.ID, "declare_done", nil); err != nil {
		t.Fatal(err)
	}
	// The launch is in flight: its git reads and its session start take
	// longer than this returns.
	stopped := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		stopped <- e.app.Stop(ctx)
	}()
	select {
	case err := <-stopped:
		if err != nil {
			t.Errorf("stopping the server: %v", err)
		}
	case <-time.After(70 * time.Second):
		t.Fatal("the server did not stop while a critic was launching")
	}
	time.Sleep(300 * time.Millisecond) // a launch that outlived the stop would panic on the closed store here
}
