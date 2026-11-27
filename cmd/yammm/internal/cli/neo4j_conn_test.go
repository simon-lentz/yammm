package cli

import (
	"context"
	"net"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
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

// refusedBeforeAnySocket reports whether a dial of the address the driver
// would dial for uri fails before net creates a socket, asked of the driver
// and of net and not of this package. A bolt+unix target dials its path. The
// hosts are IP literals, so no name is looked up.
func refusedBeforeAnySocket(t *testing.T, uri string) bool {
	t.Helper()
	driver, err := neo4j.NewDriver(uri, neo4j.BasicAuth("a", "b", ""))
	if err != nil {
		return true
	}
	defer driver.Close(t.Context())
	target := driver.Target()
	network, address := "tcp", target.Host
	if target.Scheme == "bolt+unix" {
		network, address = "unix", target.Path
	}
	sockets := 0
	dialer := net.Dialer{Timeout: 5 * time.Second, Control: func(string, string, syscall.RawConn) error {
		sockets++
		return nil
	}}
	conn, err := dialer.DialContext(t.Context(), network, address)
	if err == nil {
		_ = conn.Close()
		return false
	}
	return sockets == 0
}

// TestNewNeo4jDriver_JudgesTheURIOnItsText holds the usage class of a --uri to
// one question: can its text name a server. Each row's answer is written here
// and then asked of the driver and of net, so a URI the driver accepts and no
// dial can use is a usage error as one the driver refuses is, and a URI
// nothing listens at is not.
func TestNewNeo4jDriver_JudgesTheURIOnItsText(t *testing.T) {
	t.Parallel()
	closed := closedPort(t)
	_, closedPortNumber, err := net.SplitHostPort(closed)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		uri   string
		usage bool
		says  string // what the refusal states after the URI, "" when the driver words it
	}{
		{"no host", "bolt://", true, "the URI names no host"},
		{"no host, routing scheme", "neo4j://", true, "the URI names no host"},
		{"no host, a path", "bolt:///db", true, "the URI names no host"},
		{"an empty port, which the driver joins to its default", "bolt://127.0.0.1:", true, "the URI's address"},
		{"a port above the range", "bolt://127.0.0.1:99999", true, "the URI's port"},
		{"a port above the range, IPv6", "bolt://[::1]:65536", true, "the URI's port"},
		{"a negative port", "bolt://127.0.0.1:-1", true, ""},
		{"a port that is no number and no service", "bolt://127.0.0.1:notaport", true, ""},
		{"an unsupported scheme", "bogus://127.0.0.1", true, ""},
		{"no scheme", "127.0.0.1:7687", true, ""},
		{"a routing context on a direct scheme", "bolt://127.0.0.1:1?x=y", true, ""},
		{"a space in the host", "bolt:// x", true, ""},
		{"an IPv6 literal without brackets", "bolt://::1", true, ""},
		{"no socket path", "bolt+unix://", true, "the URI names no socket path"},
		{"a socket path nothing listens at", "bolt+unix:///nonexistent/neo4j.sock", false, ""},
		{"nothing listening", "bolt://" + closed, false, ""},
		{"nothing listening, routing scheme", "neo4j://" + closed, false, ""},
		{"an empty host before a port, which dials this host", "bolt://:" + closedPortNumber, false, ""},
		{"the highest port", "bolt://127.0.0.1:65535", false, ""},
		{"the default port", "bolt://127.0.0.1", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if oracle := refusedBeforeAnySocket(t, tt.uri); oracle != tt.usage {
				t.Fatalf("the driver and net refuse %q before any socket: %v, and the row says %v", tt.uri, oracle, tt.usage)
			}
			driver, err := NewNeo4jDriver(t.Context(), tt.uri, "a", "b")
			if driver != nil {
				defer driver.Close(t.Context())
			}
			assertURIVerdict(t, tt.uri, driver, err, tt.usage)
			if err != nil && !strings.Contains(err.Error(), tt.says) {
				t.Errorf("NewNeo4jDriver(%q): error %q does not state %q", tt.uri, err, tt.says)
			}
		})
	}
}

// assertURIVerdict checks what [NewNeo4jDriver] answered for uri against the
// verdict usage: a refusal hands back no driver, exits 2 and names the URI.
func assertURIVerdict(t *testing.T, uri string, driver neo4j.Driver, err error, usage bool) {
	t.Helper()
	if (err != nil) != usage {
		t.Fatalf("NewNeo4jDriver(%q) error = %v, want a refusal: %v", uri, err, usage)
	}
	if err == nil {
		return
	}
	if driver != nil {
		t.Errorf("NewNeo4jDriver(%q) refused the URI and handed back a driver", uri)
	}
	if got := ExitForError(err); got != ExitUsage {
		t.Errorf("NewNeo4jDriver(%q): exit = %d, want %d", uri, got, ExitUsage)
	}
	if want := "connect to neo4j at " + strconv.Quote(uri); !strings.Contains(err.Error(), want) {
		t.Errorf("NewNeo4jDriver(%q): error %q does not contain %q", uri, err, want)
	}
}

// TestVerifyNeo4j_RefusesAnUnreachableServer pins the connectivity check: a
// driver for a server that is not listening is a runtime failure that names
// the check.
func TestVerifyNeo4j_RefusesAnUnreachableServer(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	uri := "neo4j://" + closedPort(t)
	driver, err := NewNeo4jDriver(ctx, uri, "neo4j", "password")
	if err != nil {
		t.Fatalf("NewNeo4jDriver(%q) = %v, want a driver", uri, err)
	}
	defer driver.Close(ctx)

	err = VerifyNeo4j(ctx, driver, uri)
	if got := ExitForError(err); got != ExitRuntime {
		t.Fatalf("VerifyNeo4j: exit = %d (%v), want %d", got, err, ExitRuntime)
	}
	if want := "verify neo4j connectivity at " + strconv.Quote(uri); !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not contain %q", err, want)
	}
}

// The diff reads the database it was asked for and can write nothing there.
func TestReadSession_ReadsTheNamedDatabaseOnly(t *testing.T) {
	t.Parallel()
	got := readSession("analytics")
	if got.DatabaseName != "analytics" || got.AccessMode != neo4j.AccessModeRead {
		t.Errorf("session config = %+v, want database %q in read mode", got, "analytics")
	}
}
