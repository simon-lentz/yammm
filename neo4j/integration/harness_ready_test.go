//go:build neo4j_integration

package integration

import (
	"context"
	"regexp"
	"strconv"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v6/neo4j"
)

// attemptCount reads the attempt total awaitReachable reports.
var attemptCount = regexp.MustCompile(`after (\d+) attempts`)

// TestAwaitReachable_RetriesUntilTheContextEnds pins the behaviour the harness
// gained after one CI image failed the whole suite on
// Neo.TransientError.General.DatabaseUnavailable: a single VerifyConnectivity
// treats an open Bolt port as a ready database, and the window between the two
// is where that error lives.
//
// The subject is a driver against a port nothing listens on, so every attempt
// fails and the loop can only end at the deadline. A harness that did not retry
// would return on the first attempt, well inside it.
func TestAwaitReachable_RetriesUntilTheContextEnds(t *testing.T) {
	t.Parallel()

	driver, err := neo4jdriver.NewDriver("neo4j://localhost:1", neo4jdriver.BasicAuth("none", "none", ""))
	if err != nil {
		t.Fatalf("open a driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })

	const window = 4 * time.Second
	ctx, cancel := context.WithTimeout(t.Context(), window)
	defer cancel()

	start := time.Now()
	err = awaitReachable(ctx, driver)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("a port nothing listens on reported no error")
	}
	if elapsed < window/2 {
		t.Errorf("returned after %s of a %s window: it did not retry", elapsed, window)
	}

	m := attemptCount.FindStringSubmatch(err.Error())
	if m == nil {
		t.Fatalf("the error names no attempt count: %v", err)
	}
	if n, _ := strconv.Atoi(m[1]); n < 2 {
		t.Errorf("reported %d attempts, want more than one: %v", n, err)
	}
}

// TestAwaitReachable_ReportsTheLastError pins that the wait does not swallow
// what it was waiting on: the operator needs the driver's own message, not a
// timeout with no subject.
func TestAwaitReachable_ReportsTheLastError(t *testing.T) {
	t.Parallel()

	driver, err := neo4jdriver.NewDriver("neo4j://localhost:1", neo4jdriver.BasicAuth("none", "none", ""))
	if err != nil {
		t.Fatalf("open a driver: %v", err)
	}
	t.Cleanup(func() { _ = driver.Close(context.Background()) })

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	if err := awaitReachable(ctx, driver); err == nil {
		t.Fatal("a port nothing listens on reported no error")
	} else if err.Error() == "" {
		t.Error("the wait reported an empty error")
	}
}
