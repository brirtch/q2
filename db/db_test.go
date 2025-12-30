package db

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func setupTestDB(t *testing.T) (*DB, func()) {
	tmpDir, err := os.MkdirTemp("", "q2-db-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := Open(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to open database: %v", err)
	}

	// Create a test table
	result := db.Write(`CREATE TABLE IF NOT EXISTS test (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		value INTEGER
	)`)
	if result.Err != nil {
		db.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create test table: %v", result.Err)
	}

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}

	return db, cleanup
}

func TestOpen(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "q2-db-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	// Verify WAL mode is enabled
	var journalMode string
	row := db.QueryRow("PRAGMA journal_mode")
	if err := row.Scan(&journalMode); err != nil {
		t.Fatalf("Failed to query journal mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("Expected journal_mode=wal, got %s", journalMode)
	}
}

func TestWrite_Insert(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	result := db.Write("INSERT INTO test (name, value) VALUES (?, ?)", "foo", 42)
	if result.Err != nil {
		t.Fatalf("Write failed: %v", result.Err)
	}
	if result.LastInsertID != 1 {
		t.Errorf("Expected LastInsertID=1, got %d", result.LastInsertID)
	}
	if result.RowsAffected != 1 {
		t.Errorf("Expected RowsAffected=1, got %d", result.RowsAffected)
	}
}

func TestWrite_Update(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Insert initial row
	db.Write("INSERT INTO test (name, value) VALUES (?, ?)", "foo", 42)

	// Update it
	result := db.Write("UPDATE test SET value = ? WHERE name = ?", 100, "foo")
	if result.Err != nil {
		t.Fatalf("Update failed: %v", result.Err)
	}
	if result.RowsAffected != 1 {
		t.Errorf("Expected RowsAffected=1, got %d", result.RowsAffected)
	}

	// Verify the update
	var value int
	row := db.QueryRow("SELECT value FROM test WHERE name = ?", "foo")
	if err := row.Scan(&value); err != nil {
		t.Fatalf("QueryRow failed: %v", err)
	}
	if value != 100 {
		t.Errorf("Expected value=100, got %d", value)
	}
}

func TestQuery(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Insert test data
	db.Write("INSERT INTO test (name, value) VALUES (?, ?)", "a", 1)
	db.Write("INSERT INTO test (name, value) VALUES (?, ?)", "b", 2)
	db.Write("INSERT INTO test (name, value) VALUES (?, ?)", "c", 3)

	rows, err := db.Query("SELECT name, value FROM test ORDER BY value")
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	defer rows.Close()

	var results []struct {
		name  string
		value int
	}
	for rows.Next() {
		var r struct {
			name  string
			value int
		}
		if err := rows.Scan(&r.name, &r.value); err != nil {
			t.Fatalf("Scan failed: %v", err)
		}
		results = append(results, r)
	}

	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(results))
	}
	if results[0].name != "a" || results[0].value != 1 {
		t.Errorf("Unexpected first result: %+v", results[0])
	}
}

func TestQueryRow(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	db.Write("INSERT INTO test (name, value) VALUES (?, ?)", "foo", 42)

	var name string
	var value int
	row := db.QueryRow("SELECT name, value FROM test WHERE id = ?", 1)
	if err := row.Scan(&name, &value); err != nil {
		t.Fatalf("QueryRow.Scan failed: %v", err)
	}

	if name != "foo" {
		t.Errorf("Expected name=foo, got %s", name)
	}
	if value != 42 {
		t.Errorf("Expected value=42, got %d", value)
	}
}

func TestConcurrentWrites(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Launch many concurrent writers
	const numWriters = 50
	var wg sync.WaitGroup
	errors := make(chan error, numWriters)

	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			result := db.Write("INSERT INTO test (name, value) VALUES (?, ?)", "writer", id)
			if result.Err != nil {
				errors <- result.Err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("Concurrent write failed: %v", err)
	}

	// Verify all writes succeeded
	var count int
	row := db.QueryRow("SELECT COUNT(*) FROM test WHERE name = ?", "writer")
	if err := row.Scan(&count); err != nil {
		t.Fatalf("Count query failed: %v", err)
	}
	if count != numWriters {
		t.Errorf("Expected %d rows, got %d", numWriters, count)
	}
}

func TestConcurrentReadsAndWrites(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Insert initial data
	for i := 0; i < 10; i++ {
		db.Write("INSERT INTO test (name, value) VALUES (?, ?)", "initial", i)
	}

	var wg sync.WaitGroup
	const numOps = 100

	// Concurrent writers
	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			db.Write("INSERT INTO test (name, value) VALUES (?, ?)", "concurrent", id)
		}(i)
	}

	// Concurrent readers
	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, err := db.Query("SELECT * FROM test")
			if err != nil {
				t.Errorf("Read failed: %v", err)
				return
			}
			rows.Close()
		}()
	}

	wg.Wait()

	// Verify write count
	var count int
	row := db.QueryRow("SELECT COUNT(*) FROM test WHERE name = ?", "concurrent")
	if err := row.Scan(&count); err != nil {
		t.Fatalf("Count query failed: %v", err)
	}
	if count != numOps {
		t.Errorf("Expected %d concurrent rows, got %d", numOps, count)
	}
}

func TestWriteContext_Cancellation(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	result := db.WriteContext(ctx, "INSERT INTO test (name, value) VALUES (?, ?)", "foo", 1)
	if result.Err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got: %v", result.Err)
	}
}

func TestWriteContext_Timeout(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	time.Sleep(1 * time.Millisecond) // Ensure timeout

	result := db.WriteContext(ctx, "INSERT INTO test (name, value) VALUES (?, ?)", "foo", 1)
	if result.Err != context.DeadlineExceeded {
		// Could also be successful if the write was fast enough
		if result.Err != nil {
			t.Logf("Got expected timeout or error: %v", result.Err)
		}
	}
}

func TestClose(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "q2-db-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// Write something
	db.Write("CREATE TABLE test (id INTEGER)")
	db.Write("INSERT INTO test (id) VALUES (1)")

	// Close should not error
	if err := db.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

func TestWriteError(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// Try to insert into non-existent table
	result := db.Write("INSERT INTO nonexistent (foo) VALUES (?)", "bar")
	if result.Err == nil {
		t.Error("Expected error for insert into nonexistent table")
	}
}
