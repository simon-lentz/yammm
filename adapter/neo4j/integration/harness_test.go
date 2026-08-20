//go:build neo4j_integration

package integration

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v6/neo4j"
	"github.com/testcontainers/testcontainers-go"
	tcneo4j "github.com/testcontainers/testcontainers-go/modules/neo4j"
)

// defaultImage is a current calendar-versioned release, comfortably past the
// 5.15 floor the emitted CREATE VECTOR INDEX ... OPTIONS form requires. Pinned
// rather than floating on `latest` so a suite failure is attributable to a code
// change and not to whatever the registry served that morning; the pin is
// expected to be bumped deliberately.
//
// Enterprise, because Enterprise is what the adapter targets by default:
// NOT NULL, TYPE, and NODE KEY constraints are Enterprise-only, and so is the
// propertyType column the constraint diff compares and its Unverified bucket
// turns on. On Community the adapter's edition gating emits UNIQUE alone, so
// three of its four constraint kinds and an entire diff classification would
// never execute. The container accepts the evaluation licence below.
const defaultImage = "neo4j:2026.05.0-enterprise"

const adminPassword = "yammm-integration"

// shared is the one container every test in this package uses. Starting Neo4j
// costs tens of seconds, and nothing here mutates state another test reads —
// each test namespaces its own labels — so one container is started in TestMain
// and torn down after.
var shared struct {
	driver  neo4jdriver.Driver
	uri     string // the container's Bolt URL, for a test that needs its own pool
	skip    string // non-empty when the suite cannot run
	cleanup func()
}

func TestMain(m *testing.M) {
	code := func() int {
		defer func() {
			if shared.cleanup != nil {
				shared.cleanup()
			}
		}()
		start()
		return m.Run()
	}()
	os.Exit(code)
}

func start() {
	image := defaultImage
	if custom := os.Getenv("YAMMM_NEO4J_TEST_IMAGE"); custom != "" {
		image = custom
	}

	// Generous: the first run pulls the image, which dominates.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)

	// The evaluation licence is accepted unconditionally: it is what the default
	// Enterprise image requires to start, and it is inert for a Community image.
	opts := []testcontainers.ContainerCustomizer{
		tcneo4j.WithAdminPassword(adminPassword),
		tcneo4j.WithAcceptEvaluationLicenseAgreement(),
	}

	container, err := tcneo4j.Run(ctx, image, opts...)
	if err != nil {
		cancel()
		// A missing or stopped Docker is an environment fact, not a test
		// failure: the tag already says the operator asked for these tests, and
		// failing here would only tell them something they can see for
		// themselves.
		shared.skip = fmt.Sprintf("cannot start %s (is Docker running?): %v", image, err)
		return
	}

	// Registered the moment the container exists, so every failure below tears it
	// down. A ~1-2 GB Neo4j left running with no owner survives the test binary,
	// and the next run starts another; the only recovery is a manual docker rm.
	var closeDriver func(context.Context)
	shared.cleanup = func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Minute)
		defer closeCancel()
		if closeDriver != nil {
			closeDriver(closeCtx)
		}
		_ = testcontainers.TerminateContainer(container, testcontainers.StopContext(closeCtx))
		cancel()
	}

	uri, err := container.BoltUrl(ctx)
	if err != nil {
		shared.skip = fmt.Sprintf("cannot resolve the container's Bolt URL: %v", err)
		return
	}

	driver, err := neo4jdriver.NewDriver(uri, neo4jdriver.BasicAuth("neo4j", adminPassword, ""))
	if err != nil {
		shared.skip = fmt.Sprintf("cannot open a driver against %s: %v", uri, err)
		return
	}
	closeDriver = func(closeCtx context.Context) { _ = driver.Close(closeCtx) }

	if err := driver.VerifyConnectivity(ctx); err != nil {
		shared.skip = fmt.Sprintf("cannot reach %s: %v", uri, err)
		return
	}

	shared.driver = driver
	shared.uri = uri
}

