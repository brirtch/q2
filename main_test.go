package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestRemoveFolder_Success(t *testing.T) {
	database, testFolder, cleanup := setupTestEnv(t)
	defer cleanup()

	// Add a folder first
	err := addFolder(testFolder, database)
	if err != nil {
		t.Fatalf("addFolder failed: %v", err)
	}

	// Verify it's there
	folders := getFolders(t, database)
	if len(folders) != 1 {
		t.Fatalf("Expected 1 folder, got %d", len(folders))
	}

	// Remove it
	err = removeFolder(testFolder, database)
	if err != nil {
		t.Fatalf("removeFolder failed: %v", err)
	}

	// Verify it's gone
	folders = getFolders(t, database)
	if len(folders) != 0 {
		t.Fatalf("Expected 0 folders after removal, got %d", len(folders))
	}
}

func TestRemoveFolder_NotFound(t *testing.T) {
	database, _, cleanup := setupTestEnv(t)
	defer cleanup()

	err := removeFolder("/nonexistent/folder", database)
	if err == nil {
		t.Fatal("Expected error for non-existent folder, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("Expected 'not found' error, got: %v", err)
	}
}

func TestRemoveFolder_EmptyFolder(t *testing.T) {
	database, _, cleanup := setupTestEnv(t)
	defer cleanup()

	err := removeFolder("", database)
	if err == nil {
		t.Fatal("Expected error for empty folder, got nil")
	}
	if err.Error() != "folder cannot be empty" {
		t.Errorf("Expected 'folder cannot be empty' error, got: %v", err)
	}
}

func TestRemoveFolder_CaseHandlingOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows case test on non-Windows platform")
	}

	database, testFolder, cleanup := setupTestEnv(t)
	defer cleanup()

	// Add with original case
	err := addFolder(testFolder, database)
	if err != nil {
		t.Fatalf("addFolder failed: %v", err)
	}

	// Remove with different case (should work on Windows)
	upperFolder := strings.ToUpper(testFolder)
	err = removeFolder(upperFolder, database)
	if err != nil {
		t.Fatalf("removeFolder with different case failed: %v", err)
	}

	// Verify it's gone
	folders := getFolders(t, database)
	if len(folders) != 0 {
		t.Fatalf("Expected 0 folders after removal, got %d", len(folders))
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

// Tests for isPathWithinRoots

func TestIsPathWithinRoots_ExactMatch(t *testing.T) {
	roots := []string{"/home/user/photos", "/home/user/music"}
	if runtime.GOOS == "windows" {
		roots = []string{"c:\\users\\user\\photos", "c:\\users\\user\\music"}
	}

	result := isPathWithinRoots(roots[0], roots)
	if result == "" {
		t.Error("Expected root folder to match itself")
	}
}

func TestIsPathWithinRoots_Subdirectory(t *testing.T) {
	var root, subdir string
	if runtime.GOOS == "windows" {
		root = "c:\\users\\user\\photos"
		subdir = "c:\\users\\user\\photos\\2024\\vacation"
	} else {
		root = "/home/user/photos"
		subdir = "/home/user/photos/2024/vacation"
	}
	roots := []string{root}

	result := isPathWithinRoots(subdir, roots)
	if result == "" {
		t.Errorf("Expected subdirectory %s to be within root %s", subdir, root)
	}
}

func TestIsPathWithinRoots_OutsideRoots(t *testing.T) {
	var roots []string
	var outsidePath string
	if runtime.GOOS == "windows" {
		roots = []string{"c:\\users\\user\\photos"}
		outsidePath = "c:\\users\\user\\documents"
	} else {
		roots = []string{"/home/user/photos"}
		outsidePath = "/home/user/documents"
	}

	result := isPathWithinRoots(outsidePath, roots)
	if result != "" {
		t.Errorf("Expected path outside roots to not match, but got: %s", result)
	}
}

func TestIsPathWithinRoots_SimilarPrefix(t *testing.T) {
	// Test that /home/user/photos doesn't match /home/user/photos2
	var roots []string
	var similarPath string
	if runtime.GOOS == "windows" {
		roots = []string{"c:\\users\\user\\photos"}
		similarPath = "c:\\users\\user\\photos2"
	} else {
		roots = []string{"/home/user/photos"}
		similarPath = "/home/user/photos2"
	}

	result := isPathWithinRoots(similarPath, roots)
	if result != "" {
		t.Errorf("Expected similar prefix path to not match, but got: %s", result)
	}
}

func TestIsPathWithinRoots_EmptyRoots(t *testing.T) {
	result := isPathWithinRoots("/some/path", []string{})
	if result != "" {
		t.Error("Expected no match with empty roots")
	}
}

// Tests for listDirectory

func TestListDirectory_Basic(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "listdir-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create some test files and directories
	if err := os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	entries, err := listDirectory(tmpDir)
	if err != nil {
		t.Fatalf("listDirectory failed: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(entries))
	}

	// Check that we have both a file and a dir
	hasDir := false
	hasFile := false
	for _, e := range entries {
		if e.Name == "subdir" && e.Type == "dir" {
			hasDir = true
		}
		if e.Name == "file.txt" && e.Type == "file" {
			hasFile = true
			if e.Size != 4 {
				t.Errorf("Expected file size 4, got %d", e.Size)
			}
		}
	}
	if !hasDir {
		t.Error("Expected to find subdir")
	}
	if !hasFile {
		t.Error("Expected to find file.txt")
	}
}

func TestListDirectory_Empty(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "listdir-empty-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	entries, err := listDirectory(tmpDir)
	if err != nil {
		t.Fatalf("listDirectory failed: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("Expected 0 entries for empty dir, got %d", len(entries))
	}
}

func TestListDirectory_NonExistent(t *testing.T) {
	_, err := listDirectory("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Error("Expected error for non-existent directory")
	}
}

func TestListDirectory_HiddenFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "listdir-hidden-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a hidden file (starts with dot)
	if err := os.WriteFile(filepath.Join(tmpDir, ".hidden"), []byte("secret"), 0644); err != nil {
		t.Fatalf("Failed to create hidden file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "visible.txt"), []byte("visible"), 0644); err != nil {
		t.Fatalf("Failed to create visible file: %v", err)
	}

	entries, err := listDirectory(tmpDir)
	if err != nil {
		t.Fatalf("listDirectory failed: %v", err)
	}

	// Should include hidden files
	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries (including hidden), got %d", len(entries))
	}

	hasHidden := false
	for _, e := range entries {
		if e.Name == ".hidden" {
			hasHidden = true
		}
	}
	if !hasHidden {
		t.Error("Expected hidden file to be included")
	}
}

// Tests for /api/roots handler

func TestRootsHandler_Empty(t *testing.T) {
	database, _, cleanup := setupTestEnv(t)
	defer cleanup()

	handler := makeRootsHandler(database)
	req := httptest.NewRequest(http.MethodGet, "/api/roots", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp RootsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(resp.Roots) != 0 {
		t.Errorf("Expected 0 roots, got %d", len(resp.Roots))
	}
}

func TestRootsHandler_WithFolders(t *testing.T) {
	database, testFolder, cleanup := setupTestEnv(t)
	defer cleanup()

	// Add some folders
	parent := filepath.Dir(testFolder)
	folder1 := createTestFolder(t, parent, "photos")
	folder2 := createTestFolder(t, parent, "music")

	if err := addFolder(folder1, database); err != nil {
		t.Fatalf("addFolder failed: %v", err)
	}
	if err := addFolder(folder2, database); err != nil {
		t.Fatalf("addFolder failed: %v", err)
	}

	handler := makeRootsHandler(database)
	req := httptest.NewRequest(http.MethodGet, "/api/roots", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp RootsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(resp.Roots) != 2 {
		t.Errorf("Expected 2 roots, got %d", len(resp.Roots))
	}

	// Check that names are set correctly
	for _, root := range resp.Roots {
		if root.Name == "" {
			t.Error("Expected root name to be set")
		}
		if root.Path == "" {
			t.Error("Expected root path to be set")
		}
	}
}

func TestRootsHandler_MethodNotAllowed(t *testing.T) {
	database, _, cleanup := setupTestEnv(t)
	defer cleanup()

	handler := makeRootsHandler(database)
	req := httptest.NewRequest(http.MethodPost, "/api/roots", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

// Tests for /api/browse handler

func TestBrowseHandler_ValidPath(t *testing.T) {
	database, testFolder, cleanup := setupTestEnv(t)
	defer cleanup()

	// Add the test folder as a monitored folder
	if err := addFolder(testFolder, database); err != nil {
		t.Fatalf("addFolder failed: %v", err)
	}

	// Create some content in the folder
	subdir := filepath.Join(testFolder, "subdir")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(testFolder, "file.txt"), []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	handler := makeBrowseHandler(database)
	req := httptest.NewRequest(http.MethodGet, "/api/browse?path="+testFolder, nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp BrowseResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(resp.Entries) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(resp.Entries))
	}

	// Parent should be nil for root folder
	if resp.Parent != nil {
		t.Errorf("Expected parent to be nil for root folder, got %v", *resp.Parent)
	}
}

func TestBrowseHandler_Subdirectory(t *testing.T) {
	database, testFolder, cleanup := setupTestEnv(t)
	defer cleanup()

	// Add the test folder as a monitored folder
	if err := addFolder(testFolder, database); err != nil {
		t.Fatalf("addFolder failed: %v", err)
	}

	// Create a subdirectory
	subdir := filepath.Join(testFolder, "subdir")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	handler := makeBrowseHandler(database)
	req := httptest.NewRequest(http.MethodGet, "/api/browse?path="+subdir, nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp BrowseResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Parent should be set for subdirectory
	if resp.Parent == nil {
		t.Error("Expected parent to be set for subdirectory")
	} else if *resp.Parent != testFolder {
		t.Errorf("Expected parent to be %s, got %s", testFolder, *resp.Parent)
	}
}

func TestBrowseHandler_PathOutsideRoots(t *testing.T) {
	database, testFolder, cleanup := setupTestEnv(t)
	defer cleanup()

	// Add a folder
	if err := addFolder(testFolder, database); err != nil {
		t.Fatalf("addFolder failed: %v", err)
	}

	// Try to browse a path outside monitored folders
	outsidePath := filepath.Dir(filepath.Dir(testFolder))

	handler := makeBrowseHandler(database)
	req := httptest.NewRequest(http.MethodGet, "/api/browse?path="+outsidePath, nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d: %s", w.Code, w.Body.String())
	}

	var resp ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if !strings.Contains(resp.Error, "not within monitored") {
		t.Errorf("Expected 'not within monitored' error, got: %s", resp.Error)
	}
}

func TestBrowseHandler_NonExistentPath(t *testing.T) {
	database, testFolder, cleanup := setupTestEnv(t)
	defer cleanup()

	// Add a folder
	if err := addFolder(testFolder, database); err != nil {
		t.Fatalf("addFolder failed: %v", err)
	}

	// Try to browse a non-existent path within the monitored folder
	nonExistent := filepath.Join(testFolder, "does-not-exist")

	handler := makeBrowseHandler(database)
	req := httptest.NewRequest(http.MethodGet, "/api/browse?path="+nonExistent, nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBrowseHandler_FileNotDirectory(t *testing.T) {
	database, testFolder, cleanup := setupTestEnv(t)
	defer cleanup()

	// Add a folder
	if err := addFolder(testFolder, database); err != nil {
		t.Fatalf("addFolder failed: %v", err)
	}

	// Create a file and try to browse it
	filePath := filepath.Join(testFolder, "file.txt")
	if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	handler := makeBrowseHandler(database)
	req := httptest.NewRequest(http.MethodGet, "/api/browse?path="+filePath, nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d: %s", w.Code, w.Body.String())
	}

	var resp ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if !strings.Contains(resp.Error, "not a directory") {
		t.Errorf("Expected 'not a directory' error, got: %s", resp.Error)
	}
}

func TestBrowseHandler_MissingPath(t *testing.T) {
	database, _, cleanup := setupTestEnv(t)
	defer cleanup()

	handler := makeBrowseHandler(database)
	req := httptest.NewRequest(http.MethodGet, "/api/browse", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d: %s", w.Code, w.Body.String())
	}

	var resp ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if !strings.Contains(resp.Error, "path parameter required") {
		t.Errorf("Expected 'path parameter required' error, got: %s", resp.Error)
	}
}

func TestBrowseHandler_MethodNotAllowed(t *testing.T) {
	database, _, cleanup := setupTestEnv(t)
	defer cleanup()

	handler := makeBrowseHandler(database)
	req := httptest.NewRequest(http.MethodPost, "/api/browse?path=/some/path", nil)
	w := httptest.NewRecorder()

	handler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}
