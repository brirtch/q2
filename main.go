package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// homeEndpoint responds with a simple "Q2" message.
func homeEndpoint(w http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(w, "Q2")
}

// addFolder adds the given folder path to .q2/folders.txt.
// It creates the file if it doesn't exist and ensures no duplicate entries
// (case-insensitive). Returns an error if the folder is empty or an I/O error occurs.
func addFolder(folder string, baseDir string) error {
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return errors.New("folder cannot be empty")
	}

	folder = filepath.Clean(folder)

	q2Dir := ".q2"
	filePath := filepath.Join(baseDir, "folders.txt")

	// Ensure .q2 directory exists
	if err := os.MkdirAll(q2Dir, 0755); err != nil {
		return err
	}

	// Check if file exists and read existing entries
	existing := make(map[string]struct{})

	if file, err := os.Open(filePath); err == nil {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				existing[strings.ToLower(filepath.Clean(line))] = struct{}{}
			}
		}
		file.Close()
		if err := scanner.Err(); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	// Case-insensitive duplicate check
	if _, found := existing[strings.ToLower(folder)]; found {
		fmt.Printf("Folder %s already exists in folders.txt\n", folder)
		return nil // already present, do nothing
	}

	// Append folder
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(folder + "\n")
	if err == nil {
		fmt.Printf("Folder %s added to folders.txt\n", folder)
	}
	return err
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

		if err := addFolder(folder, ".q2"); err != nil {
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
