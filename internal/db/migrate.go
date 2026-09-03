package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func RunMigrations(ctx context.Context, sqlDB *sql.DB, dir string) error {
	if _, err := sqlDB.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version bigint PRIMARY KEY,
    name text NOT NULL,
    applied_at timestamptz NOT NULL DEFAULT now()
)`); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(files)

	for _, file := range files {
		version, name, err := migrationName(file)
		if err != nil {
			return err
		}

		applied, err := migrationApplied(ctx, sqlDB, version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		body, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", file, err)
		}

		tx, err := sqlDB.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", name, err)
		}

		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}

		if _, err := tx.ExecContext(ctx, `
INSERT INTO schema_migrations (version, name)
VALUES ($1, $2)
ON CONFLICT (version) DO NOTHING`, version, name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}

	return nil
}

func migrationApplied(ctx context.Context, sqlDB *sql.DB, version int64) (bool, error) {
	var exists bool
	err := sqlDB.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM schema_migrations
    WHERE version = $1
)`, version).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check migration %d: %w", version, err)
	}
	return exists, nil
}

func migrationName(path string) (int64, string, error) {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, ".up.sql")
	parts := strings.SplitN(name, "_", 2)
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("invalid migration name %s", base)
	}
	version, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("invalid migration version %s: %w", base, err)
	}
	return version, name, nil
}
