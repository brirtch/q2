package main

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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

// addFolder adds the given folder path to the database.
// It ensures no duplicate entries (case-insensitive due to COLLATE NOCASE on the column).
// Returns an error if the folder is empty or a database error occurs.
func addFolder(folder string, database *db.DB) error {
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return errors.New("folder cannot be empty")
	}

	folder = filepath.Clean(folder)

	// Try to insert - will fail if duplicate due to UNIQUE constraint
	result := database.Write(
		"INSERT OR IGNORE INTO folders (path) VALUES (?)",
		folder,
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

// main parses subcommands and dispatches to the appropriate handler.
// Supported commands: addfolder, serve
func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  %s <command> [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  addfolder	Add a folder to Q2\n")
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

	case "serve":
		serveCmd := flag.NewFlagSet("serve", flag.ContinueOnError)

		serveCmd.Usage = func() {
			fmt.Fprintf(os.Stderr, "Usage: \n")
			fmt.Fprintf(os.Stderr, "  %s serve\n\n", os.Args[0])
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

		fmt.Println("Listening on port :8090")
		if err := http.ListenAndServe(":8090", nil); err != nil {
			fmt.Fprintln(os.Stderr, "Server error:", err)
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		flag.Usage()
		os.Exit(2)

	}

}
