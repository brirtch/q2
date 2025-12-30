package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"jukel.org/q2/db"
	_ "jukel.org/q2/migrations"
)

// setupTestDB creates a temporary database for testing.
func setupTestDB(t *testing.T) (*db.DB, func()) {
	tmpDir, err := os.MkdirTemp("", "q2-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to open database: %v", err)
	}

	if err := database.Migrate(); err != nil {
		database.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to run migrations: %v", err)
	}

	cleanup := func() {
		database.Close()
		os.RemoveAll(tmpDir)
	}

	return database, cleanup
}

// getFolders retrieves all folders from the database.
func getFolders(t *testing.T, database *db.DB) []string {
	rows, err := database.Query("SELECT path FROM folders ORDER BY path")
	if err != nil {
		t.Fatalf("Failed to query folders: %v", err)
	}
	defer rows.Close()

	var folders []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			t.Fatalf("Failed to scan folder: %v", err)
		}
		folders = append(folders, path)
	}
	return folders
}

func TestAddFolder_Basic(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	folder := `c:\test`
	err := addFolder(folder, database)
	if err != nil {
		t.Fatalf("addFolder failed: %v", err)
	}

	folders := getFolders(t, database)
	if len(folders) != 1 {
		t.Fatalf("Expected 1 folder, got %d", len(folders))
	}
	if folders[0] != filepath.Clean(folder) {
		t.Errorf("Expected %s, got %s", filepath.Clean(folder), folders[0])
	}
}

func TestAddFolder_EmptyFolder(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	err := addFolder("", database)
	if err == nil {
		t.Fatal("Expected error for empty folder, got nil")
	}
	if err.Error() != "folder cannot be empty" {
		t.Errorf("Expected 'folder cannot be empty' error, got: %v", err)
	}
}

func TestAddFolder_WhitespaceOnly(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	err := addFolder("   ", database)
	if err == nil {
		t.Fatal("Expected error for whitespace-only folder, got nil")
	}
}

func TestAddFolder_ExactDuplicate(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	folder := `c:\test`

	// Add first time
	err := addFolder(folder, database)
	if err != nil {
		t.Fatalf("First addFolder failed: %v", err)
	}

	// Add second time (duplicate)
	err = addFolder(folder, database)
	if err != nil {
		t.Fatalf("Second addFolder failed: %v", err)
	}

	// Should still only have 1 entry
	folders := getFolders(t, database)
	if len(folders) != 1 {
		t.Fatalf("Expected 1 folder after duplicate, got %d", len(folders))
	}
}

func TestAddFolder_CaseInsensitiveDuplicate(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// Add first time with lowercase
	err := addFolder(`c:\test`, database)
	if err != nil {
		t.Fatalf("First addFolder failed: %v", err)
	}

	// Add second time with uppercase (should be detected as duplicate due to COLLATE NOCASE)
	err = addFolder(`C:\TEST`, database)
	if err != nil {
		t.Fatalf("Second addFolder failed: %v", err)
	}

	// Should still only have 1 entry
	folders := getFolders(t, database)
	if len(folders) != 1 {
		t.Fatalf("Expected 1 folder after case-insensitive duplicate, got %d", len(folders))
	}
}

