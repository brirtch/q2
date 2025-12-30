package db

import (
	"fmt"
	"sort"
	"time"
)

// Migration represents a database migration with up and down operations.
type Migration struct {
	// ID is a unique identifier for the migration, typically a timestamp like "20250101120000".
	// Migrations are applied in lexicographical order by ID.
	ID string

	// Up applies the migration.
	Up func(db *DB) error

	// Down rolls back the migration.
	Down func(db *DB) error
}

// registry holds all registered migrations.
var registry []Migration

// Register adds a migration to the registry.
// Call this from init() functions in migration files.
func Register(m Migration) {
	registry = append(registry, m)
}

// Migrate applies all pending migrations in order.
// It creates the migrations tracking table if it doesn't exist.
func (db *DB) Migrate() error {
	// Create migrations table if not exists
	result := db.Write(`
		CREATE TABLE IF NOT EXISTS _migrations (
			id TEXT PRIMARY KEY,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if result.Err != nil {
		return fmt.Errorf("failed to create migrations table: %w", result.Err)
	}

	// Get applied migrations
	applied, err := db.getAppliedMigrations()
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}

	// Sort migrations by ID
	migrations := make([]Migration, len(registry))
	copy(migrations, registry)
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].ID < migrations[j].ID
	})

	// Apply pending migrations
	for _, m := range migrations {
		if applied[m.ID] {
			continue
		}

		if err := db.applyMigration(m); err != nil {
			return fmt.Errorf("migration %s failed: %w", m.ID, err)
		}
	}

	return nil
}

// MigrateDown rolls back the last n migrations.
// If n is 0, rolls back all migrations.
func (db *DB) MigrateDown(n int) error {
	applied, err := db.getAppliedMigrations()
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}

	// Get applied migrations in reverse order
	var appliedIDs []string
	for id := range applied {
		appliedIDs = append(appliedIDs, id)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(appliedIDs)))

	if n == 0 || n > len(appliedIDs) {
		n = len(appliedIDs)
	}

	// Build migration lookup
	migrationMap := make(map[string]Migration)
	for _, m := range registry {
		migrationMap[m.ID] = m
	}

	// Rollback migrations
	for i := 0; i < n; i++ {
		id := appliedIDs[i]
		m, ok := migrationMap[id]
		if !ok {
			return fmt.Errorf("migration %s not found in registry", id)
		}

		if err := db.rollbackMigration(m); err != nil {
			return fmt.Errorf("rollback of migration %s failed: %w", id, err)
		}
	}

	return nil
}

// getAppliedMigrations returns a set of applied migration IDs.
func (db *DB) getAppliedMigrations() (map[string]bool, error) {
	applied := make(map[string]bool)

	// Check if _migrations table exists
	var count int
	row := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='_migrations'")
	if err := row.Scan(&count); err != nil {
		return nil, err
	}
	if count == 0 {
		// Table doesn't exist yet, no migrations applied
		return applied, nil
	}

	rows, err := db.Query("SELECT id FROM _migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		applied[id] = true
	}

	return applied, rows.Err()
}

// applyMigration runs the Up function and records the migration.
func (db *DB) applyMigration(m Migration) error {
	if m.Up == nil {
		return fmt.Errorf("migration %s has no Up function", m.ID)
	}

	if err := m.Up(db); err != nil {
		return err
	}

	result := db.Write(
		"INSERT INTO _migrations (id, applied_at) VALUES (?, ?)",
		m.ID, time.Now().UTC(),
	)
	return result.Err
}

// rollbackMigration runs the Down function and removes the migration record.
func (db *DB) rollbackMigration(m Migration) error {
	if m.Down == nil {
		return fmt.Errorf("migration %s has no Down function", m.ID)
	}

	if err := m.Down(db); err != nil {
		return err
	}

	result := db.Write("DELETE FROM _migrations WHERE id = ?", m.ID)
	return result.Err
}

// GetAppliedMigrations returns a list of applied migration IDs in order.
func (db *DB) GetAppliedMigrations() ([]string, error) {
	applied, err := db.getAppliedMigrations()
	if err != nil {
		return nil, err
	}

	var ids []string
	for id := range applied {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

// PendingMigrations returns migration IDs that haven't been applied yet.
func (db *DB) PendingMigrations() ([]string, error) {
	applied, err := db.getAppliedMigrations()
	if err != nil {
		return nil, err
	}

	var pending []string
	for _, m := range registry {
		if !applied[m.ID] {
			pending = append(pending, m.ID)
		}
	}
	sort.Strings(pending)
	return pending, nil
}

// ClearRegistry removes all registered migrations.
// This is primarily useful for testing.
func ClearRegistry() {
	registry = nil
}
