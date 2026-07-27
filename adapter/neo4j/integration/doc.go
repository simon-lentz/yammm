// Package integration round-trips the neo4j adapter against a real Neo4j
// server started in Docker.
//
// # Why it exists
//
// Every other test of [github.com/simon-lentz/yammm/adapter/neo4j] compares the
// adapter's output against fixtures somebody hand-wrote from their
// understanding of Neo4j. Those tests converge on "consistent with our model of
// the server", not "correct against the server", and the two differ in ways no
// amount of review can find: a column name that does not exist, a value the
// driver delivers as a different Go type, a similarity function the server
// echoes back in a different case, a statement form the parser rejects. A
// fixture that agrees with the code proves nothing when both share the same
// wrong assumption.
//
// This package closes that gap for the claims the adapter actually depends on:
// that its emitted DDL executes for every constraint kind and index kind it can
// produce, that its introspection queries parse and yield the columns it reads,
// that the Go types it type-asserts on are the ones the driver delivers, that
// the classifications it makes (drift, unverified, drop) are the ones a real
// server's output produces, and that introspecting what it just applied diffs
// clean.
//
// # Running
//
// Guarded by a build tag, so neither `go test ./...` nor the pre-commit gate
// needs Docker:
//
//	go test -tags neo4j_integration ./adapter/neo4j/integration/
//
// One container is started per package run and shared by every test. If Docker
// is not reachable the tests skip rather than fail, so the tag is the only
// thing standing between a developer and running them.
//
// # Edition
//
// The default image is Enterprise, started with the evaluation licence
// accepted, because Enterprise is the edition the adapter targets by default.
// NOT NULL, TYPE, and NODE KEY constraints are Enterprise-only, as is the
// propertyType column the constraint diff compares and its Unverified bucket
// turns on — on Community the adapter emits UNIQUE alone and three of its four
// constraint kinds would never execute here.
//
// YAMMM_NEO4J_TEST_IMAGE overrides the image. Pointing it at a Community image
// is supported and the licence acceptance is inert there: the adapter under test
// is built with a matching edition, so it emits UNIQUE alone and every statement
// still executes. Only the tests whose subject IS an Enterprise-only capability
// skip.
//
// # Why this is not a separate module
//
// testcontainers-go and its Neo4j module are direct requirements of the
// published library's go.mod, which is not build-tag aware, so a consumer
// inherits roughly thirty Docker, containerd and OpenTelemetry modules for a
// package it never compiles. Moving this directory into a nested module would
// remove them.
//
// It is deliberately not done. The same go.mod already carries the
// golangci-lint toolchain through a `tool` directive, whose indirect graph is
// several times larger; testcontainers is the same class of dev-only dependency
// and moving one while keeping the other treats the symptom. A nested module
// also takes this package out of the repo's single golangci-lint invocation —
// `run.build-tags` in .golangci.yml is what puts it under the type checker at
// all, and 1,500 lines of test code compiled by nothing else in the gate is the
// worse exposure. If the dependency graph is to be trimmed, the change is to the
// whole dev-dependency surface at once.
package integration
