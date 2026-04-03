package workspace

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/simon-lentz/yammm/lsp/internal/analysis"
)

// DefaultDebounceDelay is the default delay before triggering analysis after a change.
const DefaultDebounceDelay = 150 * time.Millisecond

// maxConcurrentAnalysis bounds the number of concurrent analysis goroutines.
// In the rdata workspace, editing a foundation schema can trigger 7+ dependent
// re-analyses. This semaphore prevents unbounded goroutine growth.
const maxConcurrentAnalysis = 4

// debounceEntry tracks a pending analysis for a single document.
// Using a struct with pointer identity allows callbacks to safely clean up
// only their own entries, avoiding the race where a stale callback deletes
// a newer entry that was scheduled while analysis was running.
type debounceEntry struct {
	timer  *time.Timer
	cancel context.CancelFunc
}

// analysisScheduler manages debounced analysis scheduling and background context.
//
// debounceMu protects the debounces map. It has its own lock (not Workspace.mu)
// because debounce operations must never be called while holding Workspace.mu
// (lock ordering: debounceMu must never be acquired while holding mu).
//
// bgCtx/bgCancel and analyzer are set once at construction and are immutable.
type analysisScheduler struct {
	debounceMu sync.Mutex
	debounces  map[string]*debounceEntry

	// bgCtx is the workspace-lifetime context for analysis goroutines.
	// Cancelled during shutdown to abort in-flight analysis.
	bgCtx    context.Context //nolint:containedctx // workspace-lifetime context, not request-scoped
	bgCancel context.CancelFunc

	// analyzer for schema loading
	analyzer *analysis.Analyzer

	// logger for shutdown warnings
	logger *slog.Logger

	// debounceDelay controls the debounce timer duration.
	// Defaults to DefaultDebounceDelay; overridden in tests via SetDebounceDelayForTest.
	debounceDelay time.Duration

	// sem bounds concurrent analysis goroutines.
	sem chan struct{}

	// wg tracks in-flight analysis goroutines for clean shutdown.
	wg sync.WaitGroup

	// stoppingMu guards the stopping flag. shutdown() takes the write lock;
	// goAttach() takes a read lock. This closes the TOCTOU gap between
	// checking stopping and calling wg.Add(1) (via wg.Go).
	stoppingMu sync.RWMutex
	stopping   bool

	// doneCond is signaled after each analysis completes.
	// Used by WaitForAnalysis to block until a specific URI's analysis finishes.
	doneCond *sync.Cond
	doneMu   sync.Mutex
}

// newAnalysisScheduler creates an analysisScheduler with all fields initialized.
func newAnalysisScheduler(logger *slog.Logger, bgCtx context.Context, bgCancel context.CancelFunc) *analysisScheduler {
	s := &analysisScheduler{
		debounces:     make(map[string]*debounceEntry),
		bgCtx:         bgCtx,
		bgCancel:      bgCancel,
		analyzer:      analysis.NewAnalyzer(logger),
		logger:        logger,
		debounceDelay: DefaultDebounceDelay,
		sem:           make(chan struct{}, maxConcurrentAnalysis),
	}
	s.doneCond = sync.NewCond(&s.doneMu)
	return s
}

// goAttach launches f in a tracked goroutine, returning false if shutdown
// is in progress. The RWMutex ensures the stopping check and wg.Go(f) are
// atomic with respect to shutdown — preventing the race where shutdown()
// calls wg.Wait() while a timer callback is between the check and Add(1).
func (s *analysisScheduler) goAttach(f func()) bool {
	s.stoppingMu.RLock()
	if s.stopping {
		s.stoppingMu.RUnlock()
		return false
	}
	s.wg.Go(f)
	s.stoppingMu.RUnlock()
	return true
}

// schedule schedules a debounced analysis for the given URI.
// analyzeFn is called after the debounce delay with (analyzeCtx, uri).
// This unified method handles both .yammm and markdown analysis scheduling.
func (s *analysisScheduler) schedule(uri string, analyzeFn func(context.Context, string)) {
	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()

	// Cancel existing entry
	if existing, ok := s.debounces[uri]; ok {
		existing.timer.Stop()
		existing.cancel()
	}

	// Create new cancellation context derived from workspace background context.
	// This ensures in-flight analysis is cancelled when the workspace shuts down.
	analyzeCtx, cancel := context.WithCancel(s.bgCtx)

	// Create entry before timer so we can capture its pointer for identity check.
	entry := &debounceEntry{cancel: cancel}

	delay := s.debounceDelay
	if delay == 0 {
		delay = DefaultDebounceDelay
	}

	// Schedule new analysis via goAttach. The semaphore is acquired inside
	// the goroutine so the WaitGroup always makes progress (a goroutine
	// blocked on sem acquire can still exit on context cancellation).
	entry.timer = time.AfterFunc(delay, func() {
		select {
		case <-analyzeCtx.Done():
			return
		default:
		}

		if !s.goAttach(func() {
			// Acquire semaphore (bounded concurrency).
			select {
			case s.sem <- struct{}{}:
			case <-analyzeCtx.Done():
				return
			}
			defer func() { <-s.sem }()

			analyzeFn(analyzeCtx, uri)

			// Clean up only if this is still our entry.
			s.debounceMu.Lock()
			if s.debounces[uri] == entry {
				delete(s.debounces, uri)
			}
			s.debounceMu.Unlock()

			// Signal WaitForAnalysis waiters.
			s.doneCond.Broadcast()
		}) {
			// goAttach returned false — shutdown in progress, cancel.
			cancel()
		}
	})

	s.debounces[uri] = entry
}

// cancelPending cancels any pending analysis for a URI.
func (s *analysisScheduler) cancelPending(uri string) {
	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()

	if entry, ok := s.debounces[uri]; ok {
		entry.timer.Stop()
		entry.cancel()
		delete(s.debounces, uri)
	}
}

// shutdown performs an ordered shutdown:
//  1. Set stopping flag — refuse new goroutines via goAttach
//  2. Cancel background context — abort in-flight analysis
//  3. Stop pending debounce timers
//  4. Wait for tracked goroutines with internal timeout
//
// The 3-second timeout prevents blocking forever if a goroutine is stuck
// in pathological disk I/O. The yammm loader respects context cancellation
// at I/O checkpoints, so this is a safety net.
func (s *analysisScheduler) shutdown() {
	// 1. Signal: no new goroutines accepted by goAttach.
	s.stoppingMu.Lock()
	s.stopping = true
	s.stoppingMu.Unlock()

	// 2. Cancel in-flight analysis contexts.
	s.bgCancel()

	// 3. Cancel pending timers.
	s.debounceMu.Lock()
	for uri, entry := range s.debounces {
		entry.timer.Stop()
		entry.cancel()
		delete(s.debounces, uri)
	}
	s.debounceMu.Unlock()

	// Wake up any WaitForAnalysis waiters so they can observe shutdown.
	s.doneCond.Broadcast()

	// 4. Wait for tracked goroutines with internal timeout.
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		// all goroutines exited
	case <-time.After(3 * time.Second):
		if s.logger != nil {
			s.logger.Warn("analysis goroutines did not exit within shutdown timeout")
		}
	}
}

// backgroundContext returns the workspace-lifetime context for analysis.
// This context is cancelled during shutdown.
func (s *analysisScheduler) backgroundContext() context.Context {
	return s.bgCtx
}
