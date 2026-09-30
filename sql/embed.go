// Package sql embeds the numbered migration files so that `cardo` is a single self-contained
// binary: a platform team copies one file to a server and it carries its own schema.
//
// ADR-0019 chose numbered plain .sql files applied in order over dbt. They stay readable on disk
// and reviewable in a pull request; embedding does not change that, it only removes the need to
// ship a directory beside the binary.
//
// The package is named `sql` to match its directory. Nothing in this repository imports
// database/sql -- the ClickHouse store speaks the HTTP interface (ADR-0022) -- so there is no
// collision to guard against.
package sql

import "embed"

// ClickHouse holds sql/clickhouse/*.sql. Files are applied in lexical order, so the numeric
// prefix is the ordering and must not be reused or renumbered once a migration has shipped.
//
//go:embed clickhouse/*.sql
var ClickHouse embed.FS
