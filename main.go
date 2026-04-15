package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"jukel.org/q2/cast"
	"jukel.org/q2/ffmpeg"
	_ "jukel.org/q2/migrations"
	"jukel.org/q2/scanner"
)


func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  %s <command> [options]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  addfolder	Add a folder to Q2\n")
		fmt.Fprintf(os.Stderr, "  removefolder	Remove a folder from Q2\n")
		fmt.Fprintf(os.Stderr, "  listfolders	List stored folders\n")
		fmt.Fprintf(os.Stderr, "  scan		Scan a folder for files\n")
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

	case "scan":
		scanCmd := flag.NewFlagSet("scan", flag.ContinueOnError)

		scanCmd.Usage = func() {
			fmt.Fprintf(os.Stderr, "Usage: \n")
			fmt.Fprintf(os.Stderr, "  %s scan <folder>\n\n", os.Args[0])
			fmt.Fprintf(os.Stderr, "Scans a folder for files and adds them to the database.\n")
			fmt.Fprintf(os.Stderr, "The folder must be within a monitored folder.\n\n")
			scanCmd.PrintDefaults()
		}

		if err := scanCmd.Parse(os.Args[2:]); err != nil {
			scanCmd.Usage()
			os.Exit(2)
		}

		args := scanCmd.Args()

		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "scan requires exactly one <folder>")
			scanCmd.Usage()
			os.Exit(2)
		}

		folder := args[0]

		// Clean and validate the folder path
		folder, ok := cleanPath(folder)
		if !ok {
			fmt.Fprintln(os.Stderr, "Error: folder cannot be empty")
			os.Exit(1)
		}

		// Check if folder exists
		info, err := os.Stat(folder)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "Error: folder does not exist: %s\n", folder)
			} else {
				fmt.Fprintf(os.Stderr, "Error: cannot access folder: %v\n", err)
			}
			os.Exit(1)
		}
		if !info.IsDir() {
			fmt.Fprintf(os.Stderr, "Error: path is not a directory: %s\n", folder)
			os.Exit(1)
		}

		database, err := initDB(q2Dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error initializing database:", err)
			os.Exit(1)
		}
		defer database.Close()

		// Find the parent monitored folder
		parentPath, folderID, err := scanner.FindParentFolder(database, folder)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Scanning %s (monitored folder: %s)...\n", folder, parentPath)

		// Perform the scan
		result, err := scanner.ScanFolder(database, folder, folderID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error scanning folder: %v\n", err)
			os.Exit(1)
		}

		// Report results
		fmt.Printf("Scan complete: %d added, %d updated, %d removed\n",
			result.FilesAdded, result.FilesUpdated, result.FilesRemoved)

		if len(result.Errors) > 0 {
			fmt.Printf("%d errors encountered:\n", len(result.Errors))
			for _, e := range result.Errors {
				fmt.Printf("  - %v\n", e)
			}
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

		// Ensure playlists folder exists and is monitored
		playlistDir, err := ensurePlaylistsFolder(q2Dir, database)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Warning: could not initialize playlists folder:", err)
			playlistDir = filepath.Join(q2Dir, "playlists") // Use default path anyway
		}

		// Create cast manager - base URL will be set when first request comes in
		castMgr := cast.NewManager("")

		// Create ffmpeg manager for video transcoding
		ffmpegBinDir := filepath.Join(q2Dir, "bin")
		ffmpegMgr := ffmpeg.NewManager(ffmpegBinDir)

		// Set up HTTP handlers
		mux := http.NewServeMux()
		mux.HandleFunc("/", homeEndpoint)
		mux.HandleFunc("/browse", browsePageHandler)
		mux.HandleFunc("/albums", albumsPageHandler)
		mux.HandleFunc("/music", musicPageHandler)
		mux.HandleFunc("/schema", makeSchemaHandler(database))
		mux.HandleFunc("/api/roots", makeRootsHandler(database))
		mux.HandleFunc("/api/browse", makeBrowseHandler(database, q2Dir))
		mux.HandleFunc("/api/stream", makeStreamHandler(database))
		mux.HandleFunc("/api/image", makeImageHandler(database))
		mux.HandleFunc("/api/thumbnail", makeThumbnailHandler(database, q2Dir))
		mux.HandleFunc("/api/video", makeVideoHandler(database, ffmpegMgr))

		// Cast API endpoints
		mux.HandleFunc("/api/cast/devices", makeCastDevicesHandler(castMgr))
		mux.HandleFunc("/api/cast/connect", makeCastConnectHandler(castMgr))
		mux.HandleFunc("/api/cast/disconnect", makeCastDisconnectHandler(castMgr))
		mux.HandleFunc("/api/cast/play", makeCastPlayHandler(castMgr))
		mux.HandleFunc("/api/cast/pause", makeCastPauseHandler(castMgr))
		mux.HandleFunc("/api/cast/resume", makeCastResumeHandler(castMgr))
		mux.HandleFunc("/api/cast/stop", makeCastStopHandler(castMgr))
		mux.HandleFunc("/api/cast/seek", makeCastSeekHandler(castMgr))
		mux.HandleFunc("/api/cast/volume", makeCastVolumeHandler(castMgr))
		mux.HandleFunc("/api/cast/status", makeCastStatusHandler(castMgr))

		// Playlist API endpoints
		mux.HandleFunc("/api/playlists", makePlaylistsHandler(playlistDir))
		mux.HandleFunc("/api/playlist", makePlaylistHandler(playlistDir, database))
		mux.HandleFunc("/api/playlist/add", makePlaylistAddHandler(playlistDir))
		mux.HandleFunc("/api/playlist/remove", makePlaylistRemoveHandler(playlistDir))
		mux.HandleFunc("/api/playlist/reorder", makePlaylistReorderHandler(playlistDir))
		mux.HandleFunc("/api/playlist/check", makePlaylistCheckHandler(playlistDir))

		// Album endpoints
		mux.HandleFunc("/api/albums", makeAlbumsHandler(database))
		mux.HandleFunc("/api/album", makeAlbumHandler(database))
		mux.HandleFunc("/api/album/add", makeAlbumAddHandler(database))
		mux.HandleFunc("/api/album/remove", makeAlbumRemoveHandler(database))
		mux.HandleFunc("/api/album/reorder", makeAlbumReorderHandler(database))
		mux.HandleFunc("/api/album/check", makeAlbumCheckHandler(database))

		// Music library API endpoints
		mux.HandleFunc("/api/music/artists", makeMusicArtistsHandler(database))
		mux.HandleFunc("/api/music/albums", makeMusicAlbumsHandler(database))
		mux.HandleFunc("/api/music/genres", makeMusicGenresHandler(database))
		mux.HandleFunc("/api/music/songs", makeMusicSongsHandler(database))
		mux.HandleFunc("/api/music/top", makeTopSongsHandler(database))
		mux.HandleFunc("/api/history/record", makeRecordPlayHandler(database))
		mux.HandleFunc("/api/favourites", makeFavouritesHandler(database))
		mux.HandleFunc("/api/lyrics", makeLyricsHandler(database))

		// Metadata refresh endpoints
		mux.HandleFunc("/api/metadata/refresh", makeMetadataRefreshHandler(database, q2Dir, ffmpegMgr))
		mux.HandleFunc("/api/metadata/status", makeMetadataStatusHandler())
		mux.HandleFunc("/api/metadata/queue", makeMetadataQueueRemoveHandler())
		mux.HandleFunc("/api/metadata/queue/prioritize", makeMetadataQueuePrioritizeHandler())
		mux.HandleFunc("/api/metadata/cancel", makeMetadataCancelHandler())

		// Settings and folder management endpoints
		mux.HandleFunc("/settings", settingsPageHandler)
		settingsGet := makeSettingsGetHandler(database)
		settingsPost := makeSettingsPostHandler(database)
		mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				settingsGet(w, r)
			} else {
				settingsPost(w, r)
			}
		})
		mux.HandleFunc("/api/folders/add", makeFolderAddHandler(database))
		mux.HandleFunc("/api/folders/remove", makeFolderRemoveHandler(database))

		// Inbox endpoints
		mux.HandleFunc("/api/inbox/upload", makeInboxUploadHandler(database, q2Dir, ffmpegMgr))
		mux.HandleFunc("/api/inbox/status", makeInboxStatusHandler())
		mux.HandleFunc("/api/inbox/clear", makeInboxClearHandler())

		// Middleware: keep the cast manager's base URL in sync with each request's host.
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			castMgr.SetBaseURL(fmt.Sprintf("%s://%s", scheme, r.Host))
			mux.ServeHTTP(w, r)
		})

		addr := fmt.Sprintf(":%d", *port)
		server := &http.Server{
			Addr:    addr,
			Handler: handler,
		}

		// Handle shutdown signals
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		// Start server in goroutine
		go func() {
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				fmt.Fprintln(os.Stderr, "Server error:", err)
				os.Exit(1)
			}
		}()

		fmt.Printf("Listening on port %s\n", addr)

		// Wait for shutdown signal
		<-sigChan
		fmt.Println("\nShutting down...")

		// Shutdown HTTP server with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "Server shutdown error:", err)
		}

		fmt.Println("Shutdown complete")

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		flag.Usage()
		os.Exit(2)

	}

}
