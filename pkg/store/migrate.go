package store

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// AdoptLegacySchema renames the schema a service used to own to the name it
// owns now — the data-continuity half of renaming a service (#50).
//
// A rename in code alone would have the new binary CREATE SCHEMA an empty
// namespace beside the full old one, and every read would answer "nothing
// here" about data that is three millimetres to the left. Refuses nothing and
// races nothing: if the new schema already exists the rename already happened
// (or this is a fresh database), and if neither exists Migrate creates the new
// one as usual.
func (db *DB) AdoptLegacySchema(ctx context.Context, former string) error {
	var hasNew, hasOld bool
	err := db.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = $1),
		       EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = $2)`,
		db.schema, former).Scan(&hasNew, &hasOld)
	if err != nil {
		return err
	}
	if hasNew || !hasOld {
		return nil
	}
	_, err = db.pool.Exec(ctx, fmt.Sprintf("ALTER SCHEMA %s RENAME TO %s",
		quoteIdent(former), quoteIdent(db.schema)))
	return err
}

// Migrate applies every .sql file in dir, in filename order, exactly once.
//
// Deliberately small. A migration framework is a dependency that has to be
// trusted about the one operation nobody watches; this is a table of applied
// filenames and a loop. What it does not do — down migrations, checksums of
// already-applied files — it does not do on purpose, because both invite
// editing history that a running deployment has already acted on.
//
// Each file runs inside a transaction with the advisory lock held, so two
// service replicas starting together cannot both apply it.
func (db *DB) Migrate(ctx context.Context, fsys fs.FS, dir string) error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return fmt.Errorf("no .sql files in %s", dir)
	}

	if _, err := db.pool.Exec(ctx,
		fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", quoteIdent(db.schema))); err != nil {
		return fmt.Errorf("create schema %s: %w", db.schema, err)
	}
	if _, err := db.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name        text PRIMARY KEY,
		applied_at  timestamptz NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	// One lock per schema, so services migrating in parallel do not serialise
	// behind each other — only replicas of the same service do.
	lockKey := int64(hash32(db.schema))
	if _, err := db.pool.Exec(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return fmt.Errorf("take migration lock: %w", err)
	}
	defer func() { _, _ = db.pool.Exec(ctx, "SELECT pg_advisory_unlock($1)", lockKey) }()

	for _, name := range names {
		var exists bool
		if err := db.pool.QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = $1)", name).
			Scan(&exists); err != nil {
			return fmt.Errorf("check %s: %w", name, err)
		}
		if exists {
			continue
		}
		body, err := fs.ReadFile(fsys, dir+"/"+name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if err := db.InTx(ctx, func(tx Querier) error {
			if _, err := tx.Exec(ctx, string(body)); err != nil {
				return fmt.Errorf("apply %s: %w", name, err)
			}
			_, err := tx.Exec(ctx, "INSERT INTO schema_migrations (name) VALUES ($1)", name)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

// quoteIdent is for the schema name, which comes from configuration rather than
// from a request — but a schema name is still an identifier being interpolated,
// and the day it comes from somewhere else is the day this matters.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func hash32(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}
