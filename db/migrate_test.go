package db

import (
	"os"
	"path/filepath"
	"testing"
)

func setupMigrateTestDB(t *testing.T) (*DB, func()) {
	// Clear registry before each test
	ClearRegistry()

	tmpDir, err := os.MkdirTemp("", "q2-migrate-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := Open(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to open database: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
		ClearRegistry()
	}

	return db, cleanup
}

func TestMigrate_CreatesMigrationsTable(t *testing.T) {
	db, cleanup := setupMigrateTestDB(t)
	defer cleanup()

	err := db.Migrate()
	if err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// Verify _migrations table exists
	var name string
	row := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='_migrations'")
	if err := row.Scan(&name); err != nil {
		t.Fatalf("_migrations table not created: %v", err)
	}
}

func TestMigrate_AppliesMigrations(t *testing.T) {
	db, cleanup := setupMigrateTestDB(t)
	defer cleanup()

	// Register test migrations
	Register(Migration{
		ID: "001_create_users",
		Up: func(db *DB) error {
			result := db.Write("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)")
			return result.Err
		},
		Down: func(db *DB) error {
			result := db.Write("DROP TABLE users")
			return result.Err
		},
	})

	Register(Migration{
		ID: "002_create_posts",
		Up: func(db *DB) error {
			result := db.Write("CREATE TABLE posts (id INTEGER PRIMARY KEY, title TEXT)")
			return result.Err
		},
		Down: func(db *DB) error {
			result := db.Write("DROP TABLE posts")
			return result.Err
		},
	})

	err := db.Migrate()
	if err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// Verify tables were created
	var count int
	row := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('users', 'posts')")
	if err := row.Scan(&count); err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected 2 tables, got %d", count)
	}

	// Verify migrations were recorded
	applied, err := db.GetAppliedMigrations()
	if err != nil {
		t.Fatalf("GetAppliedMigrations failed: %v", err)
	}
	if len(applied) != 2 {
		t.Errorf("Expected 2 applied migrations, got %d", len(applied))
	}
}

func TestMigrate_SkipsAppliedMigrations(t *testing.T) {
	db, cleanup := setupMigrateTestDB(t)
	defer cleanup()

	callCount := 0
	Register(Migration{
		ID: "001_test",
		Up: func(db *DB) error {
			callCount++
			result := db.Write("CREATE TABLE IF NOT EXISTS test (id INTEGER)")
			return result.Err
		},
		Down: func(db *DB) error {
			result := db.Write("DROP TABLE test")
			return result.Err
		},
	})

	// Run migrate twice
	if err := db.Migrate(); err != nil {
		t.Fatalf("First migrate failed: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("Second migrate failed: %v", err)
	}

	// Up should only be called once
	if callCount != 1 {
		t.Errorf("Expected Up to be called 1 time, got %d", callCount)
	}
}

func TestMigrate_AppliesInOrder(t *testing.T) {
	db, cleanup := setupMigrateTestDB(t)
	defer cleanup()

	var order []string

	// Register out of order
	Register(Migration{
		ID: "003_third",
		Up: func(db *DB) error {
			order = append(order, "003")
			return nil
		},
		Down: func(db *DB) error { return nil },
	})

	Register(Migration{
		ID: "001_first",
		Up: func(db *DB) error {
			order = append(order, "001")
			return nil
		},
		Down: func(db *DB) error { return nil },
	})

	Register(Migration{
		ID: "002_second",
		Up: func(db *DB) error {
			order = append(order, "002")
			return nil
		},
		Down: func(db *DB) error { return nil },
	})

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	expected := []string{"001", "002", "003"}
	if len(order) != len(expected) {
		t.Fatalf("Expected %d migrations, got %d", len(expected), len(order))
	}
	for i, id := range expected {
		if order[i] != id {
			t.Errorf("Expected migration %s at position %d, got %s", id, i, order[i])
		}
	}
}

func TestMigrateDown_RollsBackMigrations(t *testing.T) {
	db, cleanup := setupMigrateTestDB(t)
	defer cleanup()

	Register(Migration{
		ID: "001_create_users",
		Up: func(db *DB) error {
			result := db.Write("CREATE TABLE users (id INTEGER PRIMARY KEY)")
			return result.Err
		},
		Down: func(db *DB) error {
			result := db.Write("DROP TABLE users")
			return result.Err
		},
	})

	Register(Migration{
		ID: "002_create_posts",
		Up: func(db *DB) error {
			result := db.Write("CREATE TABLE posts (id INTEGER PRIMARY KEY)")
			return result.Err
		},
		Down: func(db *DB) error {
			result := db.Write("DROP TABLE posts")
			return result.Err
		},
	})

	// Apply migrations
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// Roll back last migration
	if err := db.MigrateDown(1); err != nil {
		t.Fatalf("MigrateDown failed: %v", err)
	}

	// Verify posts table was dropped
	var count int
	row := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='posts'")
	if err := row.Scan(&count); err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if count != 0 {
		t.Error("Expected posts table to be dropped")
	}

	// Verify users table still exists
	row = db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='users'")
	if err := row.Scan(&count); err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if count != 1 {
		t.Error("Expected users table to still exist")
	}

	// Verify migration record was removed
	applied, err := db.GetAppliedMigrations()
	if err != nil {
		t.Fatalf("GetAppliedMigrations failed: %v", err)
	}
	if len(applied) != 1 {
		t.Errorf("Expected 1 applied migration, got %d", len(applied))
	}
}

func TestPendingMigrations(t *testing.T) {
	db, cleanup := setupMigrateTestDB(t)
	defer cleanup()

	Register(Migration{
		ID:   "001_first",
		Up:   func(db *DB) error { return nil },
		Down: func(db *DB) error { return nil },
	})

	Register(Migration{
		ID:   "002_second",
		Up:   func(db *DB) error { return nil },
		Down: func(db *DB) error { return nil },
	})

	// Before migrating, both should be pending
	pending, err := db.PendingMigrations()
	if err != nil {
		t.Fatalf("PendingMigrations failed: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("Expected 2 pending migrations, got %d", len(pending))
	}

	// Apply migrations
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	// After migrating, none should be pending
	pending, err = db.PendingMigrations()
	if err != nil {
		t.Fatalf("PendingMigrations failed: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("Expected 0 pending migrations, got %d", len(pending))
	}
}

func TestMigrate_FailsOnError(t *testing.T) {
	db, cleanup := setupMigrateTestDB(t)
	defer cleanup()

	Register(Migration{
		ID: "001_bad",
		Up: func(db *DB) error {
			result := db.Write("INVALID SQL STATEMENT")
			return result.Err
		},
		Down: func(db *DB) error { return nil },
	})

	err := db.Migrate()
	if err == nil {
		t.Error("Expected migration to fail")
	}
}