func TestAddFolder_TrailingSlashDuplicate(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// Add first time with trailing backslash
	err := addFolder(`c:\test\`, database)
	if err != nil {
		t.Fatalf("First addFolder failed: %v", err)
	}

	// Add second time without trailing backslash (should be detected as duplicate after Clean)
	err = addFolder(`c:\test`, database)
	if err != nil {
		t.Fatalf("Second addFolder failed: %v", err)
	}

	// Should still only have 1 entry
	folders := getFolders(t, database)
	if len(folders) != 1 {
		t.Fatalf("Expected 1 folder after trailing slash duplicate, got %d: %v", len(folders), folders)
	}
}

func TestAddFolder_ReverseTrailingSlashDuplicate(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// Add first time without trailing backslash
	err := addFolder(`c:\test`, database)
	if err != nil {
		t.Fatalf("First addFolder failed: %v", err)
	}

	// Add second time with trailing backslash (should be detected as duplicate after Clean)
	err = addFolder(`c:\test\`, database)
	if err != nil {
		t.Fatalf("Second addFolder failed: %v", err)
	}

	// Should still only have 1 entry
	folders := getFolders(t, database)
	if len(folders) != 1 {
		t.Fatalf("Expected 1 folder after trailing slash duplicate, got %d: %v", len(folders), folders)
	}
}

func TestAddFolder_MultipleDifferentFolders(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	inputFolders := []string{`c:\test1`, `c:\test2`, `c:\test3`}

	for _, folder := range inputFolders {
		err := addFolder(folder, database)
		if err != nil {
			t.Fatalf("addFolder failed for %s: %v", folder, err)
		}
	}

	savedFolders := getFolders(t, database)
	if len(savedFolders) != len(inputFolders) {
		t.Fatalf("Expected %d folders, got %d", len(inputFolders), len(savedFolders))
	}
}

func TestAddFolder_WhitespaceHandling(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	// Add folder with leading/trailing whitespace
	err := addFolder("  c:\\test  ", database)
	if err != nil {
		t.Fatalf("addFolder failed: %v", err)
	}

	folders := getFolders(t, database)
	if len(folders) != 1 {
		t.Fatalf("Expected 1 folder, got %d", len(folders))
	}

	// Should be cleaned (no whitespace)
	if folders[0] != `c:\test` {
		t.Errorf("Expected c:\\test, got %s", folders[0])
	}
}

func TestAddFolder_LinuxPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping Linux path test on Windows")
	}

	database, cleanup := setupTestDB(t)
	defer cleanup()

	folder := `/home/user/test`
	err := addFolder(folder, database)
	if err != nil {
		t.Fatalf("addFolder failed: %v", err)
	}

	folders := getFolders(t, database)
	if len(folders) != 1 {
		t.Fatalf("Expected 1 folder, got %d", len(folders))
	}
	if folders[0] != folder {
		t.Errorf("Expected %s, got %s", folder, folders[0])
	}
}

func TestAddFolder_CaseSensitivityOnLinux(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping case sensitivity test on Windows")
	}

	database, cleanup := setupTestDB(t)
	defer cleanup()

	// On Linux with COLLATE NOCASE, these are still treated as duplicates
	// This test documents current behavior - may need to change if we
	// implement platform-specific case handling
	err := addFolder(`/home/Test`, database)
	if err != nil {
		t.Fatalf("First addFolder failed: %v", err)
	}

	err = addFolder(`/home/test`, database)
	if err != nil {
		t.Fatalf("Second addFolder failed: %v", err)
	}

	folders := getFolders(t, database)
	// Current behavior: COLLATE NOCASE treats them as same
	// Note: This may not be desired behavior on Linux
	if len(folders) != 1 {
		t.Logf("Note: %d folders stored - case sensitivity behavior may need review for Linux", len(folders))
	}
}

func TestInitDB(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "q2-initdb-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	baseDir := filepath.Join(tmpDir, "subdir")

	database, err := initDB(baseDir)
	if err != nil {
		t.Fatalf("initDB failed: %v", err)
	}
	defer database.Close()

	// Verify directory was created
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		t.Error("Expected base directory to be created")
	}

	// Verify database file was created
	dbPath := filepath.Join(baseDir, dbFile)
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("Expected database file to be created")
	}

	// Verify folders table exists (migration ran)
	var name string
	row := database.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='folders'")
	if err := row.Scan(&name); err != nil {
		t.Errorf("folders table not created: %v", err)
	}
}

func TestInitDB_MigrationsTable(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "q2-migrations-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	database, err := initDB(tmpDir)
	if err != nil {
		t.Fatalf("initDB failed: %v", err)
	}
	defer database.Close()

	// Verify migrations were recorded
	applied, err := database.GetAppliedMigrations()
	if err != nil {
		t.Fatalf("GetAppliedMigrations failed: %v", err)
	}

	found := false
	for _, id := range applied {
		if strings.Contains(id, "folders") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected folders migration to be applied")
	}
}
