package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// cleanPath trims whitespace and removes stray quote characters from shell escaping issues.
// Returns the cleaned path and true if non-empty, or empty string and false if empty.
func cleanPath(path string) (string, bool) {
	path = strings.TrimSpace(path)
	path = strings.Trim(path, `"'`)
	if path == "" {
		return "", false
	}
	return filepath.Clean(path), true
}

// normalizePath cleans the path and applies platform-specific normalization.
// On Windows, paths are lowercased for case-insensitive comparison.
// On Linux/macOS, paths are kept as-is for case-sensitive comparison.
func normalizePath(path string) string {
	path, _ = cleanPath(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

// sanitizePlaylistName sanitizes a playlist name to be a valid filename.
func sanitizePlaylistName(name string) string {
	// Remove or replace invalid filename characters
	invalid := []string{"<", ">", ":", "\"", "/", "\\", "|", "?", "*"}
	result := name
	for _, char := range invalid {
		result = strings.ReplaceAll(result, char, "_")
	}
	// Trim spaces and dots from ends
	result = strings.Trim(result, " .")
	if result == "" {
		result = "Untitled"
	}
	return result
}

// sanitizeFolderName removes characters that are invalid in folder names.
func sanitizeFolderName(name string) string {
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_",
		"?", "_", "\"", "_", "<", "_", ">", "_", "|", "_",
	)
	result := strings.TrimSpace(replacer.Replace(name))
	if result == "" {
		return "_"
	}
	return result
}

// moveFile moves a file from src to dst, falling back to copy+delete if rename fails (cross-device).
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Cross-device fallback: copy then delete
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	out.Close()
	in.Close()
	return os.Remove(src)
}
