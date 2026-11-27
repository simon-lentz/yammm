package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
)

// NewNeo4jDriver creates a Neo4j driver with basic authentication and dials
// nothing. A URI that names no server on its text is a usage error: one the
// driver refuses, such as an unsupported scheme, and one whose address no dial
// can use ([checkNeo4jAddress]). A command calls it where it judges its flags,
// before any read, and [VerifyNeo4j] dials.
func NewNeo4jDriver(ctx context.Context, uri, username, password string) (neo4j.Driver, error) {
	driver, err := neo4j.NewDriver(uri, neo4j.BasicAuth(username, password, ""))
	if err != nil {
		return nil, Usagef("connect to neo4j at %q: %w", uri, err)
	}
	if err := checkNeo4jAddress(ctx, driver.Target()); err != nil {
		_ = driver.Close(ctx)
		return nil, Usagef("connect to neo4j at %q: %w", uri, err)
	}
	return driver, nil
}

// checkNeo4jAddress refuses a target the driver accepted and no dial can use:
// an empty socket path, an empty network address, one net cannot split into a
// host and a port, and a port net cannot look up. The driver writes its default
// port onto a host that names none, so "bolt://h:" reaches here as "h::7687".
func checkNeo4jAddress(ctx context.Context, target url.URL) error {
	if target.Scheme == "bolt+unix" {
		if target.Path == "" {
			return errors.New("the URI names no socket path")
		}
		return nil
	}
	if target.Host == "" {
		return errors.New("the URI names no host")
	}
	_, port, err := net.SplitHostPort(target.Host)
	if err != nil {
		return fmt.Errorf("the URI's address: %w", err)
	}
	if _, err := net.DefaultResolver.LookupPort(ctx, "tcp", port); err != nil {
		return fmt.Errorf("the URI's port: %w", err)
	}
	return nil
}

// VerifyNeo4j reports a server that cannot be reached at uri as a runtime
// failure. The driver connects lazily, so without it a command holds a driver
// for a server that is not there and fails inside its first query.
func VerifyNeo4j(ctx context.Context, driver neo4j.Driver, uri string) error {
	if err := driver.VerifyConnectivity(ctx); err != nil {
		return Runtimef("verify neo4j connectivity at %q: %w", uri, err)
	}
	return nil
}

// QueryRunner executes one read query and returns its records.
//
// It is the seam `neo4j diff` reads the database through. Everything after the
// queries is pure — the parsers and the diff — so a runner returning recorded
// records exercises the command's whole read of the database without a server.
// [DriverQueries] is the production implementation.
type QueryRunner func(ctx context.Context, database, query string) ([]map[string]any, error)

// DriverQueries returns the [QueryRunner] that reads from driver.
func DriverQueries(driver neo4j.Driver) QueryRunner {
	return func(ctx context.Context, database, query string) ([]map[string]any, error) {
		return runQuery(ctx, driver, database, query)
	}
}

// readSession configures a session that reads database and can write nothing.
func readSession(database string) neo4j.SessionConfig {
	return neo4j.SessionConfig{DatabaseName: database, AccessMode: neo4j.AccessModeRead}
}

// runQuery executes a read transaction and returns all records as maps.
func runQuery(ctx context.Context, driver neo4j.Driver, database, query string) ([]map[string]any, error) {
	session := driver.NewSession(ctx, readSession(database))
	defer session.Close(ctx)

	records, err := neo4j.ExecuteRead(ctx, session, func(tx neo4j.ManagedTransaction) ([]map[string]any, error) {
		result, err := tx.Run(ctx, query, nil)
		if err != nil {
			return nil, fmt.Errorf("execute query: %w", err)
		}

		var rows []map[string]any
		for result.Next(ctx) {
			record := result.Record()
			row := make(map[string]any, len(record.Keys))
			for i, key := range record.Keys {
				row[key] = record.Values[i]
			}
			rows = append(rows, row)
		}

		if err := result.Err(); err != nil {
			return nil, fmt.Errorf("read results: %w", err)
		}

		return rows, nil
	})
	if err != nil {
		return nil, err
	}

	return records, nil
}
