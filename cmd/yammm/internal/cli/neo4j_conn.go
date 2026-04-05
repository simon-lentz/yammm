package cli

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j"
)

// ConnectNeo4j creates a new Neo4j driver with basic authentication.
func ConnectNeo4j(ctx context.Context, uri, username, password string) (neo4j.Driver, error) {
	driver, err := neo4j.NewDriver(uri, neo4j.BasicAuth(username, password, ""))
	if err != nil {
		return nil, fmt.Errorf("connect to neo4j at %s: %w", uri, err)
	}

	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(ctx)
		return nil, fmt.Errorf("verify neo4j connectivity at %s: %w", uri, err)
	}

	return driver, nil
}

// RunQuery executes a read transaction and returns all records as maps.
func RunQuery(ctx context.Context, driver neo4j.Driver, database, query string, params map[string]any) ([]map[string]any, error) {
	session := driver.NewSession(ctx, neo4j.SessionConfig{
		DatabaseName: database,
		AccessMode:   neo4j.AccessModeRead,
	})
	defer session.Close(ctx)

	records, err := neo4j.ExecuteRead(ctx, session, func(tx neo4j.ManagedTransaction) ([]map[string]any, error) {
		result, err := tx.Run(ctx, query, params)
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
