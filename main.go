package main

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"jukel.org/q2/db"
	_ "jukel.org/q2/migrations"
)

const (
	q2Dir  = ".q2"
	dbFile = "q2.db"
)

// homeEndpoint responds with a simple "Q2" message.
func homeEndpoint(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(w, "Q2")
}

// initDB initializes the database and runs migrations.
func initDB(baseDir string) (*db.DB, error) {
	// Ensure .q2 directory exists
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", baseDir, err)
	}

	dbPath := filepath.Join(baseDir, dbFile)
	database, err := db.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := database.Migrate(); err != nil {
		database.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return database, nil
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

// addFolder adds the given folder path to the database.
// It ensures the folder exists and no duplicate entries are added.
// Case sensitivity matches the platform (case-insensitive on Windows, case-sensitive on Linux).
// Returns an error if the folder is empty, doesn't exist, or a database error occurs.
func addFolder(folder string, database *db.DB) error {
	folder, ok := cleanPath(folder)
	if !ok {
		return errors.New("folder cannot be empty")
	}

	// Check if folder exists
	info, err := os.Stat(folder)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("folder does not exist: %s", folder)
		}
		return fmt.Errorf("cannot access folder: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", folder)
	}

	// Normalize path for storage (lowercase on Windows)
	normalizedPath := normalizePath(folder)

	// Try to insert - will fail if duplicate due to UNIQUE constraint
	result := database.Write(
		"INSERT OR IGNORE INTO folders (path) VALUES (?)",
		normalizedPath,
	)
	if result.Err != nil {
		return result.Err
	}

	if result.RowsAffected == 0 {
		fmt.Printf("Folder %s already exists\n", folder)
	} else {
		fmt.Printf("Folder %s added\n", folder)
	}

	return nil
}

// removeFolder removes a folder from the database.
// Returns an error if the folder is empty or not found.
func removeFolder(folder string, database *db.DB) error {
	folder, ok := cleanPath(folder)
	if !ok {
		return errors.New("folder cannot be empty")
	}

	normalizedPath := normalizePath(folder)

	result := database.Write("DELETE FROM folders WHERE path = ?", normalizedPath)
	if result.Err != nil {
		return result.Err
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("folder not found: %s", folder)
	}

	fmt.Printf("Folder %s removed\n", folder)
	return nil
}

// listFolders retrieves and displays all stored folders from the database.
func listFolders(database *db.DB) error {
	rows, err := database.Query("SELECT path FROM folders ORDER BY path")
	if err != nil {
		return fmt.Errorf("failed to query folders: %w", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return fmt.Errorf("failed to read folder: %w", err)
		}
		fmt.Println(path)
		count++
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error reading folders: %w", err)
	}

	if count == 0 {
		fmt.Println("No folders stored")
	}

	return nil
}

// main parses subcommands and dispatches to the appropriate handler.
// Supported commands: addfolder, removefolder, listfolders, serve
func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  %s <command> [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  addfolder	Add a folder to Q2\n")
		fmt.Fprintf(os.Stderr, "  removefolder	Remove a folder from Q2\n")
		fmt.Fprintf(os.Stderr, "  listfolders	List stored folders\n")
		fmt.Fprintf(os.Stderr, "  serve		Start serving Q2\n")
	}

	if len(os.Args) < 2 {
		flag.Usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "addfolder":
		addFolderCmd := flag.NewFlagSet("addfolder", flag.ContinueOnError)

		addFolderCmd.Usage = func() {
			fmt.Fprintf(os.Stderr, "Usage: \n")
			fmt.Fprintf(os.Stderr, "  %s addfolder <folder>\n\n", os.Args[0])
			addFolderCmd.PrintDefaults()
		}
		if err := addFolderCmd.Parse(os.Args[2:]); err != nil {
			addFolderCmd.Usage()
			os.Exit(2)
		}

		args := addFolderCmd.Args()

		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "addfolder requires exactly one <folder>")
			addFolderCmd.Usage()
			os.Exit(2)
		}

		folder := args[0]

		database, err := initDB(q2Dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error initializing database:", err)
			os.Exit(1)
		}
		defer database.Close()

		if err := addFolder(folder, database); err != nil {
			fmt.Fprintln(os.Stderr, "Error adding folder:", err)
			os.Exit(1)
		}

	case "removefolder":
		removeFolderCmd := flag.NewFlagSet("removefolder", flag.ContinueOnError)

		removeFolderCmd.Usage = func() {
			fmt.Fprintf(os.Stderr, "Usage: \n")
			fmt.Fprintf(os.Stderr, "  %s removefolder <folder>\n\n", os.Args[0])
			removeFolderCmd.PrintDefaults()
		}
		if err := removeFolderCmd.Parse(os.Args[2:]); err != nil {
			removeFolderCmd.Usage()
			os.Exit(2)
		}

		args := removeFolderCmd.Args()

		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "removefolder requires exactly one <folder>")
			removeFolderCmd.Usage()
			os.Exit(2)
		}

		folder := args[0]

		database, err := initDB(q2Dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error initializing database:", err)
			os.Exit(1)
		}
		defer database.Close()

		if err := removeFolder(folder, database); err != nil {
			fmt.Fprintln(os.Stderr, "Error removing folder:", err)
			os.Exit(1)
		}

	case "listfolders":
		database, err := initDB(q2Dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error initializing database:", err)
			os.Exit(1)
		}
		defer database.Close()

		if err := listFolders(database); err != nil {
			fmt.Fprintln(os.Stderr, "Error listing folders:", err)
			os.Exit(1)
		}

	case "serve":
		serveCmd := flag.NewFlagSet("serve", flag.ContinueOnError)
		port := serveCmd.Int("port", 8090, "Port to listen on")

		serveCmd.Usage = func() {
			fmt.Fprintf(os.Stderr, "Usage: \n")
			fmt.Fprintf(os.Stderr, "  %s serve [options]\n\n", os.Args[0])
			fmt.Fprintf(os.Stderr, "Options:\n")
			serveCmd.PrintDefaults()
		}

		if err := serveCmd.Parse(os.Args[2:]); err != nil {
			serveCmd.Usage()
			os.Exit(2)
		}

		database, err := initDB(q2Dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error initializing database:", err)
			os.Exit(1)
		}
		defer database.Close()

		fmt.Println("Q2")

		http.HandleFunc("/", homeEndpoint)

		addr := fmt.Sprintf(":%d", *port)
		fmt.Printf("Listening on port %s\n", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			fmt.Fprintln(os.Stderr, "Server error:", err)
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		flag.Usage()
		os.Exit(2)

	}

}
