package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"
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
	conn, err := db.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire legacy-schema lock connection: %w", err)
	}
	defer conn.Release()
	lockKey := int64(hash32(db.schema))
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return fmt.Errorf("take legacy-schema lock: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", lockKey); err != nil {
			_ = conn.Conn().Close(unlockCtx)
		}
	}()

	var hasNew, hasOld bool
	err = conn.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = $1),
		       EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = $2)`,
		db.schema, former).Scan(&hasNew, &hasOld)
	if err != nil {
		return err
	}
	if hasNew || !hasOld {
		return nil
	}
	_, err = conn.Exec(ctx, fmt.Sprintf("ALTER SCHEMA %s RENAME TO %s",
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

	conn, err := db.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()
	lockKey := int64(hash32(db.schema))
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return fmt.Errorf("take migration lock: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", lockKey); err != nil {
			_ = conn.Conn().Close(unlockCtx)
		}
	}()
	if _, err := conn.Exec(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", quoteIdent(db.schema))); err != nil {
		return err
	}
	// Service-auth replay protection is shared infrastructure rather than a
	// service-owned business migration. Provision it under the same advisory
	// lock so every replica has the atomic nonce table before its middleware can
	// accept internal traffic.
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS service_auth_nonces (
		service_id text NOT NULL,
		nonce text NOT NULL,
		expires_at timestamptz NOT NULL,
		claimed_at timestamptz NOT NULL DEFAULT now(),
		PRIMARY KEY (service_id, nonce)
	)`); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, `CREATE INDEX IF NOT EXISTS service_auth_nonces_expires_at ON service_auth_nonces (expires_at)`); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
  name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now(), checksum text
 )`); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, `ALTER TABLE schema_migrations ADD COLUMN IF NOT EXISTS checksum text`); err != nil {
		return err
	}

	for _, name := range names {
		body, err := fs.ReadFile(fsys, dir+"/"+name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		digest := sha256.Sum256(body)
		checksum := hex.EncodeToString(digest[:])
		var exists bool
		if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name=$1)", name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			var recorded *string
			if err := conn.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE name=$1", name).Scan(&recorded); err != nil {
				return err
			}
			if recorded != nil && *recorded != checksum {
				return fmt.Errorf("applied migration %s changed; use an additive migration", name)
			}
			if recorded == nil {
				if _, err := conn.Exec(ctx, "UPDATE schema_migrations SET checksum=$2 WHERE name=$1", name, checksum); err != nil {
					return err
				}
			}
			continue
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(body)); err == nil {
			_, err = tx.Exec(ctx, "INSERT INTO schema_migrations (name, checksum) VALUES ($1,$2)", name, checksum)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
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
