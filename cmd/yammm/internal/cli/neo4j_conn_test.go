package cli

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// closedPort returns an address nothing is listening on, by binding one and
// releasing it. A hard-coded port could be in use and make the test lie.
func closedPort(t *testing.T) string {
	t.Helper()
	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release the port: %v", err)
	}
	return addr
}

// TestConnectNeo4j_RefusesAnUnreachableServer pins B60. The driver is created
// lazily, so without the VerifyConnectivity guard ConnectNeo4j returns a driver
// and a nil error for a server that is not there — and the failure surfaces
// later, inside the first query, as something other than a connection problem.
func TestConnectNeo4j_RefusesAnUnreachableServer(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	driver, err := ConnectNeo4j(ctx, "neo4j://"+closedPort(t), "neo4j", "password")
	if err == nil {
		if driver != nil {
			_ = driver.Close(ctx)
		}
		t.Fatal("ConnectNeo4j returned no error for a server that is not listening")
	}
	if driver != nil {
		t.Error("a failed connection must not hand back a driver")
	}
	if !strings.Contains(err.Error(), "verify neo4j connectivity") {
		t.Errorf("error %q does not name the check that failed", err)
	}
}

// TestConnectNeo4j_RejectsAMalformedURI covers the other failure arm, so the
// two are told apart by their messages rather than by both being "an error".
func TestConnectNeo4j_RejectsAMalformedURI(t *testing.T) {
	t.Parallel()

	driver, err := ConnectNeo4j(context.Background(), "not-a-scheme://x", "neo4j", "password")
	if err == nil {
		if driver != nil {
			_ = driver.Close(context.Background())
		}
		t.Fatal("ConnectNeo4j accepted a URI with no usable scheme")
	}
	if !strings.Contains(err.Error(), "connect to neo4j") {
		t.Errorf("error %q does not name the construction step", err)
	}
}
