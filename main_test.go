package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupTestDir creates a temporary directory for testing.
func setupTestDir(t *testing.T) string {
	tmpDir, err := os.MkdirTemp("", "q2-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	return tmpDir
}

// cleanupTestDir removes the temporary test directory.
func cleanupTestDir(t *testing.T, dir string) {
	if err := os.RemoveAll(dir); err != nil {
		t.Errorf("Failed to cleanup test dir: %v", err)
	}
}

// readFolders reads all folders from the folders.txt file.
func readFolders(t *testing.T, baseDir string) []string {
	filePath := filepath.Join(baseDir, "folders.txt")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}
		}
		t.Fatalf("Failed to read folders.txt: %v", err)
	}

	lines := strings.Split(string(data), "\n")
	var folders []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			folders = append(folders, line)
		}
	}
	return folders
}

func TestAddFolder_Basic(t *testing.T) {
	tmpDir := setupTestDir(t)
	defer cleanupTestDir(t, tmpDir)

	folder := `c:\test`
	err := addFolder(folder, tmpDir)
	if err != nil {
		t.Fatalf("addFolder failed: %v", err)
	}

	folders := readFolders(t, tmpDir)
	if len(folders) != 1 {
		t.Fatalf("Expected 1 folder, got %d", len(folders))
	}
	if folders[0] != filepath.Clean(folder) {
		t.Errorf("Expected %s, got %s", filepath.Clean(folder), folders[0])
	}
}

func TestAddFolder_EmptyFolder(t *testing.T) {
	tmpDir := setupTestDir(t)
	defer cleanupTestDir(t, tmpDir)

	err := addFolder("", tmpDir)
	if err == nil {
		t.Fatal("Expected error for empty folder, got nil")
	}
	if err.Error() != "folder cannot be empty" {
		t.Errorf("Expected 'folder cannot be empty' error, got: %v", err)
	}
}

func TestAddFolder_WhitespaceOnly(t *testing.T) {
	tmpDir := setupTestDir(t)
	defer cleanupTestDir(t, tmpDir)

	err := addFolder("   ", tmpDir)
	if err == nil {
		t.Fatal("Expected error for whitespace-only folder, got nil")
	}
}

func TestAddFolder_ExactDuplicate(t *testing.T) {
	tmpDir := setupTestDir(t)
	defer cleanupTestDir(t, tmpDir)

	folder := `c:\test`

	// Add first time
	err := addFolder(folder, tmpDir)
	if err != nil {
		t.Fatalf("First addFolder failed: %v", err)
	}

	// Add second time (duplicate)
	err = addFolder(folder, tmpDir)
	if err != nil {
		t.Fatalf("Second addFolder failed: %v", err)
	}

	// Should still only have 1 entry
	folders := readFolders(t, tmpDir)
	if len(folders) != 1 {
		t.Fatalf("Expected 1 folder after duplicate, got %d", len(folders))
	}
}

func TestAddFolder_CaseInsensitiveDuplicate(t *testing.T) {
	tmpDir := setupTestDir(t)
	defer cleanupTestDir(t, tmpDir)

	// Add first time with lowercase
	err := addFolder(`c:\test`, tmpDir)
	if err != nil {
		t.Fatalf("First addFolder failed: %v", err)
	}

	// Add second time with uppercase (should be detected as duplicate)
	err = addFolder(`C:\TEST`, tmpDir)
	if err != nil {
		t.Fatalf("Second addFolder failed: %v", err)
	}

	// Should still only have 1 entry
	folders := readFolders(t, tmpDir)
	if len(folders) != 1 {
		t.Fatalf("Expected 1 folder after case-insensitive duplicate, got %d", len(folders))
	}
}

func TestAddFolder_TrailingSlashDuplicate(t *testing.T) {
	tmpDir := setupTestDir(t)
	defer cleanupTestDir(t, tmpDir)

	// Add first time with trailing backslash
	err := addFolder(`c:\test\`, tmpDir)
	if err != nil {
		t.Fatalf("First addFolder failed: %v", err)
	}

	// Add second time without trailing backslash (should be detected as duplicate)
	err = addFolder(`c:\test`, tmpDir)
	if err != nil {
		t.Fatalf("Second addFolder failed: %v", err)
	}

	// Should still only have 1 entry
	folders := readFolders(t, tmpDir)
	if len(folders) != 1 {
		t.Fatalf("Expected 1 folder after trailing slash duplicate, got %d: %v", len(folders), folders)
	}
}

func TestAddFolder_ReverseTrailingSlashDuplicate(t *testing.T) {
	tmpDir := setupTestDir(t)
	defer cleanupTestDir(t, tmpDir)

	// Add first time without trailing backslash
	err := addFolder(`c:\test`, tmpDir)
	if err != nil {
		t.Fatalf("First addFolder failed: %v", err)
	}

	// Add second time with trailing backslash (should be detected as duplicate)
	err = addFolder(`c:\test\`, tmpDir)
	if err != nil {
		t.Fatalf("Second addFolder failed: %v", err)
	}

	// Should still only have 1 entry
	folders := readFolders(t, tmpDir)
	if len(folders) != 1 {
		t.Fatalf("Expected 1 folder after trailing slash duplicate, got %d: %v", len(folders), folders)
	}
}

func TestAddFolder_MultipleDifferentFolders(t *testing.T) {
	tmpDir := setupTestDir(t)
	defer cleanupTestDir(t, tmpDir)

	folders := []string{`c:\test1`, `c:\test2`, `c:\test3`}

	for _, folder := range folders {
		err := addFolder(folder, tmpDir)
		if err != nil {
			t.Fatalf("addFolder failed for %s: %v", folder, err)
		}
	}

	savedFolders := readFolders(t, tmpDir)
	if len(savedFolders) != len(folders) {
		t.Fatalf("Expected %d folders, got %d", len(folders), len(savedFolders))
	}
}

func TestAddFolder_CreatesDirIfNotExists(t *testing.T) {
	tmpDir := setupTestDir(t)
	defer cleanupTestDir(t, tmpDir)

	// Don't create .q2 directory beforehand
	folder := `c:\test`
	err := addFolder(folder, tmpDir)
	if err != nil {
		t.Fatalf("addFolder failed: %v", err)
	}

	// Check that folders.txt was created
	filePath := filepath.Join(tmpDir, "folders.txt")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("folders.txt was not created")
	}
}

func TestAddFolder_WhitespaceHandling(t *testing.T) {
	tmpDir := setupTestDir(t)
	defer cleanupTestDir(t, tmpDir)

	// Add folder with leading/trailing whitespace
	err := addFolder("  c:\\test  ", tmpDir)
	if err != nil {
		t.Fatalf("addFolder failed: %v", err)
	}

	folders := readFolders(t, tmpDir)
	if len(folders) != 1 {
		t.Fatalf("Expected 1 folder, got %d", len(folders))
	}

	// Should be cleaned (no whitespace)
	if folders[0] != `c:\test` {
		t.Errorf("Expected c:\\test, got %s", folders[0])
	}
}
