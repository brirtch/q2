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

// setupTestEnv creates a temporary directory structure for testing.
// Returns the database, a test folder path, and a cleanup function.
func setupTestEnv(t *testing.T) (*db.DB, string, func()) {
	tmpDir, err := os.MkdirTemp("", "q2-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Create database in a subdirectory
	dbDir := filepath.Join(tmpDir, "db")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create db dir: %v", err)
	}

	dbPath := filepath.Join(dbDir, "test.db")
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

	// Create a test folder that can be added
	testFolder := filepath.Join(tmpDir, "testfolder")
	if err := os.MkdirAll(testFolder, 0755); err != nil {
		database.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create test folder: %v", err)
	}

	cleanup := func() {
		database.Close()
		os.RemoveAll(tmpDir)
	}

	return database, testFolder, cleanup
}

// createTestFolder creates a subfolder in the given parent directory.
func createTestFolder(t *testing.T, parent, name string) string {
	folder := filepath.Join(parent, name)
	if err := os.MkdirAll(folder, 0755); err != nil {
		t.Fatalf("Failed to create folder %s: %v", folder, err)
	}
	return folder
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
	database, testFolder, cleanup := setupTestEnv(t)
	defer cleanup()

	err := addFolder(testFolder, database)
	if err != nil {
		t.Fatalf("addFolder failed: %v", err)
	}

	folders := getFolders(t, database)
	if len(folders) != 1 {
		t.Fatalf("Expected 1 folder, got %d", len(folders))
	}

	expected := normalizePath(testFolder)
	if folders[0] != expected {
		t.Errorf("Expected %s, got %s", expected, folders[0])
	}
}

func TestAddFolder_EmptyFolder(t *testing.T) {
	database, _, cleanup := setupTestEnv(t)
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
	database, _, cleanup := setupTestEnv(t)
	defer cleanup()

	err := addFolder("   ", database)
	if err == nil {
		t.Fatal("Expected error for whitespace-only folder, got nil")
	}
}

func TestAddFolder_NonExistentFolder(t *testing.T) {
	database, _, cleanup := setupTestEnv(t)
	defer cleanup()

	err := addFolder("/nonexistent/path/that/does/not/exist", database)
	if err == nil {
		t.Fatal("Expected error for non-existent folder, got nil")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("Expected 'does not exist' error, got: %v", err)
	}
}

func TestAddFolder_FileNotDirectory(t *testing.T) {
	database, testFolder, cleanup := setupTestEnv(t)
	defer cleanup()

	// Create a file instead of a directory
	filePath := filepath.Join(filepath.Dir(testFolder), "testfile.txt")
	if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	err := addFolder(filePath, database)
	if err == nil {
		t.Fatal("Expected error for file path, got nil")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("Expected 'not a directory' error, got: %v", err)
	}
}

func TestAddFolder_ExactDuplicate(t *testing.T) {
	database, testFolder, cleanup := setupTestEnv(t)
	defer cleanup()

	// Add first time
	err := addFolder(testFolder, database)
	if err != nil {
		t.Fatalf("First addFolder failed: %v", err)
	}

	// Add second time (duplicate)
	err = addFolder(testFolder, database)
	if err != nil {
		t.Fatalf("Second addFolder failed: %v", err)
	}

	// Should still only have 1 entry
	folders := getFolders(t, database)
	if len(folders) != 1 {
		t.Fatalf("Expected 1 folder after duplicate, got %d", len(folders))
	}
}

func TestAddFolder_CaseHandlingOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows case test on non-Windows platform")
	}

	database, testFolder, cleanup := setupTestEnv(t)
	defer cleanup()

	// Add with original case
	err := addFolder(testFolder, database)
	if err != nil {
		t.Fatalf("First addFolder failed: %v", err)
	}

	// Add with different case (should be detected as duplicate on Windows)
	upperFolder := strings.ToUpper(testFolder)
	err = addFolder(upperFolder, database)
	if err != nil {
		t.Fatalf("Second addFolder failed: %v", err)
	}

	// Should still only have 1 entry (case-insensitive on Windows)
	folders := getFolders(t, database)
	if len(folders) != 1 {
		t.Fatalf("Expected 1 folder after case-insensitive duplicate, got %d", len(folders))
	}

	// Path should be stored as lowercase
	if folders[0] != strings.ToLower(testFolder) {
		t.Errorf("Expected lowercase path %s, got %s", strings.ToLower(testFolder), folders[0])
	}
}

