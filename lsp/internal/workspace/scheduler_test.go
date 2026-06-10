package workspace

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/simon-lentz/yammm/lsp/internal/testutil"
)

// TestSchedule_StaleCallbackPreservesNewerEntry drives the real
// [analysisScheduler.schedule] through the race its entry-pointer-identity
// cleanup exists for, under synctest's fake clock:
//
//  1. schedule(uri) — the debounce timer fires and the analysis function
//     starts running (blocked, simulating slow analysis)
//  2. schedule(uri) again while the first analysis is in flight — a newer
//     entry replaces the first in the debounce map
//  3. the first analysis completes — its cleanup must NOT delete the
//     newer entry (pointer identity), and the newer entry's own
//     completion must delete itself
func TestSchedule_StaleCallbackPreservesNewerEntry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ws := NewWorkspace(testutil.DiscardLogger(), Config{})
		defer ws.Shutdown()

		uri := "file:///test.yammm"
		started := make(chan struct{})
		release := make(chan struct{})
		var completed atomic.Int32

		// First schedule: an analysis that blocks until released.
		ws.sched.schedule(uri, func(_ context.Context, _ string) {
			close(started)
			<-release
			completed.Add(1)
		})

		// Fake time: the debounce elapses and the analysis goroutine starts.
		time.Sleep(DefaultDebounceDelay)
		<-started

		// Reschedule while the first analysis is in flight: a newer entry
		// replaces the first one in the map.
		ws.sched.schedule(uri, func(_ context.Context, _ string) {
			completed.Add(1)
		})
		ws.sched.debounceMu.Lock()
		newer := ws.sched.debounces[uri]
		ws.sched.debounceMu.Unlock()
		if newer == nil {
			t.Fatal("reschedule should have installed a pending entry")
		}

		// Let the first analysis finish; its cleanup runs the pointer
		// identity check against the map.
		close(release)
		synctest.Wait()

		ws.sched.debounceMu.Lock()
		current := ws.sched.debounces[uri]
		ws.sched.debounceMu.Unlock()
		if current != newer {
			t.Fatalf("stale callback cleanup removed the newer entry: got %p, want %p", current, newer)
		}

		// The newer entry's own debounce elapses, runs, and cleans up after
		// itself — now the identity check matches.
		time.Sleep(DefaultDebounceDelay)
		synctest.Wait()

		ws.sched.debounceMu.Lock()
		_, pending := ws.sched.debounces[uri]
		ws.sched.debounceMu.Unlock()
		if pending {
			t.Error("completed entry should have removed itself from the debounce map")
		}
		if got := completed.Load(); got != 2 {
			t.Errorf("completed analyses = %d, want 2", got)
		}
	})
}

// TestSchedule_DebounceCoalescesBeforeTimerFires pins the other half of the
// debounce contract: rescheduling before the timer fires cancels the first
// analysis entirely — only the latest scheduled function runs.
func TestSchedule_DebounceCoalescesBeforeTimerFires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ws := NewWorkspace(testutil.DiscardLogger(), Config{})
		defer ws.Shutdown()

		uri := "file:///test.yammm"
		var first, second atomic.Int32

		ws.sched.schedule(uri, func(_ context.Context, _ string) { first.Add(1) })
		// Reschedule before the debounce elapses.
		time.Sleep(DefaultDebounceDelay / 2)
		ws.sched.schedule(uri, func(_ context.Context, _ string) { second.Add(1) })

		time.Sleep(DefaultDebounceDelay)
		synctest.Wait()

		if got := first.Load(); got != 0 {
			t.Errorf("superseded analysis ran %d times, want 0", got)
		}
		if got := second.Load(); got != 1 {
			t.Errorf("latest analysis ran %d times, want 1", got)
		}
	})
}