// isolatedDriver returns a driver with its own connection pool, closed when the
// test ends.
//
// A query that the server rejects leaves its connection in the Bolt "failed"
// state, which needs a RESET before the connection serves another query. The
// shared driver hands that connection to whatever runs next: on some server
// versions the next query recovers, and on others it fails with
// "invalid state 4, expected: [0]". A test that provokes a server error
// therefore takes its own pool and throws it away, rather than leaving the
// recovery to chance and to test order.
func isolatedDriver(t *testing.T) neo4jdriver.Driver {
	t.Helper()
	if shared.skip != "" {
		t.Skip(shared.skip)
	}
	d, err := neo4jdriver.NewDriver(shared.uri, neo4jdriver.BasicAuth("neo4j", adminPassword, ""))
	if err != nil {
		t.Fatalf("opening an isolated driver against %s: %v", shared.uri, err)
	}
	t.Cleanup(func() { _ = d.Close(context.Background()) })
	return d
}

// driver returns the shared driver, skipping the test when the container could
// not be started.
func driver(t *testing.T) neo4jdriver.Driver {
	t.Helper()
	if shared.skip != "" {
		t.Skip(shared.skip)
	}
	return shared.driver
}

// run executes one statement and discards its result. Used for DDL.
func run(t *testing.T, ctx context.Context, cypher string) {
	t.Helper()
	if _, err := neo4jdriver.ExecuteQuery(ctx, driver(t), cypher, nil,
		neo4jdriver.EagerResultTransformer); err != nil {
		t.Fatalf("executing %q: %v", cypher, err)
	}
}

// query executes one statement and returns its records as the plain maps
// [github.com/simon-lentz/yammm/adapter/neo4j.ParseRemoteIndexes] and its
// siblings consume. This is the seam the whole package exists to test: the maps
// are whatever the driver actually produces, never hand-written.
func query(t *testing.T, ctx context.Context, cypher string) []map[string]any {
	t.Helper()
	result, err := neo4jdriver.ExecuteQuery(ctx, driver(t), cypher, nil,
		neo4jdriver.EagerResultTransformer)
	if err != nil {
		t.Fatalf("executing %q: %v", cypher, err)
	}
	records := make([]map[string]any, 0, len(result.Records))
	for _, rec := range result.Records {
		records = append(records, rec.AsMap())
	}
	return records
}

// dropAll removes every constraint and index in the database, so each test
// starts from a known state whatever ran before it.
func dropAll(t *testing.T, ctx context.Context) {
	t.Helper()
	for _, show := range []string{"SHOW CONSTRAINTS YIELD name", "SHOW INDEXES YIELD name, type"} {
		for _, rec := range query(t, ctx, show) {
			name, _ := rec["name"].(string)
			if name == "" {
				continue
			}
			// LOOKUP indexes are built in and cannot be dropped by name.
			if typ, _ := rec["type"].(string); typ == "LOOKUP" {
				continue
			}
			kind := "INDEX"
			if strings.HasPrefix(show, "SHOW CONSTRAINTS") {
				kind = "CONSTRAINT"
			}
			if _, err := neo4jdriver.ExecuteQuery(ctx,
				driver(t), fmt.Sprintf("DROP %s %s IF EXISTS", kind, quoteIdentifier(name)), nil,
				neo4jdriver.EagerResultTransformer); err != nil {
				t.Fatalf("dropping %s %q: %v", kind, name, err)
			}
		}
	}
}

// quoteIdentifier backtick-quotes a name for use in a DROP. Names the adapter
// emits never need it, but a hand-created fixture index in one of these tests
// might.
func quoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// awaitIndexes blocks until no index is left POPULATING, so a test asserting on
// steady-state introspection is not racing index population.
func awaitIndexes(t *testing.T, ctx context.Context) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for {
		populating := 0
		for _, rec := range query(t, ctx, "SHOW INDEXES YIELD state") {
			if state, _ := rec["state"].(string); strings.EqualFold(state, "POPULATING") {
				populating++
			}
		}
		if populating == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d index(es) still POPULATING after 2m", populating)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// single returns the sole record of a query, failing when there is not exactly
// one. The count is the whole diagnosis — zero means the fixture never landed,
// more than one means it landed twice — so the message states it and nothing
// else.
func single(t *testing.T, ctx context.Context, cypher string) map[string]any {
	t.Helper()
	records := query(t, ctx, cypher)
	if len(records) != 1 {
		t.Fatalf("%q returned %d records, want exactly 1", cypher, len(records))
	}
	return records[0]
}
