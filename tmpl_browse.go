package main

import (
	"net/http"
)

const browsePageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Q2 File Browser</title>
    <script src="https://unpkg.com/vue@3/dist/vue.global.prod.js"></script>
    <style>
        * { box-sizing: border-box; }
        body { font-family: "Cascadia Code", "Fira Code", "JetBrains Mono", "SF Mono", Consolas, monospace; margin: 0; padding: 20px; padding-bottom: 100px; background: #0d1117; color: #c9d1d9; }

        /* Two-pane layout */
        .panes-wrapper { display: flex; gap: 20px; }
        .panes-wrapper.single-pane .pane { flex: 1; }
        .panes-wrapper.dual-pane .pane { flex: 1; min-width: 0; }
        .pane { background: #161b22; border-radius: 6px; border: 1px solid #30363d; overflow: hidden; display: flex; flex-direction: column; }
        .pane-content { flex: 1; overflow-y: auto; max-height: calc(100vh - 250px); }
        .pane table { width: 100%; }

        /* Header with toggle */
        .header-bar { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
        .header-bar h1 { margin: 0; font-size: 22px; color: #58a6ff; }
        .view-toggle { background: #21262d; border: 1px solid #30363d; color: #c9d1d9; padding: 8px 16px; border-radius: 6px; cursor: pointer; font-family: inherit; font-size: 14px; display: flex; align-items: center; gap: 8px; }
        .view-toggle:hover { background: #30363d; border-color: #484f58; }
        .view-toggle.active { background: #1f6feb; border-color: #1f6feb; color: white; }
        .view-toggle.small { padding: 4px 10px; font-size: 11px; }
        .pane-actions { display: flex; gap: 8px; margin-left: auto; }

        .container { max-width: 1200px; margin: 0 auto; background: #161b22; border-radius: 6px; border: 1px solid #30363d; }
        .panes-wrapper.dual-pane { max-width: 100%; }
        h1 { margin: 0; padding: 20px; border-bottom: 1px solid #30363d; font-size: 22px; color: #58a6ff; }
        .breadcrumb { padding: 15px 20px; background: #0d1117; border-bottom: 1px solid #30363d; }
        .breadcrumb a { color: #58a6ff; text-decoration: none; cursor: pointer; }
        .breadcrumb a:hover { text-decoration: underline; }
        .breadcrumb span.sep { color: #484f58; margin: 0 8px; }
        table { width: 100%; border-collapse: collapse; }
        th, td { padding: 12px 20px; text-align: left; border-bottom: 1px solid #21262d; }
        th { background: #0d1117; cursor: pointer; user-select: none; font-weight: 600; color: #8b949e; }
        th:hover { background: #1f2428; }
        th .sort-indicator { margin-left: 5px; color: #484f58; }
        tr:hover { background: #1f2428; }
        .name-cell { display: flex; align-items: center; gap: 10px; }
        .icon { font-size: 18px; }
        .folder-link { color: #58a6ff; text-decoration: none; cursor: pointer; }
        .folder-link:hover { text-decoration: underline; }
        .file-name { color: #c9d1d9; }
        .size-cell, .modified-cell { color: #8b949e; }
        .type-cell { color: #6e7681; text-transform: uppercase; font-size: 11px; }
        .empty-message { padding: 40px; text-align: center; color: #8b949e; }
        .error-message { padding: 40px; text-align: center; color: #f85149; }
        .loading { padding: 40px; text-align: center; color: #8b949e; }
        .roots-list { padding: 20px; }
        .root-item { display: flex; align-items: center; gap: 10px; padding: 15px; border: 1px solid #30363d; border-radius: 6px; margin-bottom: 10px; cursor: pointer; background: #0d1117; }
        .root-item:hover { background: #1f2428; border-color: #58a6ff; }
        .root-item .icon { font-size: 24px; }
        .root-item .path { color: #8b949e; font-size: 13px; }
        .stats-bar { padding: 10px 20px; background: #0d1117; border-bottom: 1px solid #30363d; color: #8b949e; font-size: 13px; }
        .stats-bar .stat { margin-right: 20px; }
        .stats-bar .stat-value { font-weight: 600; color: #58a6ff; }

        /* Audio controls in file list */
        .audio-controls { display: flex; gap: 4px; margin-left: auto; }
        .audio-btn { background: none; border: 1px solid #30363d; border-radius: 4px; padding: 4px 8px; cursor: pointer; font-size: 11px; transition: all 0.2s; color: #8b949e; }
        .audio-btn:hover { background: #238636; color: white; border-color: #238636; }
        .audio-btn.play { background: #238636; color: white; border-color: #238636; }
        .audio-btn.play:hover { background: #2ea043; }

        /* Audio Player */
        .audio-player { position: fixed; bottom: 0; left: 0; right: 0; background: #1a1a2e; color: white; padding: 12px 20px; display: flex; align-items: center; gap: 15px; z-index: 1000; box-shadow: 0 -2px 10px rgba(0,0,0,0.3); }
        .audio-player.hidden { display: none; }
        .player-controls { display: flex; align-items: center; gap: 8px; }
        .player-btn { background: none; border: none; color: white; font-size: 20px; cursor: pointer; padding: 8px; border-radius: 50%; transition: background 0.2s; }
        .player-btn:hover { background: rgba(255,255,255,0.1); }
        .player-btn.play-pause { font-size: 22px; background: #0066cc; width: 44px; height: 44px; padding: 0; display: flex; align-items: center; justify-content: center; }
        .player-btn.play-pause:hover { background: #0052a3; }
        .track-info { flex: 1; min-width: 0; }
        .track-name { font-weight: 500; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
        .track-artist { font-size: 12px; color: #aaa; }
        .progress-container { flex: 2; display: flex; align-items: center; gap: 10px; }
        .progress-bar { flex: 1; height: 6px; background: #333; border-radius: 3px; cursor: pointer; position: relative; }
        .progress-fill { height: 100%; background: #0066cc; border-radius: 3px; transition: width 0.1s; }
        .time-display { font-size: 12px; color: #aaa; min-width: 90px; text-align: center; }
        .player-right { display: flex; align-items: center; gap: 10px; }
        .crossfade-toggle { display: flex; align-items: center; gap: 5px; font-size: 12px; color: #aaa; cursor: pointer; }
        .crossfade-toggle input { cursor: pointer; }
        .crossfade-toggle.active { color: #0066cc; }
        .queue-btn { position: relative; }
        .queue-count { position: absolute; top: -5px; right: -5px; background: #0066cc; color: white; font-size: 10px; padding: 2px 6px; border-radius: 10px; }

        /* Queue Panel */
        .queue-panel { position: fixed; bottom: 70px; right: 20px; width: 350px; max-height: 400px; background: #161b22; border: 1px solid #30363d; border-radius: 6px; box-shadow: 0 4px 20px rgba(0,0,0,0.4); z-index: 999; overflow: hidden; }
        .queue-panel.hidden { display: none; }
        .queue-header { padding: 15px; background: #0d1117; border-bottom: 1px solid #30363d; display: flex; justify-content: space-between; align-items: center; }
        .queue-header h3 { margin: 0; font-size: 14px; color: #c9d1d9; }
        .queue-clear { background: none; border: none; color: #f85149; cursor: pointer; font-size: 12px; }
        .queue-clear:hover { text-decoration: underline; }
        .queue-list { max-height: 320px; overflow-y: auto; }
        .queue-item { display: flex; align-items: center; padding: 10px 15px; border-bottom: 1px solid #21262d; gap: 10px; color: #c9d1d9; }
        .queue-item:hover { background: #1f2428; }
        .queue-item.playing { background: #1f6feb22; }
        .queue-item .num { color: #6e7681; font-size: 11px; width: 20px; }
        .queue-item .name { flex: 1; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-size: 13px; }
        .queue-item .remove { background: none; border: none; color: #6e7681; cursor: pointer; font-size: 16px; }
        .queue-item .remove:hover { color: #f85149; }
        .queue-item .move-btns { display: flex; flex-direction: column; gap: 2px; }
        .queue-item .move-btn { background: none; border: none; color: #6e7681; cursor: pointer; font-size: 10px; padding: 0; line-height: 1; }
        .queue-item .move-btn:hover { color: #58a6ff; }
        .queue-empty { padding: 30px; text-align: center; color: #6e7681; }

        /* Cast Button and Panel */
        .cast-btn { position: relative; }
        .cast-btn.casting { color: #58a6ff; }
        .cast-panel { position: fixed; bottom: 70px; right: 100px; width: 280px; background: #161b22; border: 1px solid #30363d; border-radius: 6px; box-shadow: 0 4px 20px rgba(0,0,0,0.4); z-index: 999; overflow: hidden; }
        .cast-panel.hidden { display: none; }
        .cast-header { padding: 15px; background: #0d1117; border-bottom: 1px solid #30363d; display: flex; justify-content: space-between; align-items: center; }
        .cast-header h3 { margin: 0; font-size: 14px; color: #c9d1d9; }
        .cast-refresh { background: none; border: 1px solid #30363d; color: #58a6ff; padding: 4px 10px; border-radius: 4px; cursor: pointer; font-size: 12px; }
        .cast-refresh:hover { background: #30363d; }
        .cast-scanning { font-size: 12px; color: #8b949e; }
        .cast-list { max-height: 300px; overflow-y: auto; }
        .cast-device { display: flex; align-items: center; padding: 12px 15px; border-bottom: 1px solid #21262d; gap: 10px; color: #c9d1d9; cursor: pointer; }
        .cast-device:hover { background: #1f2428; }
        .cast-device.active { background: #1f6feb22; color: #58a6ff; }
        .cast-device .icon { font-size: 18px; }
        .cast-device .name { flex: 1; font-size: 13px; }
        .cast-device .status { font-size: 11px; color: #6e7681; }
        .cast-searching { padding: 20px; text-align: center; color: #6e7681; }
        .cast-unavailable { padding: 20px; text-align: center; color: #6e7681; font-size: 13px; }

        /* Image Viewer */
        .image-viewer { position: fixed; top: 0; left: 0; right: 0; bottom: 80px; background: rgba(0,0,0,0.95); z-index: 998; display: flex; flex-direction: column; }
        .image-viewer.hidden { display: none; }
        .image-viewer-header { display: flex; justify-content: space-between; align-items: center; padding: 15px 20px; background: #161b22; border-bottom: 1px solid #30363d; }
        .image-viewer-title { color: #c9d1d9; font-size: 14px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
        .image-viewer-close { background: none; border: 1px solid #30363d; color: #c9d1d9; padding: 8px 16px; border-radius: 6px; cursor: pointer; font-size: 14px; }
        .image-viewer-close:hover { background: #30363d; }
        .image-viewer-content { flex: 1; display: flex; align-items: center; justify-content: center; padding: 20px; overflow: hidden; }
        .image-viewer-content img { max-width: 100%; max-height: 100%; object-fit: contain; }
        .image-viewer-nav { position: absolute; top: 50%; transform: translateY(-50%); background: rgba(0,0,0,0.7); border: 1px solid #30363d; color: #c9d1d9; padding: 20px 15px; cursor: pointer; font-size: 24px; }
        .image-viewer-nav:hover { background: rgba(88,166,255,0.3); }
        .image-viewer-nav.prev { left: 10px; }
        .image-viewer-nav.next { right: 10px; }
        .image-viewer-nav:disabled { opacity: 0.3; cursor: not-allowed; }
        .image-viewer-nav:disabled:hover { background: rgba(0,0,0,0.7); }
        .file-name.image-file { color: #58a6ff; cursor: pointer; }
        .file-name.image-file:hover { text-decoration: underline; }

        /* Video Player */
        .video-player { position: fixed; top: 0; left: 0; right: 0; bottom: 80px; background: rgba(0,0,0,0.98); z-index: 998; display: flex; flex-direction: column; }
        .video-player.hidden { display: none; }
        .video-player-header { display: flex; justify-content: space-between; align-items: center; padding: 15px 20px; background: #161b22; border-bottom: 1px solid #30363d; }
        .video-player-title { color: #c9d1d9; font-size: 14px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
        .video-player-close { background: none; border: 1px solid #30363d; color: #c9d1d9; padding: 8px 16px; border-radius: 6px; cursor: pointer; font-size: 14px; }
        .video-player-close:hover { background: #30363d; }
        .video-player-content { flex: 1; display: flex; align-items: center; justify-content: center; padding: 20px; overflow: hidden; background: #000; }
        .video-player-content video { max-width: 100%; max-height: 100%; }
        .video-controls { display: flex; align-items: center; gap: 15px; padding: 15px 20px; background: #161b22; border-top: 1px solid #30363d; }
        .video-btn { background: none; border: none; color: #c9d1d9; font-size: 24px; cursor: pointer; padding: 8px; border-radius: 4px; }
        .video-btn:hover { background: #30363d; }
        .video-btn.play-pause { font-size: 22px; background: #238636; color: white; width: 44px; height: 44px; padding: 0; border-radius: 50%; display: flex; align-items: center; justify-content: center; }
        .video-btn.play-pause:hover { background: #2ea043; }
        .video-progress-container { flex: 1; display: flex; align-items: center; gap: 10px; }
        .video-progress-bar { flex: 1; height: 8px; background: #30363d; border-radius: 4px; cursor: pointer; position: relative; }
        .video-progress-fill { height: 100%; background: #238636; border-radius: 4px; transition: width 0.1s; }
        .video-time { font-size: 13px; color: #8b949e; min-width: 100px; text-align: center; }
        .file-name.video-file { color: #f0883e; cursor: pointer; }
        .file-name.video-file:hover { text-decoration: underline; }
        .video-btn.casting { color: #58a6ff; }
        .video-btn:disabled { opacity: 0.3; cursor: not-allowed; }
        .video-casting-indicator { display: flex; align-items: center; justify-content: center; gap: 15px; padding: 10px 20px; background: #1f6feb33; border-top: 1px solid #1f6feb; color: #58a6ff; font-size: 14px; }
        .stop-casting-btn { background: #30363d; border: 1px solid #484f58; color: #c9d1d9; padding: 5px 12px; border-radius: 4px; cursor: pointer; font-size: 13px; }
        .stop-casting-btn:hover { background: #484f58; }

        /* Playlist Popup */
        .playlist-popup { position: fixed; background: #161b22; border: 1px solid #30363d; border-radius: 6px; box-shadow: 0 4px 20px rgba(0,0,0,0.4); z-index: 1001; min-width: 220px; max-width: 300px; }
        .playlist-popup.hidden { display: none; }
        .playlist-popup-header { display: flex; justify-content: space-between; align-items: center; padding: 10px 15px; border-bottom: 1px solid #30363d; }
        .playlist-popup-header span { color: #c9d1d9; font-size: 13px; font-weight: 600; }
        .playlist-popup-header button { background: none; border: none; color: #6e7681; cursor: pointer; font-size: 18px; padding: 0; line-height: 1; }
        .playlist-popup-header button:hover { color: #c9d1d9; }
        .playlist-popup-list { max-height: 250px; overflow-y: auto; }
        .playlist-popup-item { padding: 10px 15px; cursor: pointer; color: #c9d1d9; display: flex; justify-content: space-between; align-items: center; font-size: 13px; }
        .playlist-popup-item:hover { background: #1f2428; }
        .already-here { color: #8b949e; font-size: 11px; margin-left: 8px; }
        .playlist-popup-empty { padding: 15px; text-align: center; color: #6e7681; font-size: 13px; }
        .playlist-popup-new { padding: 10px 15px; cursor: pointer; color: #58a6ff; border-top: 1px solid #30363d; font-size: 13px; }
        .playlist-popup-new:hover { background: #1f2428; }

        /* Playlist Viewer */
        .playlist-viewer { position: fixed; top: 0; left: 0; right: 0; bottom: 80px; background: rgba(0,0,0,0.9); z-index: 998; display: flex; align-items: center; justify-content: center; }
        .playlist-viewer.hidden { display: none; }
        .playlist-viewer-content { background: #161b22; border: 1px solid #30363d; border-radius: 8px; width: 90%; max-width: 600px; max-height: 80%; display: flex; flex-direction: column; }
        .playlist-viewer-header { display: flex; justify-content: space-between; align-items: center; padding: 15px 20px; border-bottom: 1px solid #30363d; }
        .playlist-viewer-header h2 { margin: 0; color: #c9d1d9; font-size: 18px; }
        .playlist-viewer-header .close-btn { background: none; border: 1px solid #30363d; color: #c9d1d9; width: 32px; height: 32px; border-radius: 6px; cursor: pointer; font-size: 18px; }
        .playlist-viewer-header .close-btn:hover { background: #30363d; }
        .playlist-viewer-actions { display: flex; gap: 10px; padding: 15px 20px; border-bottom: 1px solid #30363d; }
        .playlist-viewer-actions button { background: #238636; border: none; color: white; padding: 8px 16px; border-radius: 6px; cursor: pointer; font-size: 13px; }
        .playlist-viewer-actions button:hover { background: #2ea043; }
        .playlist-viewer-actions button.danger { background: #30363d; }
        .playlist-viewer-actions button.danger:hover { background: #da3633; }
        .playlist-viewer-list { flex: 1; overflow-y: auto; padding: 10px 0; }
        .playlist-song { display: flex; align-items: center; gap: 10px; padding: 8px 20px; }
        .playlist-song:hover { background: #1f2428; }
        .playlist-song .song-num { color: #6e7681; font-size: 12px; min-width: 25px; text-align: right; }
        .playlist-song .song-title { flex: 1; color: #c9d1d9; font-size: 13px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
        .playlist-table { width: 100%; border-collapse: collapse; }
        .playlist-table th { padding: 6px 10px; text-align: left; color: #8b949e; font-size: 12px; border-bottom: 1px solid #30363d; }
        .playlist-song td { padding: 6px 10px; border-bottom: 1px solid #21262d; font-size: 13px; }
        .playlist-song:hover td { background: #1f2428; }
        .playlist-song .song-num { color: #8b949e; width: 40px; }
        .playlist-song .song-title { color: #c9d1d9; }
        .playlist-song .song-artist { color: #8b949e; }
        .playlist-song .song-album { color: #8b949e; font-style: italic; }
        .playlist-song .song-controls { display: flex; gap: 4px; }
        .playlist-song .song-controls button { background: none; border: 1px solid #30363d; color: #8b949e; width: 28px; height: 28px; border-radius: 4px; cursor: pointer; font-size: 12px; }
        .playlist-song .song-controls button:hover { background: #238636; color: white; border-color: #238636; }
        .playlist-song .song-controls button:disabled { opacity: 0.3; cursor: not-allowed; }
        .playlist-song .song-controls button:disabled:hover { background: none; color: #8b949e; border-color: #30363d; }
        .playlist-song .song-controls .remove-btn:hover { background: #da3633; border-color: #da3633; }
        .playlist-empty { padding: 40px 20px; text-align: center; color: #6e7681; font-size: 14px; }
        .file-name.playlist-file { color: #a371f7; cursor: pointer; }
        .file-name.playlist-file:hover { text-decoration: underline; }
        /* Metadata Refresh */
        .header-actions { display: flex; gap: 10px; align-items: center; }
        .refresh-btn { background: #238636; border: 1px solid #238636; color: white; padding: 6px 12px; border-radius: 6px; cursor: pointer; font-size: 13px; font-family: inherit; display: flex; align-items: center; gap: 6px; }
        .refresh-btn:hover { background: #2ea043; border-color: #2ea043; }
        .refresh-btn:disabled { opacity: 0.6; cursor: not-allowed; }
        .refresh-btn .spinner { width: 14px; height: 14px; border: 2px solid transparent; border-top-color: white; border-radius: 50%; animation: spin 0.8s linear infinite; }
        .refresh-btn.small { padding: 4px 10px; font-size: 11px; margin-left: auto; }
        .refresh-btn.small .spinner { width: 12px; height: 12px; }
        @keyframes spin { to { transform: rotate(360deg); } }
        .metadata-progress { background: #161b22; border: 1px solid #30363d; border-radius: 6px; padding: 8px 12px; font-size: 12px; color: #8b949e; display: flex; align-items: center; gap: 10px; }
        .metadata-progress .progress-bar { width: 100px; height: 6px; background: #21262d; border-radius: 3px; overflow: hidden; }
        .metadata-progress .progress-bar .progress-fill { height: 100%; background: #238636; transition: width 0.3s; }
        .metadata-progress .progress-text { white-space: nowrap; }
        .metadata-progress .queue-info { color: #f0883e; margin-left: 5px; }
        .metadata-progress.clickable { cursor: pointer; }
        .metadata-progress.clickable:hover { background: #1f2428; border-color: #58a6ff; }

        /* Metadata Queue Panel */
        .metadata-queue-panel { position: fixed; top: 80px; right: 20px; width: 400px; max-height: 500px; background: #161b22; border: 1px solid #30363d; border-radius: 6px; box-shadow: 0 4px 20px rgba(0,0,0,0.4); z-index: 999; overflow: hidden; }
        .metadata-queue-panel.hidden { display: none; }
        .metadata-queue-header { padding: 15px; background: #0d1117; border-bottom: 1px solid #30363d; display: flex; justify-content: space-between; align-items: center; }
        .metadata-queue-header h3 { margin: 0; font-size: 14px; color: #c9d1d9; }
        .metadata-queue-close { background: none; border: none; color: #8b949e; cursor: pointer; font-size: 18px; padding: 0; }
        .metadata-queue-close:hover { color: #c9d1d9; }
        .metadata-queue-content { max-height: 420px; overflow-y: auto; }
        .metadata-queue-current { padding: 15px; background: #1f6feb22; border-bottom: 1px solid #30363d; }
        .metadata-queue-current .current-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 5px; }
        .metadata-queue-current .label { font-size: 11px; color: #58a6ff; text-transform: uppercase; }
        .metadata-queue-current .cancel-btn { background: #da3633; border: none; color: white; padding: 4px 10px; border-radius: 4px; cursor: pointer; font-size: 11px; }
        .metadata-queue-current .cancel-btn:hover { background: #f85149; }
        .metadata-queue-current .path { font-size: 13px; color: #c9d1d9; word-break: break-all; }
        .metadata-queue-current .progress { font-size: 12px; color: #8b949e; margin-top: 5px; }
        .metadata-queue-list { padding: 10px 0; }
        .metadata-queue-item { display: flex; align-items: center; padding: 10px 15px; border-bottom: 1px solid #21262d; gap: 10px; }
        .metadata-queue-item:hover { background: #1f2428; }
        .metadata-queue-item .num { color: #6e7681; font-size: 12px; min-width: 20px; }
        .metadata-queue-item .path { flex: 1; font-size: 13px; color: #c9d1d9; word-break: break-all; }
        .metadata-queue-item .actions { display: flex; gap: 5px; }
        .metadata-queue-item .action-btn { background: none; border: 1px solid #30363d; color: #8b949e; width: 28px; height: 28px; border-radius: 4px; cursor: pointer; font-size: 12px; display: flex; align-items: center; justify-content: center; }
        .metadata-queue-item .action-btn:hover { background: #30363d; color: #c9d1d9; }
        .metadata-queue-item .action-btn.priority:hover { background: #1f6feb; border-color: #1f6feb; color: white; }
        .metadata-queue-item .action-btn.remove:hover { background: #da3633; border-color: #da3633; color: white; }
        .metadata-queue-empty { padding: 30px; text-align: center; color: #6e7681; font-size: 13px; }

        /* Media View */
        .media-view { padding: 20px; }
        .media-section { margin-bottom: 30px; }
        .section-header { display: flex; align-items: center; gap: 10px; margin-bottom: 15px; padding-bottom: 10px; border-bottom: 1px solid #30363d; }
        .section-header h3 { margin: 0; color: #c9d1d9; font-size: 16px; font-weight: 600; }
        .section-header .count { color: #8b949e; font-size: 14px; }
        .thumbnail-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 15px; }
        .thumb-item { display: flex; flex-direction: column; align-items: center; cursor: pointer; padding: 10px; border-radius: 6px; background: #0d1117; border: 1px solid #21262d; transition: all 0.2s; }
        .thumb-item:hover { background: #1f2428; border-color: #58a6ff; }
        .thumb-item img { width: 100%; aspect-ratio: 1; object-fit: cover; border-radius: 4px; background: #21262d; }
        .thumb-item .thumb-name { font-size: 12px; margin-top: 8px; text-align: center; color: #c9d1d9; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; width: 100%; }
        .thumb-item.video .thumb-name { color: #f0883e; }
        .audio-table { width: 100%; border-collapse: collapse; background: #0d1117; border-radius: 6px; overflow: hidden; }
        .audio-table th { background: #161b22; color: #8b949e; font-weight: 600; font-size: 12px; text-transform: uppercase; padding: 10px 15px; text-align: left; }
        .audio-table td { padding: 10px 15px; border-bottom: 1px solid #21262d; color: #c9d1d9; font-size: 13px; }
        .audio-table tr { cursor: pointer; transition: background 0.2s; }
        .audio-table tbody tr:hover { background: #1f2428; }
        .audio-table .title-col { color: #58a6ff; }
        .audio-table .duration-col { color: #8b949e; text-align: right; }
        .other-section table { width: 100%; }
    </style>
</head>
<body>
    <div id="app">
        <!-- Header with toggle -->
        <div class="header-bar">
            <a href="/" style="color:#58a6ff; text-decoration:none; font-size:14px; margin-right:10px;">&larr; Home</a>
            <h1>Q2 File Browser</h1>
            <div class="header-actions">
                <!-- Metadata Progress -->
                <div v-if="metadataStatus.scanning || metadataStatus.queue_length > 0"
                     class="metadata-progress clickable"
                     @click="toggleMetadataQueuePanel"
                     title="Click to manage queue">
                    <div class="progress-bar">
                        <div class="progress-fill" :style="{ width: metadataProgressPercent + '%' }"></div>
                    </div>
                    <span class="progress-text">
                        {{ metadataStatus.files_done }}/{{ metadataStatus.files_total }} files
                        <span v-if="metadataStatus.queue_length > 0" class="queue-info">(+{{ metadataStatus.queue_length }} queued)</span>
                    </span>
                </div>
                <button class="view-toggle" :class="{ active: dualPane }" @click="toggleDualPane">
                    {{ dualPane ? '▢ Single Pane' : '◫ Dual Pane' }}
                </button>
            </div>
        </div>

        <!-- Panes Wrapper -->
        <div class="panes-wrapper" :class="dualPane ? 'dual-pane' : 'single-pane'">
            <!-- Left Pane (Primary) -->
            <div class="pane">
                <!-- Breadcrumb -->
                <div class="breadcrumb">
                    <a @click="loadRoots">Roots</a>
                    <template v-if="currentPath">
                        <template v-for="(part, i) in pathParts" :key="i">
                            <span class="sep">/</span>
                            <a v-if="i < pathParts.length - 1" @click="browseTo(pathParts.slice(0, i + 1))">{{ part }}</a>
                            <strong v-else>{{ part }}</strong>
                        </template>
                    </template>
                </div>

                <!-- Stats Bar -->
                <div class="stats-bar" v-if="currentPath">
                    <span class="stat"><span class="stat-value">{{ folderCount }}</span> folder{{ folderCount !== 1 ? 's' : '' }}</span>
                    <span class="stat"><span class="stat-value">{{ fileCount }}</span> file{{ fileCount !== 1 ? 's' : '' }}</span>
                    <span class="stat"><span class="stat-value">{{ formatSize(totalSize) }}</span> total</span>
                    <div class="pane-actions">
                        <button class="view-toggle small" :class="{ active: viewMode === 'media' }" @click="toggleViewMode">
                            {{ viewMode === 'media' ? '📋 Files' : '🎨 Media' }}
                        </button>
                        <button class="refresh-btn small" @click="refreshMetadata" :disabled="isCurrentPathQueued">
                            <span v-if="isCurrentPathScanning" class="spinner"></span>
                            {{ refreshButtonText }}
                        </button>
                    </div>
                </div>

                <!-- Content -->
                <div class="pane-content">
                    <div v-if="loading" class="loading">Loading...</div>
                    <div v-else-if="error" class="error-message">{{ error }}</div>

                    <!-- Roots List -->
                    <div v-else-if="!currentPath" class="roots-list">
                        <div v-if="roots.length === 0" class="empty-message">
                            No monitored folders. Use "q2 addfolder &lt;path&gt;" to add folders.
                        </div>
                        <div v-for="root in roots" :key="root.path" class="root-item" @click="browse(root.path)">
                            <span class="icon">📁</span>
                            <div>
                                <strong>{{ root.name }}</strong>
                                <div class="path">{{ root.path }}</div>
                            </div>
                        </div>
                    </div>

                    <!-- File Table (File View) -->
                    <table v-else-if="viewMode === 'file'">
                        <thead>
                            <tr>
                                <th @click="changeSort('name')">Name <span class="sort-indicator">{{ sortIndicator('name') }}</span></th>
                                <th @click="changeSort('type')">Type <span class="sort-indicator">{{ sortIndicator('type') }}</span></th>
                                <th @click="changeSort('size')">Size <span class="sort-indicator">{{ sortIndicator('size') }}</span></th>
                                <th @click="changeSort('modified')">Modified <span class="sort-indicator">{{ sortIndicator('modified') }}</span></th>
                            </tr>
                        </thead>
                        <tbody>
                            <tr v-if="sortedEntries.length === 0">
                                <td colspan="4" class="empty-message">This folder is empty</td>
                            </tr>
                            <tr v-for="entry in sortedEntries" :key="entry.name">
                                <td class="name-cell">
                                    <span class="icon">{{ entry.type === 'dir' ? '📁' : (isAudio(entry.name) ? '🎵' : (isImage(entry.name) ? '🖼️' : (isVideo(entry.name) ? '🎬' : (isPlaylist(entry.name) ? '📋' : '📄')))) }}</span>
                                    <a v-if="entry.type === 'dir'" class="folder-link" @click="browse(fullPath(entry.name))">{{ entry.name }}</a>
                                    <span v-else-if="isPlaylist(entry.name)" class="file-name playlist-file" @click="openPlaylist(entry)">{{ entry.name }}</span>
                                    <span v-else-if="isImage(entry.name)" class="file-name image-file" @click="openImage(entry)">{{ entry.name }}</span>
                                    <span v-else-if="isVideo(entry.name)" class="file-name video-file" @click="openVideo(entry)">{{ entry.name }}</span>
                                    <span v-else class="file-name">{{ entry.name }}</span>
                                    <div v-if="isImage(entry.name)" class="audio-controls">
                                        <button class="audio-btn" @click.stop="openAlbumMenu($event, entry)" title="Add to album">📁</button>
                                    </div>
                                    <div v-if="isAudio(entry.name)" class="audio-controls">
                                        <button class="audio-btn play" @click.stop="playNow(entry)" title="Play now">▶</button>
                                        <button class="audio-btn" @click.stop="addToQueueTop(entry)" title="Add to top of queue">⬆Q</button>
                                        <button class="audio-btn" @click.stop="addToQueueBottom(entry)" title="Add to bottom of queue">Q⬇</button>
                                        <button class="audio-btn" @click.stop="openPlaylistMenu($event, entry)" title="Add to playlist">...</button>
                                    </div>
                                </td>
                                <td class="type-cell">{{ entry.type === 'dir' ? 'Folder' : getExtension(entry.name) || 'File' }}</td>
                                <td class="size-cell">{{ entry.type === 'dir' ? '-' : formatSize(entry.size) }}</td>
                                <td class="modified-cell">{{ formatDate(entry.modified) }}</td>
                            </tr>
                        </tbody>
                    </table>

                    <!-- Media View -->
                    <div v-else-if="viewMode === 'media'" class="media-view">
                        <!-- Images Section -->
                        <div v-if="imageEntries.length" class="media-section">
                            <h3 class="section-header">Images ({{ imageEntries.length }})</h3>
                            <div class="thumbnail-grid">
                                <div v-for="img in imageEntries" :key="img.name" class="thumb-item" @click="openImage(img)" @contextmenu.prevent="openAlbumMenu($event, img)">
                                    <img :src="img.thumbnailSmall || '/api/thumbnail?path=' + encodeURIComponent(fullPath(img.name)) + '&size=small'" :alt="img.name" loading="lazy" @error="handleThumbError">
                                    <span class="thumb-name">{{ img.name }}</span>
                                </div>
                            </div>
                        </div>

                        <!-- Audio Section -->
                        <div v-if="audioEntries.length" class="media-section">
                            <h3 class="section-header">Audio ({{ audioEntries.length }})</h3>
                            <table class="audio-table">
                                <thead>
                                    <tr>
                                        <th></th>
                                        <th>Title</th>
                                        <th>Artist</th>
                                        <th>Album</th>
                                        <th>Duration</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    <tr v-for="audio in audioEntries" :key="audio.name" class="audio-row" @dblclick="playNow(audio)">
                                        <td class="audio-actions">
                                            <button class="audio-btn play" @click.stop="playNow(audio)" title="Play">▶</button>
                                        </td>
                                        <td class="audio-title">{{ audio.title || audio.name }}</td>
                                        <td class="audio-artist">{{ audio.artist || '-' }}</td>
                                        <td class="audio-album">{{ audio.album || '-' }}</td>
                                        <td class="audio-duration">{{ audio.duration ? formatDuration(audio.duration) : '-' }}</td>
                                    </tr>
                                </tbody>
                            </table>
                        </div>

                        <!-- Videos Section -->
                        <div v-if="videoEntries.length" class="media-section">
                            <h3 class="section-header">Videos ({{ videoEntries.length }})</h3>
                            <div class="thumbnail-grid">
                                <div v-for="vid in videoEntries" :key="vid.name" class="thumb-item" @click="openVideo(vid)">
                                    <img :src="vid.thumbnailSmall || '/api/thumbnail?path=' + encodeURIComponent(fullPath(vid.name)) + '&size=small'" :alt="vid.name" loading="lazy" @error="handleThumbError">
                                    <span class="thumb-name">{{ vid.name }}</span>
                                </div>
                            </div>
                        </div>

                        <!-- Other Section (folders + misc files) -->
                        <div v-if="otherEntries.length" class="media-section">
                            <h3 class="section-header">Other ({{ otherEntries.length }})</h3>
                            <table>
                                <thead>
                                    <tr>
                                        <th>Name</th>
                                        <th>Type</th>
                                        <th>Size</th>
                                        <th>Modified</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    <tr v-for="entry in otherEntries" :key="entry.name">
                                        <td class="name-cell">
                                            <span class="icon">{{ entry.type === 'dir' ? '📁' : '📄' }}</span>
                                            <a v-if="entry.type === 'dir'" class="folder-link" @click="browse(fullPath(entry.name))">{{ entry.name }}</a>
                                            <span v-else class="file-name">{{ entry.name }}</span>
                                        </td>
                                        <td class="type-cell">{{ entry.type === 'dir' ? 'Folder' : getExtension(entry.name) || 'File' }}</td>
                                        <td class="size-cell">{{ entry.type === 'dir' ? '-' : formatSize(entry.size) }}</td>
                                        <td class="modified-cell">{{ formatDate(entry.modified) }}</td>
                                    </tr>
                                </tbody>
                            </table>
                        </div>

                        <div v-if="!imageEntries.length && !audioEntries.length && !videoEntries.length && !otherEntries.length" class="empty-message">
                            This folder is empty
                        </div>
                    </div>
                </div>
            </div>

            <!-- Right Pane (Secondary) - Only shown in dual pane mode -->
            <div class="pane" v-if="dualPane">
                <!-- Breadcrumb -->
                <div class="breadcrumb">
                    <a @click="loadRoots2">Roots</a>
                    <template v-if="pane2Path">
                        <template v-for="(part, i) in pane2PathParts" :key="i">
                            <span class="sep">/</span>
                            <a v-if="i < pane2PathParts.length - 1" @click="browseTo2(pane2PathParts.slice(0, i + 1))">{{ part }}</a>
                            <strong v-else>{{ part }}</strong>
                        </template>
                    </template>
                </div>

                <!-- Stats Bar -->
                <div class="stats-bar" v-if="pane2Path">
                    <span class="stat"><span class="stat-value">{{ pane2FolderCount }}</span> folder{{ pane2FolderCount !== 1 ? 's' : '' }}</span>
                    <span class="stat"><span class="stat-value">{{ pane2FileCount }}</span> file{{ pane2FileCount !== 1 ? 's' : '' }}</span>
                    <span class="stat"><span class="stat-value">{{ formatSize(pane2TotalSize) }}</span> total</span>
                    <div class="pane-actions">
                        <button class="view-toggle small" :class="{ active: viewMode2 === 'media' }" @click="toggleViewMode2">
                            {{ viewMode2 === 'media' ? '📋 Files' : '🎨 Media' }}
                        </button>
                        <button class="refresh-btn small" @click="refreshMetadata2" :disabled="isPane2PathQueued">
                            <span v-if="isPane2PathScanning" class="spinner"></span>
                            {{ refreshButtonText2 }}
                        </button>
                    </div>
                </div>

                <!-- Content -->
                <div class="pane-content">
                    <div v-if="pane2Loading" class="loading">Loading...</div>
                    <div v-else-if="pane2Error" class="error-message">{{ pane2Error }}</div>

                    <!-- Roots List -->
                    <div v-else-if="!pane2Path" class="roots-list">
                        <div v-if="roots.length === 0" class="empty-message">
                            No monitored folders. Use "q2 addfolder &lt;path&gt;" to add folders.
                        </div>
                        <div v-for="root in roots" :key="root.path" class="root-item" @click="browse2(root.path)">
                            <span class="icon">📁</span>
                            <div>
                                <strong>{{ root.name }}</strong>
                                <div class="path">{{ root.path }}</div>
                            </div>
                        </div>
                    </div>

                    <!-- File Table -->
                    <table v-else-if="viewMode2 === 'file'">
                        <thead>
                            <tr>
                                <th @click="changeSort2('name')">Name <span class="sort-indicator">{{ sortIndicator2('name') }}</span></th>
                                <th @click="changeSort2('type')">Type <span class="sort-indicator">{{ sortIndicator2('type') }}</span></th>
                                <th @click="changeSort2('size')">Size <span class="sort-indicator">{{ sortIndicator2('size') }}</span></th>
                                <th @click="changeSort2('modified')">Modified <span class="sort-indicator">{{ sortIndicator2('modified') }}</span></th>
                            </tr>
                        </thead>
                        <tbody>
                            <tr v-if="pane2SortedEntries.length === 0">
                                <td colspan="4" class="empty-message">This folder is empty</td>
                            </tr>
                            <tr v-for="entry in pane2SortedEntries" :key="entry.name">
                                <td class="name-cell">
                                    <span class="icon">{{ entry.type === 'dir' ? '📁' : (isAudio(entry.name) ? '🎵' : (isImage(entry.name) ? '🖼️' : (isVideo(entry.name) ? '🎬' : (isPlaylist(entry.name) ? '📋' : '📄')))) }}</span>
                                    <a v-if="entry.type === 'dir'" class="folder-link" @click="browse2(pane2FullPath(entry.name))">{{ entry.name }}</a>
                                    <span v-else-if="isPlaylist(entry.name)" class="file-name playlist-file" @click="openPlaylist(entry)">{{ entry.name }}</span>
                                    <span v-else-if="isImage(entry.name)" class="file-name image-file" @click="openImage2(entry)">{{ entry.name }}</span>
                                    <span v-else-if="isVideo(entry.name)" class="file-name video-file" @click="openVideo2(entry)">{{ entry.name }}</span>
                                    <span v-else class="file-name">{{ entry.name }}</span>
                                    <div v-if="isImage(entry.name)" class="audio-controls">
                                        <button class="audio-btn" @click.stop="openAlbumMenu($event, entry, true)" title="Add to album">📁</button>
                                    </div>
                                    <div v-if="isAudio(entry.name)" class="audio-controls">
                                        <button class="audio-btn play" @click.stop="playNow2(entry)" title="Play now">▶</button>
                                        <button class="audio-btn" @click.stop="addToQueueTop2(entry)" title="Add to top of queue">⬆Q</button>
                                        <button class="audio-btn" @click.stop="addToQueueBottom2(entry)" title="Add to bottom of queue">Q⬇</button>
                                        <button class="audio-btn" @click.stop="openPlaylistMenu($event, entry, true)" title="Add to playlist">...</button>
                                    </div>
                                </td>
                                <td class="type-cell">{{ entry.type === 'dir' ? 'Folder' : getExtension(entry.name) || 'File' }}</td>
                                <td class="size-cell">{{ entry.type === 'dir' ? '-' : formatSize(entry.size) }}</td>
                                <td class="modified-cell">{{ formatDate(entry.modified) }}</td>
                            </tr>
                        </tbody>
                    </table>

                    <!-- Media View for Pane 2 -->
                    <div v-else-if="viewMode2 === 'media'" class="media-view">
                        <!-- Images Section -->
                        <div v-if="imageEntries2.length" class="media-section">
                            <h3 class="section-header">Images ({{ imageEntries2.length }})</h3>
                            <div class="thumbnail-grid">
                                <div v-for="img in imageEntries2" :key="img.name" class="thumb-item" @click="openImage2(img)" @contextmenu.prevent="openAlbumMenu($event, img, true)">
                                    <img :src="img.thumbnailSmall || '/api/thumbnail?path=' + encodeURIComponent(pane2FullPath(img.name)) + '&size=small'" :alt="img.name" loading="lazy" @error="handleThumbError">
                                    <span class="thumb-name">{{ img.name }}</span>
                                </div>
                            </div>
                        </div>

                        <!-- Audio Section -->
                        <div v-if="audioEntries2.length" class="media-section">
                            <h3 class="section-header">Audio ({{ audioEntries2.length }})</h3>
                            <table class="audio-table">
                                <thead>
                                    <tr>
                                        <th></th>
                                        <th>Title</th>
                                        <th>Artist</th>
                                        <th>Album</th>
                                        <th>Duration</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    <tr v-for="audio in audioEntries2" :key="audio.name" class="audio-row" @dblclick="playNow2(audio)">
                                        <td class="audio-actions">
                                            <button class="audio-btn play" @click.stop="playNow2(audio)" title="Play">▶</button>
                                        </td>
                                        <td class="audio-title">{{ audio.title || audio.name }}</td>
                                        <td class="audio-artist">{{ audio.artist || '-' }}</td>
                                        <td class="audio-album">{{ audio.album || '-' }}</td>
                                        <td class="audio-duration">{{ audio.duration ? formatDuration(audio.duration) : '-' }}</td>
                                    </tr>
                                </tbody>
                            </table>
                        </div>

                        <!-- Videos Section -->
                        <div v-if="videoEntries2.length" class="media-section">
                            <h3 class="section-header">Videos ({{ videoEntries2.length }})</h3>
                            <div class="thumbnail-grid">
                                <div v-for="vid in videoEntries2" :key="vid.name" class="thumb-item" @click="openVideo2(vid)">
                                    <img :src="vid.thumbnailSmall || '/api/thumbnail?path=' + encodeURIComponent(pane2FullPath(vid.name)) + '&size=small'" :alt="vid.name" loading="lazy" @error="handleThumbError">
                                    <span class="thumb-name">{{ vid.name }}</span>
                                </div>
                            </div>
                        </div>

                        <!-- Other Section -->
                        <div v-if="otherEntries2.length" class="media-section">
                            <h3 class="section-header">Other ({{ otherEntries2.length }})</h3>
                            <table>
                                <thead>
                                    <tr>
                                        <th>Name</th>
                                        <th>Type</th>
                                        <th>Size</th>
                                        <th>Modified</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    <tr v-for="entry in otherEntries2" :key="entry.name">
                                        <td class="name-cell">
                                            <span class="icon">{{ entry.type === 'dir' ? '📁' : '📄' }}</span>
                                            <a v-if="entry.type === 'dir'" class="folder-link" @click="browse2(pane2FullPath(entry.name))">{{ entry.name }}</a>
                                            <span v-else class="file-name">{{ entry.name }}</span>
                                        </td>
                                        <td class="type-cell">{{ entry.type === 'dir' ? 'Folder' : getExtension(entry.name) || 'File' }}</td>
                                        <td class="size-cell">{{ entry.type === 'dir' ? '-' : formatSize(entry.size) }}</td>
                                        <td class="modified-cell">{{ formatDate(entry.modified) }}</td>
                                    </tr>
                                </tbody>
                            </table>
                        </div>

                        <div v-if="!imageEntries2.length && !audioEntries2.length && !videoEntries2.length && !otherEntries2.length" class="empty-message">
                            This folder is empty
                        </div>
                    </div>
                </div>
            </div>
        </div>

        <!-- Playlist Menu Popup -->
        <div class="playlist-popup" :class="{ hidden: !showPlaylistMenu }" :style="playlistMenuStyle">
            <div class="playlist-popup-header">
                <span>Add to Playlist</span>
                <button @click="closePlaylistMenu">×</button>
            </div>
            <div class="playlist-popup-list">
                <div v-for="pl in availablePlaylists" :key="pl.path"
                     class="playlist-popup-item"
                     @click="addToPlaylist(pl.path)">
                    {{ pl.name }}
                    <span v-if="pl.contains" class="already-here">(already here)</span>
                </div>
                <div v-if="availablePlaylists.length === 0" class="playlist-popup-empty">
                    No playlists yet
                </div>
                <div class="playlist-popup-new" @click="createNewPlaylist">
                    + Create new playlist...
                </div>
            </div>
        </div>

        <!-- Album Popup Menu -->
        <div class="playlist-popup" :class="{ hidden: !showAlbumMenu }" :style="albumMenuStyle">
            <div class="playlist-popup-header">
                <span>Add to Album</span>
                <button @click="closeAlbumMenu">×</button>
            </div>
            <div class="playlist-popup-list">
                <div v-for="album in availableAlbums" :key="album.id"
                     class="playlist-popup-item"
                     @click="addToAlbum(album.id)">
                    {{ album.name }} ({{ album.item_count }})
                    <span v-if="album.contains" class="already-here">(already here)</span>
                </div>
                <div v-if="availableAlbums.length === 0" class="playlist-popup-empty">
                    No albums yet
                </div>
                <div class="playlist-popup-new" @click="createNewAlbum">
                    + Create new album...
                </div>
            </div>
        </div>

        <!-- Playlist Viewer Modal -->
        <div class="playlist-viewer" :class="{ hidden: !viewingPlaylist }">
            <div class="playlist-viewer-content">
                <div class="playlist-viewer-header">
                    <h2>{{ viewingPlaylist?.name }}</h2>
                    <button class="close-btn" @click="closePlaylistViewer">×</button>
                </div>
                <div class="playlist-viewer-actions">
                    <button @click="playAllFromPlaylist">▶ Play All</button>
                    <button @click="shuffleAndPlayPlaylist">🔀 Shuffle</button>
                    <button @click="deletePlaylist" class="danger">🗑 Delete Playlist</button>
                </div>
                <div class="playlist-viewer-list">
                    <table class="playlist-table">
                        <thead><tr>
                            <th style="width:40px">#</th>
                            <th>Title</th>
                            <th>Artist</th>
                            <th>Album</th>
                            <th style="width:80px"></th>
                        </tr></thead>
                        <tbody>
                        <tr v-for="(song, i) in playlistSongs" :key="i" class="playlist-song">
                            <td class="song-num">{{ i + 1 }}</td>
                            <td class="song-title" :title="song.path">{{ song.title }}</td>
                            <td class="song-artist">{{ song.artist }}</td>
                            <td class="song-album">{{ song.album }}</td>
                            <td>
                                <div class="song-controls">
                                    <button @click="playSongFromPlaylist(i)" title="Play">▶</button>
                                    <button @click="movePlaylistSongUp(i)" :disabled="i === 0" title="Move up">▲</button>
                                    <button @click="movePlaylistSongDown(i)" :disabled="i === playlistSongs.length - 1" title="Move down">▼</button>
                                    <button @click="removeFromPlaylist(i)" title="Remove" class="remove-btn">×</button>
                                </div>
                            </td>
                        </tr>
                        </tbody>
                    </table>
                    <div v-if="playlistSongs.length === 0" class="playlist-empty">
                        This playlist is empty. Add songs using the "..." button next to audio files.
                    </div>
                </div>
            </div>
        </div>

        <!-- Audio Player -->
        <div class="audio-player" :class="{ hidden: !currentTrack || videoFile }">
            <div class="player-controls">
                <button class="player-btn" @click="playPrevious" title="Previous">⏮</button>
                <button class="player-btn play-pause" @click="togglePlay" :title="isPlaying ? 'Pause' : 'Play'">
                    {{ isPlaying ? '⏸' : '▶' }}
                </button>
                <button class="player-btn" @click="playNext" title="Next">⏭</button>
            </div>
            <div class="track-info">
                <div class="track-name">{{ currentTrack?.name || 'No track' }}</div>
            </div>
            <div class="progress-container">
                <span class="time-display">{{ formatTime(currentTime) }} / {{ formatTime(duration) }}</span>
                <div class="progress-bar" @click="seek($event)">
                    <div class="progress-fill" :style="{ width: progressPercent + '%' }"></div>
                </div>
            </div>
            <div class="player-right">
                <button class="player-btn" @click="toggleMute" :title="isMuted ? 'Unmute' : 'Mute'">
                    {{ isMuted ? '🔇' : '🔊' }}
                </button>
                <label class="crossfade-toggle" :class="{ active: crossfadeEnabled }">
                    <input type="checkbox" v-model="crossfadeEnabled"> Crossfade
                </label>
                <button class="player-btn queue-btn" @click="toggleQueue" title="Queue">
                    🎵
                    <span v-if="queue.length > 0" class="queue-count">{{ queue.length }}</span>
                </button>
                <button class="player-btn cast-btn" :class="{ casting: isCasting }" @click="toggleCastPanel" title="Cast">
                    📺
                </button>
            </div>
        </div>

        <!-- Metadata Queue Panel -->
        <div class="metadata-queue-panel" :class="{ hidden: !showMetadataQueuePanel }">
            <div class="metadata-queue-header">
                <h3>Metadata Refresh Queue</h3>
                <button class="metadata-queue-close" @click="showMetadataQueuePanel = false">×</button>
            </div>
            <div class="metadata-queue-content">
                <!-- Currently scanning -->
                <div v-if="metadataStatus.scanning" class="metadata-queue-current">
                    <div class="current-header">
                        <div class="label">Currently Scanning</div>
                        <button class="cancel-btn" @click="cancelMetadataScan" title="Cancel scan">Cancel</button>
                    </div>
                    <div class="path">{{ metadataStatus.path }}</div>
                    <div class="progress">{{ metadataStatus.files_done }}/{{ metadataStatus.files_total }} files ({{ metadataProgressPercent }}%)</div>
                </div>
                <!-- Queue list -->
                <div class="metadata-queue-list" v-if="metadataStatus.queue && metadataStatus.queue.length > 0">
                    <div v-for="(path, i) in metadataStatus.queue" :key="path" class="metadata-queue-item">
                        <span class="num">#{{ i + 1 }}</span>
                        <span class="path">{{ path }}</span>
                        <div class="actions">
                            <button v-if="i > 0" class="action-btn priority" @click="prioritizeInQueue(path)" title="Move to top">⬆</button>
                            <button class="action-btn remove" @click="removeFromMetadataQueue(path)" title="Remove">×</button>
                        </div>
                    </div>
                </div>
                <!-- Empty state -->
                <div v-if="!metadataStatus.scanning && (!metadataStatus.queue || metadataStatus.queue.length === 0)" class="metadata-queue-empty">
                    No folders in queue
                </div>
            </div>
        </div>

        <!-- Cast Panel -->
        <div class="cast-panel" :class="{ hidden: !showCastPanel }">
            <div class="cast-header">
                <h3>Cast to device</h3>
                <button v-if="!castScanning" class="cast-refresh" @click="scanCastDevices">Refresh</button>
                <span v-else class="cast-scanning">Scanning...</span>
            </div>
            <div class="cast-list">
                <div class="cast-device" :class="{ active: !isCasting }" @click="stopCasting">
                    <span class="icon">💻</span>
                    <span class="name">This device</span>
                    <span v-if="!isCasting" class="status">Playing</span>
                </div>
                <div v-if="isCasting" class="cast-device active">
                    <span class="icon">📺</span>
                    <span class="name">{{ castingTo }}</span>
                    <span class="status">Casting</span>
                </div>
                <div v-for="device in castDevices" :key="device.uuid"
                     class="cast-device"
                     :class="{ active: isCasting && castingTo === device.name }"
                     @click="connectCastDevice(device)">
                    <span class="icon">📺</span>
                    <span class="name">{{ device.name }}</span>
                    <span class="status">{{ device.device_type }}</span>
                </div>
                <div v-if="castScanError && !castScanning" class="cast-unavailable">
                    {{ castScanError }}<br>
                    <small>Click Refresh to retry</small>
                </div>
                <div v-else-if="castDevices.length === 0 && !castScanning" class="cast-unavailable">
                    No devices found.<br>
                    <small>Click Refresh to scan</small>
                </div>
            </div>
        </div>

        <!-- Queue Panel -->
        <div class="queue-panel" :class="{ hidden: !showQueue }">
            <div class="queue-header">
                <h3>Queue ({{ queue.length }})</h3>
                <button class="queue-clear" @click="clearQueue" v-if="queue.length > 0">Clear all</button>
            </div>
            <div class="queue-list">
                <div v-if="queue.length === 0" class="queue-empty">Queue is empty</div>
                <div v-for="(track, i) in queue" :key="track.path + i"
                     class="queue-item" :class="{ playing: i === currentIndex }">
                    <span class="num">{{ i + 1 }}</span>
                    <span class="name" :title="track.name">{{ track.name }}</span>
                    <div class="move-btns">
                        <button class="move-btn" @click="moveUp(i)" v-if="i > 0" title="Move up">▲</button>
                        <button class="move-btn" @click="moveDown(i)" v-if="i < queue.length - 1" title="Move down">▼</button>
                    </div>
                    <button class="remove" @click="removeFromQueue(i)" title="Remove">×</button>
                </div>
            </div>
        </div>

        <!-- Image Viewer -->
        <div class="image-viewer" :class="{ hidden: !viewingImage }">
            <div class="image-viewer-header">
                <span class="image-viewer-title">{{ viewingImage?.name }}</span>
                <button class="image-viewer-close" @click="closeImage">Close (Esc)</button>
            </div>
            <div class="image-viewer-content">
                <img v-if="viewingImage" :src="'/api/image?path=' + encodeURIComponent(viewingImage._pane2Path ? (viewingImage._pane2Path + '\\\\' + viewingImage.name) : fullPath(viewingImage.name))" :alt="viewingImage.name">
            </div>
            <button class="image-viewer-nav prev" @click="prevImage" :disabled="!canPrevImage">❮</button>
            <button class="image-viewer-nav next" @click="nextImage" :disabled="!canNextImage">❯</button>
        </div>

        <!-- Video Player -->
        <div class="video-player" :class="{ hidden: !videoFile }">
            <div class="video-player-header">
                <span class="video-player-title">{{ videoFile?.name }}</span>
                <button class="video-player-close" @click="closeVideo">Close (Esc)</button>
            </div>
            <div class="video-player-content">
                <video v-if="videoFile"
                       ref="videoRef"
                       :src="'/api/video?path=' + encodeURIComponent(videoFile._pane2Path ? (videoFile._pane2Path + '\\\\' + videoFile.name) : fullPath(videoFile.name))"
                       @timeupdate="onVideoTimeUpdate"
                       @loadedmetadata="onVideoMetadata"
                       @play="onVideoPlay"
                       @pause="onVideoPause"
                       @ended="onVideoEnded">
                </video>
            </div>
            <div class="video-controls">
                <button class="video-btn play-pause" @click="toggleVideoPlay" :title="videoPlaying ? 'Pause' : 'Play'">
                    {{ videoPlaying ? '⏸' : '▶' }}
                </button>
                <div class="video-progress-container">
                    <span class="video-time">{{ formatTime(videoCurrentTime) }}</span>
                    <div class="video-progress-bar" @click="seekVideo($event)">
                        <div class="video-progress-fill" :style="{ width: videoProgressPercent + '%' }"></div>
                    </div>
                    <span class="video-time">{{ formatTime(videoDuration) }}</span>
                </div>
                <button class="video-btn" @click="toggleVideoMute" :title="videoMuted ? 'Unmute' : 'Mute'">
                    {{ videoMuted ? '🔇' : '🔊' }}
                </button>
                <button class="video-btn" :class="{ casting: isVideoCasting }" @click="openVideoCastPicker" :disabled="!castAvailable" title="Cast">
                    📺
                </button>
            </div>
            <div v-if="isVideoCasting" class="video-casting-indicator">
                Casting to {{ videoCastingTo }}
                <button class="stop-casting-btn" @click="stopVideoCasting">Stop</button>
            </div>
        </div>

        <!-- Hidden audio elements for crossfade -->
        <audio ref="audioA" @timeupdate="onTimeUpdate" @ended="onEnded" @loadedmetadata="onMetadata"></audio>
        <audio ref="audioB" @timeupdate="onTimeUpdate" @ended="onEnded" @loadedmetadata="onMetadata"></audio>
    </div>

    <script>
        const AUDIO_EXTENSIONS = ['mp3', 'wav', 'flac', 'aac', 'ogg', 'wma', 'm4a'];
        const IMAGE_EXTENSIONS = ['jpg', 'jpeg', 'png', 'gif', 'webp', 'bmp', 'svg', 'ico'];
        const VIDEO_EXTENSIONS = ['mp4', 'webm', 'ogv', 'mov', 'avi', 'mkv', 'm4v'];
        const CROSSFADE_DURATION = 3; // seconds

        const { createApp, ref, computed, watch, onMounted, onUnmounted, nextTick } = Vue;

        createApp({
            setup() {
                // File browser state
                const roots = ref([]);
                const currentPath = ref(null);
                const entries = ref([]);
                const loading = ref(true);
                const error = ref(null);
                const sortColumn = ref('name');
                const sortAsc = ref(true);

                // Dual pane state
                const dualPane = ref(false);
                const pane2Path = ref(null);
                const pane2Entries = ref([]);
                const pane2Loading = ref(false);
                const pane2Error = ref(null);
                const pane2SortColumn = ref('name');
                const pane2SortAsc = ref(true);

                // View mode state
                const viewMode = ref('file'); // 'file' or 'media'
                const viewMode2 = ref('file'); // 'file' or 'media' for pane 2

                // Audio player state
                const queue = ref([]);
                const currentIndex = ref(-1);
                const isPlaying = ref(false);
                const currentTime = ref(0);
                const duration = ref(0);
                const crossfadeEnabled = ref(true);
                const showQueue = ref(false);
                const isMuted = ref(false);

                // Cast state (backend-based)
                const showCastPanel = ref(false);
                const castDevices = ref([]);
                const castScanning = ref(false);
                const castScanError = ref(null);
                const isCasting = ref(false);
                const castingTo = ref(null);
                let castStatusInterval = null;

                // Metadata refresh state
                const metadataStatus = ref({
                    scanning: false,
                    path: '',
                    current_file: '',
                    files_total: 0,
                    files_done: 0,
                    queue: [],
                    queue_length: 0
                });
                let metadataStatusInterval = null;
                const showMetadataQueuePanel = ref(false);

                // Computed property for metadata progress percent
                const metadataProgressPercent = computed(() => {
                    if (metadataStatus.value.files_total === 0) return 0;
                    return Math.round((metadataStatus.value.files_done / metadataStatus.value.files_total) * 100);
                });

                // Check if current path is being scanned
                const isCurrentPathScanning = computed(() => {
                    return metadataStatus.value.scanning && metadataStatus.value.path === currentPath.value;
                });

                // Check if current path is in the queue (returns position or 0)
                const currentPathQueuePosition = computed(() => {
                    if (!currentPath.value || !metadataStatus.value.queue) return 0;
                    const idx = metadataStatus.value.queue.indexOf(currentPath.value);
                    return idx >= 0 ? idx + 1 : 0;
                });

                // Check if current path is already queued or scanning
                const isCurrentPathQueued = computed(() => {
                    return isCurrentPathScanning.value || currentPathQueuePosition.value > 0;
                });

                // Button text for refresh button
                const refreshButtonText = computed(() => {
                    if (isCurrentPathScanning.value) {
                        return 'Scanning...';
                    }
                    if (currentPathQueuePosition.value > 0) {
                        return '#' + currentPathQueuePosition.value + ' in queue';
                    }
                    return 'Refresh Metadata';
                });

                // Pane 2 metadata scanning state
                const isPane2PathScanning = computed(() => {
                    return metadataStatus.value.scanning && metadataStatus.value.path === pane2Path.value;
                });

                const pane2PathQueuePosition = computed(() => {
                    if (!pane2Path.value || !metadataStatus.value.queue) return 0;
                    const idx = metadataStatus.value.queue.indexOf(pane2Path.value);
                    return idx >= 0 ? idx + 1 : 0;
                });

                const isPane2PathQueued = computed(() => {
                    return isPane2PathScanning.value || pane2PathQueuePosition.value > 0;
                });

                const refreshButtonText2 = computed(() => {
                    if (isPane2PathScanning.value) {
                        return 'Scanning...';
                    }
                    if (pane2PathQueuePosition.value > 0) {
                        return '#' + pane2PathQueuePosition.value + ' in queue';
                    }
                    return 'Refresh Metadata';
                });

                // Check metadata status
                const checkMetadataStatus = async () => {
                    try {
                        const resp = await fetch('/api/metadata/status');
                        const data = await resp.json();
                        metadataStatus.value = data;
                        // Stop polling only if not scanning AND queue is empty
                        if (!data.scanning && data.queue_length === 0 && metadataStatusInterval) {
                            clearInterval(metadataStatusInterval);
                            metadataStatusInterval = null;
                        }
                    } catch (e) {
                        console.error('Failed to check metadata status:', e);
                    }
                };

                // Refresh metadata for current folder
                const refreshMetadata = async () => {
                    if (!currentPath.value) return;
                    try {
                        const resp = await fetch('/api/metadata/refresh', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ path: currentPath.value })
                        });
                        const data = await resp.json();
                        if (data.success) {
                            // Start polling for status
                            metadataStatus.value.scanning = true;
                            if (!metadataStatusInterval) {
                                metadataStatusInterval = setInterval(checkMetadataStatus, 500);
                            }
                        } else if (data.error) {
                            console.error('Metadata refresh error:', data.error);
                        }
                    } catch (e) {
                        console.error('Failed to refresh metadata:', e);
                    }
                };

                const refreshMetadata2 = async () => {
                    if (!pane2Path.value) return;
                    try {
                        const resp = await fetch('/api/metadata/refresh', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ path: pane2Path.value })
                        });
                        const data = await resp.json();
                        if (data.success) {
                            // Start polling for status
                            metadataStatus.value.scanning = true;
                            if (!metadataStatusInterval) {
                                metadataStatusInterval = setInterval(checkMetadataStatus, 500);
                            }
                        } else if (data.error) {
                            console.error('Metadata refresh error:', data.error);
                        }
                    } catch (e) {
                        console.error('Failed to refresh metadata:', e);
                    }
                };

                // Toggle metadata queue panel
                const toggleMetadataQueuePanel = () => {
                    showMetadataQueuePanel.value = !showMetadataQueuePanel.value;
                };

                // Remove a folder from the metadata queue
                const removeFromMetadataQueue = async (path) => {
                    try {
                        const resp = await fetch('/api/metadata/queue?path=' + encodeURIComponent(path), {
                            method: 'DELETE'
                        });
                        if (resp.ok) {
                            // Refresh status to update queue display
                            await checkMetadataStatus();
                        }
                    } catch (e) {
                        console.error('Failed to remove from queue:', e);
                    }
                };

                // Move a folder to the top of the queue
                const prioritizeInQueue = async (path) => {
                    try {
                        const resp = await fetch('/api/metadata/queue/prioritize', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ path: path })
                        });
                        if (resp.ok) {
                            // Refresh status to update queue display
                            await checkMetadataStatus();
                        }
                    } catch (e) {
                        console.error('Failed to prioritize in queue:', e);
                    }
                };

                // Cancel current metadata scan
                const cancelMetadataScan = async () => {
                    try {
                        const resp = await fetch('/api/metadata/cancel', {
                            method: 'POST'
                        });
                        if (resp.ok) {
                            // Refresh status to update display
                            await checkMetadataStatus();
                        }
                    } catch (e) {
                        console.error('Failed to cancel scan:', e);
                    }
                };

                // Check metadata status on mount (in case refresh is already running)
                checkMetadataStatus();

                // Scan for cast devices via backend (all devices - any Chromecast can play audio)
                const scanCastDevices = async () => {
                    castScanning.value = true;
                    castScanError.value = null;
                    try {
                        const resp = await fetch('/api/cast/devices');
                        const data = await resp.json();
                        if (!resp.ok) {
                            castScanError.value = data.error || 'Discovery failed';
                        } else if (data.devices) {
                            castDevices.value = data.devices;
                        }
                    } catch (e) {
                        castScanError.value = 'Network error scanning for devices';
                        console.error('Failed to scan cast devices:', e);
                    }
                    castScanning.value = false;
                };

                // Connect to a cast device
                const connectCastDevice = async (device) => {
                    try {
                        const resp = await fetch('/api/cast/connect', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ uuid: device.uuid })
                        });
                        const data = await resp.json();
                        if (data.success) {
                            isCasting.value = true;
                            castingTo.value = device.name;
                            showCastPanel.value = false;
                            // Pause local audio
                            const localAudio = getActiveAudioElement();
                            if (localAudio) {
                                localAudio.pause();
                            }
                            // Start status polling
                            startCastStatusPolling();
                            // Cast current track if one is loaded
                            if (currentTrack.value) {
                                castCurrentTrack();
                            }
                        } else {
                            console.error('Failed to connect:', data.error);
                        }
                    } catch (e) {
                        console.error('Failed to connect to cast device:', e);
                    }
                };

                // Start polling cast status
                const startCastStatusPolling = () => {
                    if (castStatusInterval) return;
                    castStatusInterval = setInterval(async () => {
                        if (!isCasting.value) {
                            clearInterval(castStatusInterval);
                            castStatusInterval = null;
                            return;
                        }
                        try {
                            const resp = await fetch('/api/cast/status');
                            const status = await resp.json();
                            if (status.connected) {
                                if (status.player_state === 'PLAYING') {
                                    isPlaying.value = true;
                                } else if (status.player_state === 'PAUSED') {
                                    isPlaying.value = false;
                                }
                                currentTime.value = status.current_time;
                                duration.value = status.duration;
                            } else {
                                // Connection lost
                                isCasting.value = false;
                                castingTo.value = null;
                            }
                        } catch (e) {
                            console.error('Failed to get cast status:', e);
                        }
                    }, 1000);
                };

                // Image viewer state
                const viewingImage = ref(null);

                // Video player state
                const videoFile = ref(null);
                const videoRef = ref(null);
                const videoPlaying = ref(false);
                const videoCurrentTime = ref(0);
                const videoDuration = ref(0);
                const videoMuted = ref(false);
                const isVideoCasting = ref(false);
                const videoCastingTo = ref(null);
                let videoCastSession = null;

                // Google Cast SDK state (SDK not yet loaded — keep refs for future use)
                const castAvailable = ref(false);
                let castContext = null;

                // Playlist state
                const showPlaylistMenu = ref(false);
                const playlistMenuX = ref(0);
                const playlistMenuY = ref(0);
                const playlistMenuSong = ref(null);
                const playlistMenuPane2 = ref(false);
                const availablePlaylists = ref([]);
                const viewingPlaylist = ref(null);
                const playlistSongs = ref([]);

                // Album state
                const showAlbumMenu = ref(false);
                const albumMenuX = ref(0);
                const albumMenuY = ref(0);
                const albumMenuImage = ref(null);
                const albumMenuPane2 = ref(false);
                const availableAlbums = ref([]);

                // Audio elements
                const audioA = ref(null);
                const audioB = ref(null);
                const activeAudio = ref('A');
                const crossfading = ref(false);

                // Web Audio for crossfade
                let audioContext = null;
                let gainNodeA = null;
                let gainNodeB = null;

                // Computed properties
                const currentTrack = computed(() =>
                    currentIndex.value >= 0 && currentIndex.value < queue.value.length
                        ? queue.value[currentIndex.value]
                        : null
                );

                const progressPercent = computed(() =>
                    duration.value > 0 ? (currentTime.value / duration.value) * 100 : 0
                );

                const pathParts = computed(() => {
                    if (!currentPath.value) return [];
                    return currentPath.value.split(/[\\/]/).filter(p => p);
                });

                const folderCount = computed(() => entries.value.filter(e => e.type === 'dir').length);
                const fileCount = computed(() => entries.value.filter(e => e.type === 'file').length);
                const totalSize = computed(() => entries.value.filter(e => e.type === 'file').reduce((sum, e) => sum + e.size, 0));

                const sortedEntries = computed(() => {
                    const sorted = [...entries.value].sort((a, b) => {
                        if (a.type !== b.type) return a.type === 'dir' ? -1 : 1;
                        let cmp = 0;
                        switch (sortColumn.value) {
                            case 'name':
                                cmp = a.name.localeCompare(b.name, undefined, { sensitivity: 'base' });
                                break;
                            case 'type':
                                cmp = getExtension(a.name).localeCompare(getExtension(b.name));
                                break;
                            case 'size':
                                cmp = a.size - b.size;
                                break;
                            case 'modified':
                                cmp = new Date(a.modified) - new Date(b.modified);
                                break;
                        }
                        return sortAsc.value ? cmp : -cmp;
                    });
                    return sorted;
                });

                // Media view computed properties
                const imageEntries = computed(() => sortedEntries.value.filter(e => e.type === 'file' && isImage(e.name)));
                const audioEntries = computed(() => sortedEntries.value.filter(e => e.type === 'file' && isAudio(e.name)));
                const videoEntries = computed(() => sortedEntries.value.filter(e => e.type === 'file' && isVideo(e.name)));
                const otherEntries = computed(() => sortedEntries.value.filter(e => e.type === 'dir' || (!isImage(e.name) && !isAudio(e.name) && !isVideo(e.name))));

                // Pane 2 media view entries
                const imageEntries2 = computed(() => pane2SortedEntries.value.filter(e => e.type === 'file' && isImage(e.name)));
                const audioEntries2 = computed(() => pane2SortedEntries.value.filter(e => e.type === 'file' && isAudio(e.name)));
                const videoEntries2 = computed(() => pane2SortedEntries.value.filter(e => e.type === 'file' && isVideo(e.name)));
                const otherEntries2 = computed(() => pane2SortedEntries.value.filter(e => e.type === 'dir' || (!isImage(e.name) && !isAudio(e.name) && !isVideo(e.name))));

                // Image list for navigation (filtered to only images)
                const imageList = computed(() =>
                    sortedEntries.value.filter(e => e.type === 'file' && isImage(e.name))
                );

                const currentImageIndex = computed(() => {
                    if (!viewingImage.value) return -1;
                    return imageList.value.findIndex(e => e.name === viewingImage.value.name);
                });

                const canPrevImage = computed(() => currentImageIndex.value > 0);
                const canNextImage = computed(() =>
                    currentImageIndex.value >= 0 && currentImageIndex.value < imageList.value.length - 1
                );

                // Pane 2 computed properties
                const pane2PathParts = computed(() => {
                    if (!pane2Path.value) return [];
                    return pane2Path.value.split(/[\\/]/).filter(p => p);
                });

                const pane2FolderCount = computed(() => pane2Entries.value.filter(e => e.type === 'dir').length);
                const pane2FileCount = computed(() => pane2Entries.value.filter(e => e.type === 'file').length);
                const pane2TotalSize = computed(() => pane2Entries.value.filter(e => e.type === 'file').reduce((sum, e) => sum + e.size, 0));

                const pane2SortedEntries = computed(() => {
                    const sorted = [...pane2Entries.value].sort((a, b) => {
                        if (a.type !== b.type) return a.type === 'dir' ? -1 : 1;
                        let cmp = 0;
                        switch (pane2SortColumn.value) {
                            case 'name':
                                cmp = a.name.localeCompare(b.name, undefined, { sensitivity: 'base' });
                                break;
                            case 'type':
                                cmp = getExtension(a.name).localeCompare(getExtension(b.name));
                                break;
                            case 'size':
                                cmp = a.size - b.size;
                                break;
                            case 'modified':
                                cmp = new Date(a.modified) - new Date(b.modified);
                                break;
                        }
                        return pane2SortAsc.value ? cmp : -cmp;
                    });
                    return sorted;
                });

                const pane2FullPath = (name) => pane2Path.value ? pane2Path.value + '\\' + name : name;

                // Helper functions
                const formatSize = (bytes) => {
                    if (bytes === 0) return '-';
                    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
                    const i = Math.floor(Math.log(bytes) / Math.log(1024));
                    return (bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0) + ' ' + units[i];
                };

                const formatDate = (isoString) => {
                    const date = new Date(isoString);
                    return date.toLocaleDateString() + ' ' + date.toLocaleTimeString();
                };

                const formatTime = (seconds) => {
                    if (!seconds || isNaN(seconds)) return '0:00';
                    const m = Math.floor(seconds / 60);
                    const s = Math.floor(seconds % 60);
                    return m + ':' + s.toString().padStart(2, '0');
                };

                const getExtension = (name) => {
                    const idx = name.lastIndexOf('.');
                    return idx > 0 ? name.substring(idx + 1).toLowerCase() : '';
                };

                const isAudio = (name) => AUDIO_EXTENSIONS.includes(getExtension(name));
                const isImage = (name) => IMAGE_EXTENSIONS.includes(getExtension(name));
                const isVideo = (name) => VIDEO_EXTENSIONS.includes(getExtension(name));
                const isPlaylist = (name) => {
                    const ext = getExtension(name);
                    return ext === 'm3u8' || ext === 'm3u';
                };

                const playlistMenuStyle = computed(() => ({
                    top: playlistMenuY.value + 'px',
                    left: playlistMenuX.value + 'px'
                }));

                const albumMenuStyle = computed(() => ({
                    top: albumMenuY.value + 'px',
                    left: albumMenuX.value + 'px'
                }));

                const fullPath = (name) => {
                    const sep = currentPath.value.includes('\\') ? '\\' : '/';
                    return currentPath.value + sep + name;
                };

                const sortIndicator = (col) => sortColumn.value === col ? (sortAsc.value ? '▲' : '▼') : '';

                // File browser functions
                const loadRoots = async (doUpdateUrl = true) => {
                    loading.value = true;
                    error.value = null;
                    currentPath.value = null;
                    try {
                        const resp = await fetch('/api/roots');
                        const data = await resp.json();
                        if (data.error) throw new Error(data.error);
                        roots.value = data.roots;
                        if (doUpdateUrl) {
                            updateUrl();
                        }
                    } catch (e) {
                        error.value = 'Failed to load: ' + e.message;
                    }
                    loading.value = false;
                };

                const browse = async (path, doUpdateUrl = true) => {
                    loading.value = true;
                    error.value = null;
                    try {
                        // Include metadata param when in media view mode
                        const metaParam = viewMode.value === 'media' ? '&metadata=true' : '';
                        const resp = await fetch('/api/browse?path=' + encodeURIComponent(path) + metaParam);
                        const data = await resp.json();
                        if (data.error) throw new Error(data.error);
                        currentPath.value = data.path;
                        entries.value = data.entries;
                        sortColumn.value = 'name';
                        sortAsc.value = true;
                        if (doUpdateUrl) {
                            updateUrl();
                        }
                    } catch (e) {
                        error.value = 'Failed to load: ' + e.message;
                    }
                    loading.value = false;
                };

                // Browse with metadata (for media view)
                const browseWithMetadata = async (path) => {
                    loading.value = true;
                    error.value = null;
                    try {
                        const resp = await fetch('/api/browse?path=' + encodeURIComponent(path) + '&metadata=true');
                        const data = await resp.json();
                        if (data.error) throw new Error(data.error);
                        currentPath.value = data.path;
                        entries.value = data.entries;
                        sortColumn.value = 'name';
                        sortAsc.value = true;
                    } catch (e) {
                        error.value = 'Failed to load: ' + e.message;
                    }
                    loading.value = false;
                };

                const browseTo = (parts) => {
                    const sep = currentPath.value.includes('\\') ? '\\' : '/';
                    let path = parts.join(sep);
                    if (currentPath.value.match(/^[a-zA-Z]:/)) {
                        // For Windows paths, ensure drive root has trailing separator (P:\ not P:)
                        path = parts[0] + sep + (parts.length > 1 ? parts.slice(1).join(sep) : '');
                    }
                    browse(path);
                };

                const changeSort = (column) => {
                    if (sortColumn.value === column) {
                        sortAsc.value = !sortAsc.value;
                    } else {
                        sortColumn.value = column;
                        sortAsc.value = true;
                    }
                };

                // Dual pane toggle and functions
                const toggleDualPane = () => {
                    dualPane.value = !dualPane.value;
                    updateUrl();
                };

                // View mode toggle
                const toggleViewMode = () => {
                    viewMode.value = viewMode.value === 'file' ? 'media' : 'file';
                    updateUrl();
                    // Reload with metadata when switching to media view
                    if (viewMode.value === 'media' && currentPath.value) {
                        browseWithMetadata(currentPath.value);
                    }
                };

                // Browse with metadata for pane 2
                const browseWithMetadata2 = async (path) => {
                    pane2Loading.value = true;
                    pane2Error.value = null;
                    try {
                        const resp = await fetch('/api/browse?path=' + encodeURIComponent(path) + '&metadata=true');
                        const data = await resp.json();
                        if (data.error) throw new Error(data.error);
                        pane2Path.value = data.path;
                        pane2Entries.value = data.entries;
                        pane2SortColumn.value = 'name';
                        pane2SortAsc.value = true;
                    } catch (e) {
                        pane2Error.value = 'Failed to load: ' + e.message;
                    }
                    pane2Loading.value = false;
                };

                // View mode toggle for pane 2
                const toggleViewMode2 = () => {
                    viewMode2.value = viewMode2.value === 'file' ? 'media' : 'file';
                    updateUrl();
                    // Reload with metadata when switching to media view
                    if (viewMode2.value === 'media' && pane2Path.value) {
                        browseWithMetadata2(pane2Path.value);
                    }
                };

                // Format duration in seconds to mm:ss
                const formatDuration = (seconds) => {
                    if (!seconds) return '-';
                    const mins = Math.floor(seconds / 60);
                    const secs = seconds % 60;
                    return mins + ':' + String(secs).padStart(2, '0');
                };

                // Handle thumbnail load error (show placeholder)
                const handleThumbError = (e) => {
                    e.target.src = 'data:image/svg+xml,' + encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100" viewBox="0 0 100 100"><rect fill="%2321262d" width="100" height="100"/><text x="50" y="55" text-anchor="middle" fill="%236e7681" font-size="12">No Preview</text></svg>');
                };

                const loadRoots2 = (doUpdateUrl = true) => {
                    pane2Path.value = null;
                    pane2Entries.value = [];
                    pane2Error.value = null;
                    if (doUpdateUrl) {
                        updateUrl();
                    }
                };

                const browse2 = async (path, doUpdateUrl = true) => {
                    pane2Loading.value = true;
                    pane2Error.value = null;
                    try {
                        const resp = await fetch('/api/browse?path=' + encodeURIComponent(path));
                        const data = await resp.json();
                        if (data.error) throw new Error(data.error);
                        pane2Path.value = data.path;
                        pane2Entries.value = data.entries;
                        pane2SortColumn.value = 'name';
                        pane2SortAsc.value = true;
                        if (doUpdateUrl) {
                            updateUrl();
                        }
                    } catch (e) {
                        pane2Error.value = 'Failed to load: ' + e.message;
                    }
                    pane2Loading.value = false;
                };

                const browseTo2 = (parts) => {
                    const sep = pane2Path.value.includes('\\') ? '\\' : '/';
                    let path = parts.join(sep);
                    if (pane2Path.value.match(/^[a-zA-Z]:/)) {
                        // For Windows paths, ensure drive root has trailing separator (P:\ not P:)
                        path = parts[0] + sep + (parts.length > 1 ? parts.slice(1).join(sep) : '');
                    }
                    browse2(path);
                };

                const changeSort2 = (column) => {
                    if (pane2SortColumn.value === column) {
                        pane2SortAsc.value = !pane2SortAsc.value;
                    } else {
                        pane2SortColumn.value = column;
                        pane2SortAsc.value = true;
                    }
                };

                const sortIndicator2 = (column) => {
                    if (pane2SortColumn.value !== column) return '';
                    return pane2SortAsc.value ? '▲' : '▼';
                };

                // Pane 2 media functions (use same players, just different path)
                const openImage2 = (entry) => {
                    // Use pane2Path for the full path
                    viewingImage.value = { ...entry, _pane2Path: pane2Path.value };
                };

                const openVideo2 = (entry) => {
                    videoFile.value = { ...entry, _pane2Path: pane2Path.value };
                    videoPlaying.value = false;
                    videoCurrentTime.value = 0;
                    videoDuration.value = 0;
                };

                const playNow2 = (entry) => {
                    const track = { path: pane2FullPath(entry.name), name: entry.name };
                    queue.value = [track];
                    currentIndex.value = -1;
                    playTrack(0);
                };

                const addToQueueTop2 = (entry) => {
                    const track = { path: pane2FullPath(entry.name), name: entry.name };
                    const insertAt = currentIndex.value >= 0 ? currentIndex.value + 1 : 0;
                    queue.value.splice(insertAt, 0, track);
                    if (currentIndex.value < 0 && queue.value.length === 1) {
                        playTrack(0);
                    }
                    saveState();
                };

                const addToQueueBottom2 = (entry) => {
                    const track = { path: pane2FullPath(entry.name), name: entry.name };
                    queue.value.push(track);
                    if (currentIndex.value < 0 && queue.value.length === 1) {
                        playTrack(0);
                    }
                    saveState();
                };

                // Image viewer functions
                const openImage = (entry) => {
                    viewingImage.value = entry;
                };

                const closeImage = () => {
                    viewingImage.value = null;
                };

                const prevImage = () => {
                    const idx = currentImageIndex.value;
                    if (idx > 0) {
                        viewingImage.value = imageList.value[idx - 1];
                    }
                };

                const nextImage = () => {
                    const idx = currentImageIndex.value;
                    if (idx >= 0 && idx < imageList.value.length - 1) {
                        viewingImage.value = imageList.value[idx + 1];
                    }
                };

                // Keyboard handler for image viewer
                const handleKeydown = (e) => {
                    if (videoFile.value && e.key === 'Escape') {
                        closeVideo();
                        return;
                    }
                    if (!viewingImage.value) return;
                    if (e.key === 'Escape') closeImage();
                    if (e.key === 'ArrowLeft') prevImage();
                    if (e.key === 'ArrowRight') nextImage();
                };

                // Video player functions
                const videoProgressPercent = computed(() =>
                    videoDuration.value > 0 ? (videoCurrentTime.value / videoDuration.value) * 100 : 0
                );

                const openVideo = (entry) => {
                    videoFile.value = entry;
                    videoPlaying.value = false;
                    videoCurrentTime.value = 0;
                    videoDuration.value = 0;
                };

                const closeVideo = () => {
                    // Stop casting if active
                    if (isVideoCasting.value && videoCastSession) {
                        videoCastSession.endSession(true);
                        videoCastSession = null;
                        isVideoCasting.value = false;
                        videoCastingTo.value = null;
                    }
                    if (videoRef.value) {
                        videoRef.value.pause();
                    }
                    videoFile.value = null;
                    videoPlaying.value = false;
                };

                const toggleVideoPlay = () => {
                    if (!videoRef.value) return;
                    if (videoPlaying.value) {
                        videoRef.value.pause();
                    } else {
                        videoRef.value.play();
                    }
                };

                const onVideoTimeUpdate = () => {
                    if (videoRef.value) {
                        videoCurrentTime.value = videoRef.value.currentTime;
                    }
                };

                const onVideoMetadata = () => {
                    if (videoRef.value) {
                        videoDuration.value = videoRef.value.duration;
                    }
                };

                const onVideoPlay = () => { videoPlaying.value = true; };
                const onVideoPause = () => { videoPlaying.value = false; };
                const onVideoEnded = () => { videoPlaying.value = false; };

                const seekVideo = (event) => {
                    if (!videoRef.value) return;
                    const rect = event.currentTarget.getBoundingClientRect();
                    const percent = (event.clientX - rect.left) / rect.width;
                    videoRef.value.currentTime = percent * videoDuration.value;
                };

                const toggleVideoMute = () => {
                    videoMuted.value = !videoMuted.value;
                    if (videoRef.value) {
                        videoRef.value.muted = videoMuted.value;
                    }
                };

                // Video casting functions
                const getVideoContentType = (filename) => {
                    const ext = filename.toLowerCase().split('.').pop();
                    const types = {
                        'mp4': 'video/mp4',
                        'webm': 'video/webm',
                        'ogv': 'video/ogg',
                        'mov': 'video/quicktime',
                        'avi': 'video/x-msvideo',
                        'mkv': 'video/x-matroska',
                        'm4v': 'video/mp4'
                    };
                    return types[ext] || 'video/mp4';
                };

                const openVideoCastPicker = async () => {
                    if (!castAvailable.value || !castContext) return;

                    try {
                        await castContext.requestSession();
                        // Session started, now load the video
                        videoCastSession = castContext.getCurrentSession();
                        if (videoCastSession && videoFile.value) {
                            castCurrentVideo();
                        }
                    } catch (err) {
                        if (err.code !== 'cancel') {
                            console.log('Video cast request failed:', err);
                        }
                    }
                };

                const castCurrentVideo = () => {
                    if (!videoCastSession || !videoFile.value) return;

                    // Pause local video
                    if (videoRef.value) {
                        videoRef.value.pause();
                    }

                    const videoPath = fullPath(videoFile.value.name);
                    const streamUrl = window.location.origin + '/api/video?path=' + encodeURIComponent(videoPath);
                    const contentType = getVideoContentType(videoFile.value.name);

                    console.log('Casting video:', streamUrl, 'as', contentType);

                    const mediaInfo = new chrome.cast.media.MediaInfo(streamUrl, contentType);
                    mediaInfo.metadata = new chrome.cast.media.GenericMediaMetadata();
                    mediaInfo.metadata.title = videoFile.value.name;

                    const request = new chrome.cast.media.LoadRequest(mediaInfo);
                    request.autoplay = true;
                    request.currentTime = videoCurrentTime.value;

                    videoCastSession.loadMedia(request).then(
                        () => {
                            console.log('Video loaded on cast device');
                            isVideoCasting.value = true;
                            videoCastingTo.value = videoCastSession.getCastDevice().friendlyName;
                            videoPlaying.value = true;
                        },
                        (err) => {
                            console.error('Failed to load video on cast:', err);
                            isVideoCasting.value = false;
                        }
                    );
                };

                const stopVideoCasting = () => {
                    if (videoCastSession) {
                        videoCastSession.endSession(true);
                    }
                    videoCastSession = null;
                    isVideoCasting.value = false;
                    videoCastingTo.value = null;
                    // Resume local playback if video is still open
                    if (videoRef.value && videoFile.value) {
                        videoRef.value.play();
                    }
                };

                // Cast functions (backend-based)
                const toggleCastPanel = () => {
                    showCastPanel.value = !showCastPanel.value;
                    showQueue.value = false; // Close queue panel
                    // Auto-scan when opening
                    if (showCastPanel.value && castDevices.value.length === 0) {
                        scanCastDevices();
                    }
                };

                const stopCasting = async () => {
                    const wasPlaying = isPlaying.value;
                    const resumeTime = currentTime.value;

                    try {
                        await fetch('/api/cast/disconnect', { method: 'POST' });
                    } catch (e) {
                        console.error('Failed to disconnect:', e);
                    }
                    if (castStatusInterval) {
                        clearInterval(castStatusInterval);
                        castStatusInterval = null;
                    }
                    isCasting.value = false;
                    castingTo.value = null;
                    showCastPanel.value = false;

                    // Resume local playback if we were playing and have a track loaded
                    if (currentTrack.value) {
                        const audio = getActiveAudioElement();
                        // Ensure the track is loaded
                        if (!audio.src || !audio.src.includes(encodeURIComponent(currentTrack.value.path))) {
                            audio.src = '/api/stream?path=' + encodeURIComponent(currentTrack.value.path);
                        }
                        audio.currentTime = resumeTime;
                        if (wasPlaying) {
                            audio.play().then(() => {
                                isPlaying.value = true;
                            }).catch(e => {
                                console.error('Failed to resume playback:', e);
                            });
                        }
                    }
                };

                const castCurrentTrack = async () => {
                    if (!isCasting.value || !currentTrack.value) return;

                    try {
                        const resp = await fetch('/api/cast/play', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({
                                path: currentTrack.value.path,
                                title: currentTrack.value.name
                            })
                        });
                        const data = await resp.json();
                        if (data.success) {
                            console.log('Media loaded on cast device:', data.media_url);
                            isPlaying.value = true;
                        } else {
                            console.error('Failed to cast:', data.error);
                        }
                        console.log('Cast response:', data);
                    } catch (e) {
                        console.error('Failed to cast media:', e);
                    }
                };

                const castTogglePlay = async () => {
                    if (!isCasting.value) return;
                    try {
                        if (isPlaying.value) {
                            const resp = await fetch('/api/cast/pause', { method: 'POST' });
                            if (resp.ok) isPlaying.value = false;
                        } else {
                            const resp = await fetch('/api/cast/resume', { method: 'POST' });
                            if (resp.ok) isPlaying.value = true;
                        }
                    } catch (e) {
                        console.error('Failed to toggle cast play:', e);
                    }
                };

                const castSeek = async (percent) => {
                    if (!isCasting.value) return;
                    // Convert percent to seconds using duration
                    const seekTime = percent * duration.value;
                    try {
                        await fetch('/api/cast/seek', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ position: seekTime })
                        });
                        currentTime.value = seekTime;
                    } catch (e) {
                        console.error('Failed to seek on cast:', e);
                    }
                };

                // Audio player functions
                const initAudioContext = () => {
                    if (audioContext) return;
                    try {
                        audioContext = new (window.AudioContext || window.webkitAudioContext)();

                        const sourceA = audioContext.createMediaElementSource(audioA.value);
                        gainNodeA = audioContext.createGain();
                        sourceA.connect(gainNodeA);
                        gainNodeA.connect(audioContext.destination);

                        const sourceB = audioContext.createMediaElementSource(audioB.value);
                        gainNodeB = audioContext.createGain();
                        sourceB.connect(gainNodeB);
                        gainNodeB.connect(audioContext.destination);
                    } catch (e) {
                        console.warn('Web Audio API not available, crossfade will use volume fallback');
                    }
                };

                const getActiveAudioElement = () => activeAudio.value === 'A' ? audioA.value : audioB.value;
                const getInactiveAudioElement = () => activeAudio.value === 'A' ? audioB.value : audioA.value;
                const getActiveGain = () => activeAudio.value === 'A' ? gainNodeA : gainNodeB;
                const getInactiveGain = () => activeAudio.value === 'A' ? gainNodeB : gainNodeA;

                const playTrack = (index) => {
                    if (index < 0 || index >= queue.value.length) return;

                    initAudioContext();
                    if (audioContext && audioContext.state === 'suspended') {
                        audioContext.resume();
                    }

                    currentIndex.value = index;
                    const track = queue.value[index];
                    const audio = getActiveAudioElement();

                    audio.src = '/api/stream?path=' + encodeURIComponent(track.path);
                    audio.play().then(() => {
                        isPlaying.value = true;
                    }).catch(e => console.error('Play error:', e));

                    saveState();
                };

                const playNow = (entry) => {
                    const track = { path: fullPath(entry.name), name: entry.name };
                    queue.value = [track];
                    currentIndex.value = -1;
                    playTrack(0);
                };

                const addToQueueTop = (entry) => {
                    const track = { path: fullPath(entry.name), name: entry.name };
                    const insertAt = currentIndex.value >= 0 ? currentIndex.value + 1 : 0;
                    queue.value.splice(insertAt, 0, track);
                    if (currentIndex.value < 0 && queue.value.length === 1) {
                        playTrack(0);
                    }
                    saveState();
                };

                const addToQueueBottom = (entry) => {
                    const track = { path: fullPath(entry.name), name: entry.name };
                    queue.value.push(track);
                    if (currentIndex.value < 0 && queue.value.length === 1) {
                        playTrack(0);
                    }
                    saveState();
                };

                const togglePlay = () => {
                    // If casting, control the Cast device
                    if (isCasting.value) {
                        castTogglePlay();
                        return;
                    }
                    const audio = getActiveAudioElement();
                    if (isPlaying.value) {
                        audio.pause();
                        isPlaying.value = false;
                    } else if (currentTrack.value) {
                        audio.play().then(() => {
                            isPlaying.value = true;
                        });
                    }
                };

                const playNext = () => {
                    if (currentIndex.value < queue.value.length - 1) {
                        if (crossfadeEnabled.value && isPlaying.value) {
                            startCrossfade(currentIndex.value + 1);
                        } else {
                            playTrack(currentIndex.value + 1);
                        }
                    }
                };

                const playPrevious = () => {
                    const audio = getActiveAudioElement();
                    if (audio.currentTime > 3) {
                        audio.currentTime = 0;
                    } else if (currentIndex.value > 0) {
                        playTrack(currentIndex.value - 1);
                    }
                };

                const startCrossfade = (nextIndex) => {
                    if (crossfading.value || nextIndex >= queue.value.length) return;

                    crossfading.value = true;
                    const track = queue.value[nextIndex];
                    const inactiveAudio = getInactiveAudioElement();
                    const activeGain = getActiveGain();
                    const inactiveGain = getInactiveGain();

                    // Load next track on inactive element
                    inactiveAudio.src = '/api/stream?path=' + encodeURIComponent(track.path);

                    if (audioContext && activeGain && inactiveGain) {
                        // Use Web Audio for smooth crossfade
                        inactiveGain.gain.value = 0;
                        inactiveAudio.play().then(() => {
                            const now = audioContext.currentTime;
                            activeGain.gain.linearRampToValueAtTime(0, now + CROSSFADE_DURATION);
                            inactiveGain.gain.linearRampToValueAtTime(1, now + CROSSFADE_DURATION);

                            setTimeout(() => {
                                getActiveAudioElement().pause();
                                activeAudio.value = activeAudio.value === 'A' ? 'B' : 'A';
                                currentIndex.value = nextIndex;
                                crossfading.value = false;
                                saveState();
                            }, CROSSFADE_DURATION * 1000);
                        });
                    } else {
                        // Fallback: simple volume crossfade
                        const activeAudioEl = getActiveAudioElement();
                        inactiveAudio.volume = 0;
                        inactiveAudio.play().then(() => {
                            const steps = 30;
                            const stepTime = (CROSSFADE_DURATION * 1000) / steps;
                            let step = 0;
                            const interval = setInterval(() => {
                                step++;
                                activeAudioEl.volume = Math.max(0, 1 - step / steps);
                                inactiveAudio.volume = Math.min(1, step / steps);
                                if (step >= steps) {
                                    clearInterval(interval);
                                    activeAudioEl.pause();
                                    activeAudioEl.volume = 1;
                                    activeAudio.value = activeAudio.value === 'A' ? 'B' : 'A';
                                    currentIndex.value = nextIndex;
                                    crossfading.value = false;
                                    saveState();
                                }
                            }, stepTime);
                        });
                    }
                };

                const seek = (event) => {
                    const bar = event.currentTarget;
                    const rect = bar.getBoundingClientRect();
                    const percent = (event.clientX - rect.left) / rect.width;
                    // If casting, seek on the Cast device
                    if (isCasting.value) {
                        castSeek(percent);
                        return;
                    }
                    const audio = getActiveAudioElement();
                    audio.currentTime = percent * audio.duration;
                };

                const onTimeUpdate = (event) => {
                    const audio = event.target;
                    if (audio === getActiveAudioElement()) {
                        currentTime.value = audio.currentTime;

                        // Check for crossfade trigger
                        if (crossfadeEnabled.value && !crossfading.value &&
                            audio.duration && audio.currentTime > 0 &&
                            audio.duration - audio.currentTime <= CROSSFADE_DURATION &&
                            currentIndex.value < queue.value.length - 1) {
                            startCrossfade(currentIndex.value + 1);
                        }
                    }
                };

                const onMetadata = (event) => {
                    const audio = event.target;
                    if (audio === getActiveAudioElement()) {
                        duration.value = audio.duration;
                    }
                };

                const onEnded = (event) => {
                    const audio = event.target;
                    if (audio === getActiveAudioElement() && !crossfading.value) {
                        if (currentIndex.value < queue.value.length - 1) {
                            playTrack(currentIndex.value + 1);
                        } else {
                            isPlaying.value = false;
                        }
                    }
                };

                // Mute control
                const toggleMute = () => {
                    isMuted.value = !isMuted.value;
                    if (audioA.value) audioA.value.muted = isMuted.value;
                    if (audioB.value) audioB.value.muted = isMuted.value;
                    saveState();
                };

                // Queue management
                const toggleQueue = () => {
                    showQueue.value = !showQueue.value;
                };

                const removeFromQueue = (index) => {
                    if (index === currentIndex.value) {
                        // Removing currently playing track
                        if (queue.value.length > 1) {
                            if (index < queue.value.length - 1) {
                                queue.value.splice(index, 1);
                                playTrack(index);
                            } else {
                                queue.value.splice(index, 1);
                                currentIndex.value = -1;
                                isPlaying.value = false;
                                getActiveAudioElement().pause();
                            }
                        } else {
                            queue.value = [];
                            currentIndex.value = -1;
                            isPlaying.value = false;
                            getActiveAudioElement().pause();
                        }
                    } else {
                        if (index < currentIndex.value) {
                            currentIndex.value--;
                        }
                        queue.value.splice(index, 1);
                    }
                    saveState();
                };

                const clearQueue = () => {
                    queue.value = [];
                    currentIndex.value = -1;
                    isPlaying.value = false;
                    getActiveAudioElement()?.pause();
                    saveState();
                };

                const moveUp = (index) => {
                    if (index <= 0) return;
                    const item = queue.value.splice(index, 1)[0];
                    queue.value.splice(index - 1, 0, item);
                    if (currentIndex.value === index) currentIndex.value--;
                    else if (currentIndex.value === index - 1) currentIndex.value++;
                    saveState();
                };

                const moveDown = (index) => {
                    if (index >= queue.value.length - 1) return;
                    const item = queue.value.splice(index, 1)[0];
                    queue.value.splice(index + 1, 0, item);
                    if (currentIndex.value === index) currentIndex.value++;
                    else if (currentIndex.value === index + 1) currentIndex.value--;
                    saveState();
                };

                // Persistence
                const saveState = () => {
                    localStorage.setItem('q2-queue', JSON.stringify(queue.value));
                    localStorage.setItem('q2-currentIndex', currentIndex.value.toString());
                    localStorage.setItem('q2-crossfade', crossfadeEnabled.value.toString());
                    localStorage.setItem('q2-muted', isMuted.value.toString());
                };

                const loadState = () => {
                    try {
                        const savedQueue = localStorage.getItem('q2-queue');
                        if (savedQueue) queue.value = JSON.parse(savedQueue);

                        const savedIndex = localStorage.getItem('q2-currentIndex');
                        if (savedIndex) currentIndex.value = parseInt(savedIndex, 10);

                        const savedCrossfade = localStorage.getItem('q2-crossfade');
                        if (savedCrossfade) crossfadeEnabled.value = savedCrossfade === 'true';

                        const savedMuted = localStorage.getItem('q2-muted');
                        if (savedMuted) isMuted.value = savedMuted === 'true';
                    } catch (e) {
                        console.error('Failed to load state:', e);
                    }
                };

                // Playlist functions
                const openPlaylistMenu = async (event, entry, isPane2 = false) => {
                    const songPath = isPane2 ? pane2FullPath(entry.name) : fullPath(entry.name);
                    playlistMenuSong.value = { path: songPath, name: entry.name };
                    playlistMenuPane2.value = isPane2;

                    // Position popup near button
                    const rect = event.target.getBoundingClientRect();
                    playlistMenuX.value = Math.min(rect.left, window.innerWidth - 250);
                    playlistMenuY.value = rect.bottom + 5;

                    // Fetch playlists with contains info
                    try {
                        const resp = await fetch('/api/playlist/check?song=' + encodeURIComponent(songPath));
                        const data = await resp.json();
                        availablePlaylists.value = data.playlists || [];
                    } catch (e) {
                        console.error('Failed to load playlists:', e);
                        availablePlaylists.value = [];
                    }

                    showPlaylistMenu.value = true;
                };

                const closePlaylistMenu = () => {
                    showPlaylistMenu.value = false;
                    playlistMenuSong.value = null;
                };

                const addToPlaylist = async (playlistPath) => {
                    if (!playlistMenuSong.value) return;

                    try {
                        await fetch('/api/playlist/add', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({
                                playlist: playlistPath,
                                song: playlistMenuSong.value.path,
                                title: playlistMenuSong.value.name,
                                duration: 0
                            })
                        });
                    } catch (e) {
                        console.error('Failed to add to playlist:', e);
                    }

                    closePlaylistMenu();
                };

                const createNewPlaylist = async () => {
                    const name = prompt('Enter playlist name:');
                    if (!name) return;

                    try {
                        const createResp = await fetch('/api/playlist', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ name })
                        });
                        const createData = await createResp.json();

                        if (createData.success && playlistMenuSong.value) {
                            await addToPlaylist(createData.path);
                        } else {
                            closePlaylistMenu();
                        }
                    } catch (e) {
                        console.error('Failed to create playlist:', e);
                        closePlaylistMenu();
                    }
                };

                // Album functions
                const openAlbumMenu = async (event, entry, isPane2 = false) => {
                    const imagePath = isPane2 ? pane2FullPath(entry.name) : fullPath(entry.name);
                    albumMenuImage.value = { path: imagePath, name: entry.name };
                    albumMenuPane2.value = isPane2;

                    // Position popup near button
                    const rect = event.target.getBoundingClientRect();
                    albumMenuX.value = Math.min(rect.left, window.innerWidth - 250);
                    albumMenuY.value = rect.bottom + 5;

                    // Fetch albums with contains info
                    try {
                        const resp = await fetch('/api/album/check?path=' + encodeURIComponent(imagePath));
                        const data = await resp.json();
                        availableAlbums.value = data.albums || [];
                    } catch (e) {
                        console.error('Failed to load albums:', e);
                        availableAlbums.value = [];
                    }

                    showAlbumMenu.value = true;
                };

                const closeAlbumMenu = () => {
                    showAlbumMenu.value = false;
                    albumMenuImage.value = null;
                };

                const addToAlbum = async (albumId) => {
                    if (!albumMenuImage.value) return;

                    try {
                        await fetch('/api/album/add', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({
                                album_id: albumId,
                                path: albumMenuImage.value.path
                            })
                        });
                    } catch (e) {
                        console.error('Failed to add to album:', e);
                    }

                    closeAlbumMenu();
                };

                const createNewAlbum = async () => {
                    const name = prompt('Enter album name:');
                    if (!name) return;

                    try {
                        const createResp = await fetch('/api/album', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ name })
                        });
                        const createData = await createResp.json();

                        if (createData.success && albumMenuImage.value) {
                            await addToAlbum(createData.id);
                        } else {
                            closeAlbumMenu();
                        }
                    } catch (e) {
                        console.error('Failed to create album:', e);
                        closeAlbumMenu();
                    }
                };

                const openPlaylist = async (entry) => {
                    const path = fullPath(entry.name);
                    try {
                        const resp = await fetch('/api/playlist?path=' + encodeURIComponent(path));
                        const data = await resp.json();
                        if (data.error) {
                            console.error('Failed to load playlist:', data.error);
                            return;
                        }
                        viewingPlaylist.value = { name: data.name, path: path };
                        playlistSongs.value = data.songs || [];
                    } catch (e) {
                        console.error('Failed to load playlist:', e);
                    }
                };

                const closePlaylistViewer = () => {
                    viewingPlaylist.value = null;
                    playlistSongs.value = [];
                };

                const playAllFromPlaylist = () => {
                    if (playlistSongs.value.length === 0) return;
                    queue.value = playlistSongs.value.map(s => ({ path: s.path, name: s.title }));
                    currentIndex.value = 0;
                    playTrack(0);
                    closePlaylistViewer();
                };

                const shuffleAndPlayPlaylist = () => {
                    if (playlistSongs.value.length === 0) return;
                    const shuffled = [...playlistSongs.value].sort(() => Math.random() - 0.5);
                    queue.value = shuffled.map(s => ({ path: s.path, name: s.title }));
                    currentIndex.value = 0;
                    playTrack(0);
                    closePlaylistViewer();
                };

                const playSongFromPlaylist = (index) => {
                    const song = playlistSongs.value[index];
                    if (!song) return;
                    queue.value = [{ path: song.path, name: song.title }];
                    currentIndex.value = 0;
                    playTrack(0);
                };

                const movePlaylistSongUp = async (index) => {
                    if (index <= 0 || !viewingPlaylist.value) return;
                    try {
                        await fetch('/api/playlist/reorder', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({
                                playlist: viewingPlaylist.value.path,
                                from_index: index,
                                to_index: index - 1
                            })
                        });
                        // Refresh playlist
                        const resp = await fetch('/api/playlist?path=' + encodeURIComponent(viewingPlaylist.value.path));
                        const data = await resp.json();
                        playlistSongs.value = data.songs || [];
                    } catch (e) {
                        console.error('Failed to reorder:', e);
                    }
                };

                const movePlaylistSongDown = async (index) => {
                    if (index >= playlistSongs.value.length - 1 || !viewingPlaylist.value) return;
                    try {
                        await fetch('/api/playlist/reorder', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({
                                playlist: viewingPlaylist.value.path,
                                from_index: index,
                                to_index: index + 1
                            })
                        });
                        // Refresh playlist
                        const resp = await fetch('/api/playlist?path=' + encodeURIComponent(viewingPlaylist.value.path));
                        const data = await resp.json();
                        playlistSongs.value = data.songs || [];
                    } catch (e) {
                        console.error('Failed to reorder:', e);
                    }
                };

                const removeFromPlaylist = async (index) => {
                    if (!viewingPlaylist.value) return;
                    try {
                        await fetch('/api/playlist/remove', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({
                                playlist: viewingPlaylist.value.path,
                                index: index
                            })
                        });
                        // Refresh playlist
                        const resp = await fetch('/api/playlist?path=' + encodeURIComponent(viewingPlaylist.value.path));
                        const data = await resp.json();
                        playlistSongs.value = data.songs || [];
                    } catch (e) {
                        console.error('Failed to remove song:', e);
                    }
                };

                const deletePlaylist = async () => {
                    if (!viewingPlaylist.value) return;
                    if (!confirm('Delete playlist "' + viewingPlaylist.value.name + '"?')) return;
                    try {
                        await fetch('/api/playlist?path=' + encodeURIComponent(viewingPlaylist.value.path), {
                            method: 'DELETE'
                        });
                        closePlaylistViewer();
                        // Refresh current directory to update file list
                        if (currentPath.value) {
                            browse(currentPath.value);
                        }
                    } catch (e) {
                        console.error('Failed to delete playlist:', e);
                    }
                };

                // Watch crossfade setting
                watch(crossfadeEnabled, () => saveState());

                // Build URL hash from current state
                const buildUrlHash = () => {
                    const params = new URLSearchParams();
                    if (currentPath.value) {
                        params.set('path', currentPath.value);
                    }
                    if (dualPane.value) {
                        params.set('dual', '1');
                        if (pane2Path.value) {
                            params.set('pane2', pane2Path.value);
                        }
                        if (viewMode2.value === 'media') {
                            params.set('view2', 'media');
                        }
                    }
                    if (viewMode.value === 'media') {
                        params.set('view', 'media');
                    }
                    const hashStr = params.toString();
                    return hashStr ? '#' + hashStr : window.location.pathname;
                };

                // Update URL with current state
                const updateUrl = () => {
                    const newUrl = buildUrlHash();
                    history.pushState(null, '', newUrl);
                };

                // Handle URL hash navigation
                const navigateFromHash = async () => {
                    const hash = window.location.hash;
                    if (hash && hash.length > 1) {
                        const params = new URLSearchParams(hash.substring(1));

                        // Restore view mode
                        const view = params.get('view');
                        viewMode.value = view === 'media' ? 'media' : 'file';

                        // Restore dual pane state
                        const dual = params.get('dual');
                        dualPane.value = dual === '1';

                        // Restore pane 2 view mode
                        const view2 = params.get('view2');
                        viewMode2.value = view2 === 'media' ? 'media' : 'file';

                        // Restore main pane
                        const path = params.get('path');
                        if (path) {
                            await browse(path, false);
                        } else {
                            await loadRoots(false);
                        }

                        // Restore pane 2 if dual pane
                        if (dualPane.value) {
                            const pane2 = params.get('pane2');
                            if (pane2) {
                                await browse2(pane2, false);
                            } else {
                                loadRoots2();
                            }
                        }
                    } else {
                        loadRoots(false);
                    }
                };

                // Lifecycle
                onMounted(() => {
                    loadState();
                    // Apply mute state to audio elements
                    if (audioA.value) audioA.value.muted = isMuted.value;
                    if (audioB.value) audioB.value.muted = isMuted.value;
                    // Navigate based on URL hash (or load roots if no hash)
                    navigateFromHash();
                    // Add keyboard listener for image viewer
                    document.addEventListener('keydown', handleKeydown);
                    // Handle browser back/forward buttons
                    window.addEventListener('popstate', navigateFromHash);
                });

                onUnmounted(() => {
                    document.removeEventListener('keydown', handleKeydown);
                    window.removeEventListener('popstate', navigateFromHash);
                });

                return {
                    // File browser
                    roots, currentPath, entries, loading, error,
                    sortColumn, sortAsc, pathParts, folderCount, fileCount, totalSize, sortedEntries,
                    formatSize, formatDate, getExtension, isAudio, isImage, isVideo, isPlaylist, fullPath, sortIndicator,
                    loadRoots, browse, browseTo, changeSort,

                    // Media view
                    viewMode, toggleViewMode, viewMode2, toggleViewMode2,
                    imageEntries, audioEntries, videoEntries, otherEntries,
                    imageEntries2, audioEntries2, videoEntries2, otherEntries2,
                    formatDuration, handleThumbError,

                    // Dual pane
                    dualPane, toggleDualPane,
                    pane2Path, pane2Entries, pane2Loading, pane2Error,
                    pane2SortColumn, pane2SortAsc, pane2PathParts, pane2FolderCount, pane2FileCount, pane2TotalSize,
                    pane2SortedEntries, pane2FullPath, sortIndicator2,
                    loadRoots2, browse2, browseTo2, changeSort2,
                    openImage2, openVideo2, playNow2, addToQueueTop2, addToQueueBottom2,

                    // Image viewer
                    viewingImage, canPrevImage, canNextImage,
                    openImage, closeImage, prevImage, nextImage,

                    // Video player
                    videoFile, videoRef, videoPlaying, videoCurrentTime, videoDuration, videoMuted,
                    videoProgressPercent, formatTime,
                    openVideo, closeVideo, toggleVideoPlay, seekVideo, toggleVideoMute,
                    onVideoTimeUpdate, onVideoMetadata, onVideoPlay, onVideoPause, onVideoEnded,
                    isVideoCasting, videoCastingTo, openVideoCastPicker, stopVideoCasting,

                    // Audio player
                    queue, currentIndex, currentTrack, isPlaying, currentTime, duration,
                    progressPercent, crossfadeEnabled, showQueue, isMuted,
                    audioA, audioB,
                    playNow, addToQueueTop, addToQueueBottom,
                    togglePlay, playNext, playPrevious, seek, toggleMute,
                    onTimeUpdate, onMetadata, onEnded,
                    toggleQueue, removeFromQueue, clearQueue, moveUp, moveDown,

                    // Chromecast (backend-based)
                    showCastPanel, castDevices, castScanning, castScanError, isCasting, castingTo,
                    toggleCastPanel, scanCastDevices, connectCastDevice, stopCasting,

                    // Playlists
                    showPlaylistMenu, playlistMenuStyle, availablePlaylists,
                    openPlaylistMenu, closePlaylistMenu, addToPlaylist, createNewPlaylist,
                    viewingPlaylist, playlistSongs, openPlaylist, closePlaylistViewer,
                    playAllFromPlaylist, shuffleAndPlayPlaylist, playSongFromPlaylist,
                    movePlaylistSongUp, movePlaylistSongDown, removeFromPlaylist, deletePlaylist,

                    // Albums
                    showAlbumMenu, albumMenuStyle, availableAlbums,
                    openAlbumMenu, closeAlbumMenu, addToAlbum, createNewAlbum,

                    // Metadata refresh
                    metadataStatus, metadataProgressPercent, refreshMetadata, refreshMetadata2,
                    isCurrentPathScanning, isCurrentPathQueued, refreshButtonText,
                    isPane2PathScanning, isPane2PathQueued, refreshButtonText2,
                    showMetadataQueuePanel, toggleMetadataQueuePanel,
                    removeFromMetadataQueue, prioritizeInQueue, cancelMetadataScan
                };
            }
        }).mount('#app');
    </script>
</body>
</html>`

// browsePageHandler serves the file browser HTML page.
func browsePageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(browsePageHTML))
}

// albumsPageHTML is the HTML for the albums page.