func TestAddFolder_TrailingSlashDuplicate(t *testing.T) {
	database, testFolder, cleanup := setupTestEnv(t)
	defer cleanup()

	// Add first time with trailing separator
	err := addFolder(testFolder+string(filepath.Separator), database)
	if err != nil {
		t.Fatalf("First addFolder failed: %v", err)
	}

	// Add second time without trailing separator
	err = addFolder(testFolder, database)
	if err != nil {
		t.Fatalf("Second addFolder failed: %v", err)
	}

	// Should still only have 1 entry (filepath.Clean normalizes)
	folders := getFolders(t, database)
	if len(folders) != 1 {
		t.Fatalf("Expected 1 folder after trailing slash duplicate, got %d: %v", len(folders), folders)
	}
}

func TestAddFolder_MultipleDifferentFolders(t *testing.T) {
	database, testFolder, cleanup := setupTestEnv(t)
	defer cleanup()

	parent := filepath.Dir(testFolder)
	folders := []string{
		createTestFolder(t, parent, "folder1"),
		createTestFolder(t, parent, "folder2"),
		createTestFolder(t, parent, "folder3"),
	}

	for _, folder := range folders {
		err := addFolder(folder, database)
		if err != nil {
			t.Fatalf("addFolder failed for %s: %v", folder, err)
		}
	}

	savedFolders := getFolders(t, database)
	if len(savedFolders) != len(folders) {
		t.Fatalf("Expected %d folders, got %d", len(folders), len(savedFolders))
	}
}

func TestAddFolder_WhitespaceHandling(t *testing.T) {
	database, testFolder, cleanup := setupTestEnv(t)
	defer cleanup()

	// Add folder with leading/trailing whitespace
	err := addFolder("  "+testFolder+"  ", database)
	if err != nil {
		t.Fatalf("addFolder failed: %v", err)
	}

	folders := getFolders(t, database)
	if len(folders) != 1 {
		t.Fatalf("Expected 1 folder, got %d", len(folders))
	}

	// Should be cleaned (no whitespace)
	expected := normalizePath(testFolder)
	if folders[0] != expected {
		t.Errorf("Expected %s, got %s", expected, folders[0])
	}
}

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"cleans path", "foo//bar", filepath.Clean("foo//bar")},
		{"trims whitespace", "  foo  ", "foo"},
		{"handles trailing slash", "foo/", "foo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizePath(tt.input)
			expected := tt.expected
			if runtime.GOOS == "windows" {
				expected = strings.ToLower(expected)
			}
			if result != expected {
				t.Errorf("normalizePath(%q) = %q, expected %q", tt.input, result, expected)
			}
		})
	}
}

func TestNormalizePath_CaseSensitivity(t *testing.T) {
	input := "Foo/Bar"

	result := normalizePath(input)

	if runtime.GOOS == "windows" {
		if result != "foo\\bar" {
			t.Errorf("On Windows, expected lowercase 'foo\\bar', got %q", result)
		}
	} else {
		expected := filepath.Clean(input)
		if result != expected {
			t.Errorf("On Linux/macOS, expected %q, got %q", expected, result)
		}
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

func TestInitDB_MigrationsApplied(t *testing.T) {
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

	// Should have both migrations applied
	if len(applied) < 2 {
		t.Errorf("Expected at least 2 migrations applied, got %d: %v", len(applied), applied)
	}

	// Check for specific migrations
	hasCreate := false
	hasCaseFix := false
	for _, id := range applied {
		if strings.Contains(id, "create_folders") {
			hasCreate = true
		}
		if strings.Contains(id, "case_sensitivity") {
			hasCaseFix = true
		}
	}
	if !hasCreate {
		t.Error("Expected create_folders migration to be applied")
	}
	if !hasCaseFix {
		t.Error("Expected case_sensitivity migration to be applied")
	}
}

func TestListFolders_Empty(t *testing.T) {
	database, _, cleanup := setupTestEnv(t)
	defer cleanup()

	err := listFolders(database)
	if err != nil {
		t.Fatalf("listFolders failed: %v", err)
	}
}

func TestListFolders_WithFolders(t *testing.T) {
	database, testFolder, cleanup := setupTestEnv(t)
	defer cleanup()

	// Add some folders
	parent := filepath.Dir(testFolder)
	folders := []string{
		createTestFolder(t, parent, "aaa"),
		createTestFolder(t, parent, "bbb"),
		createTestFolder(t, parent, "ccc"),
	}

	for _, folder := range folders {
		if err := addFolder(folder, database); err != nil {
			t.Fatalf("addFolder failed: %v", err)
		}
	}

	// listFolders should not error
	err := listFolders(database)
	if err != nil {
		t.Fatalf("listFolders failed: %v", err)
	}

	// Verify folders are in database
	storedFolders := getFolders(t, database)
	if len(storedFolders) != 3 {
		t.Errorf("Expected 3 folders, got %d", len(storedFolders))
	}
}
