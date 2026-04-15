package main

import (
	"net/http"
)

func settingsPageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(settingsPageHTML))
}

const settingsPageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Q2 - Settings</title>
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: "Cascadia Code", "Fira Code", "JetBrains Mono", "SF Mono", Consolas, monospace; background: #0d1117; color: #c9d1d9; padding: 30px; }
        .container { max-width: 700px; margin: 0 auto; }
        .header { display: flex; align-items: center; gap: 15px; margin-bottom: 30px; }
        .header a { color: #58a6ff; text-decoration: none; font-size: 14px; }
        .header a:hover { text-decoration: underline; }
        h1 { color: #58a6ff; font-size: 28px; }
        .section { background: #161b22; border: 1px solid #30363d; border-radius: 6px; padding: 20px; margin-bottom: 20px; }
        .section h2 { color: #58a6ff; font-size: 16px; margin-bottom: 15px; border-bottom: 1px solid #30363d; padding-bottom: 8px; }
        .folder-list { list-style: none; }
        .folder-list li { display: flex; align-items: center; justify-content: space-between; padding: 8px 12px; border-bottom: 1px solid #21262d; font-size: 13px; }
        .folder-list li:last-child { border-bottom: none; }
        .folder-path { color: #c9d1d9; word-break: break-all; }
        .btn { padding: 6px 14px; border: 1px solid #30363d; border-radius: 4px; background: #21262d; color: #c9d1d9; cursor: pointer; font-family: inherit; font-size: 12px; transition: background 0.15s; }
        .btn:hover { background: #30363d; }
        .btn-danger { border-color: #f85149; color: #f85149; }
        .btn-danger:hover { background: #f8514922; }
        .btn-primary { border-color: #58a6ff; color: #58a6ff; }
        .btn-primary:hover { background: #58a6ff22; }
        .add-folder { display: flex; gap: 8px; margin-top: 12px; }
        .add-folder input { flex: 1; padding: 8px 12px; background: #0d1117; border: 1px solid #30363d; border-radius: 4px; color: #c9d1d9; font-family: inherit; font-size: 13px; }
        .add-folder input::placeholder { color: #484f58; }
        .setting-row { display: flex; align-items: center; gap: 10px; margin-bottom: 10px; }
        .setting-row label { font-size: 13px; color: #8b949e; min-width: 140px; }
        .setting-row input { flex: 1; padding: 8px 12px; background: #0d1117; border: 1px solid #30363d; border-radius: 4px; color: #c9d1d9; font-family: inherit; font-size: 13px; }
        .setting-row input::placeholder { color: #484f58; }
        .empty { color: #484f58; font-size: 13px; padding: 10px 0; font-style: italic; }
        .status-msg { font-size: 12px; margin-top: 8px; }
        .status-msg.ok { color: #3fb950; }
        .status-msg.err { color: #f85149; }
    </style>
</head>
<body>
    <div class="container" id="app">
        <div class="header">
            <a href="/">&larr; Home</a>
            <h1>Settings</h1>
        </div>

        <div class="section">
            <h2>Monitored Folders</h2>
            <ul class="folder-list" v-if="folders.length">
                <li v-for="f in folders" :key="f.path">
                    <span class="folder-path">{{ f.path }}</span>
                    <button class="btn btn-danger" @click="removeFolder(f.path)">Remove</button>
                </li>
            </ul>
            <p class="empty" v-else>No folders configured</p>
            <div class="add-folder">
                <input v-model="newFolder" placeholder="Enter folder path (e.g. P:\Music)" @keyup.enter="addFolder" />
                <button class="btn btn-primary" @click="addFolder">Add</button>
            </div>
            <p class="status-msg" :class="folderMsg.type" v-if="folderMsg.text">{{ folderMsg.text }}</p>
        </div>

        <div class="section">
            <h2>Inbox Settings</h2>
            <div class="setting-row">
                <label>Audio destination:</label>
                <input v-model="audioDestination" placeholder="e.g. P:\Music" @change="saveSettings" />
            </div>
            <p style="font-size: 12px; color: #484f58; margin-top: 4px;">New audio files dropped in the Inbox will be copied to &lt;destination&gt;/&lt;Artist&gt;/&lt;Album&gt;/</p>
            <p class="status-msg" :class="settingsMsg.type" v-if="settingsMsg.text">{{ settingsMsg.text }}</p>
        </div>
    </div>

    <script type="module">
    import { createApp, ref, onMounted } from 'https://unpkg.com/vue@3/dist/vue.esm-browser.prod.js';

    createApp({
        setup() {
            const folders = ref([]);
            const newFolder = ref('');
            const folderMsg = ref({ text: '', type: '' });
            const audioDestination = ref('');
            const settingsMsg = ref({ text: '', type: '' });

            const loadFolders = async () => {
                const res = await fetch('/api/roots');
                const data = await res.json();
                folders.value = data.roots || [];
            };

            const loadSettings = async () => {
                const res = await fetch('/api/settings');
                const data = await res.json();
                audioDestination.value = data.audio_destination || '';
            };

            const addFolder = async () => {
                if (!newFolder.value.trim()) return;
                folderMsg.value = { text: '', type: '' };
                try {
                    const res = await fetch('/api/folders/add', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ path: newFolder.value.trim() })
                    });
                    const data = await res.json();
                    if (!res.ok) {
                        folderMsg.value = { text: data.error, type: 'err' };
                    } else {
                        folderMsg.value = { text: 'Folder ' + data.status, type: 'ok' };
                        newFolder.value = '';
                        await loadFolders();
                    }
                } catch (e) {
                    folderMsg.value = { text: e.message, type: 'err' };
                }
            };

            const removeFolder = async (path) => {
                try {
                    const res = await fetch('/api/folders/remove', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ path })
                    });
                    if (res.ok) {
                        await loadFolders();
                    }
                } catch (e) {
                    folderMsg.value = { text: e.message, type: 'err' };
                }
            };

            const saveSettings = async () => {
                settingsMsg.value = { text: '', type: '' };
                try {
                    const res = await fetch('/api/settings', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ audio_destination: audioDestination.value })
                    });
                    if (res.ok) {
                        settingsMsg.value = { text: 'Saved', type: 'ok' };
                        setTimeout(() => { settingsMsg.value = { text: '', type: '' }; }, 2000);
                    }
                } catch (e) {
                    settingsMsg.value = { text: e.message, type: 'err' };
                }
            };

            onMounted(() => { loadFolders(); loadSettings(); });

            return { folders, newFolder, folderMsg, audioDestination, settingsMsg, addFolder, removeFolder, saveSettings };
        }
    }).mount('#app');
    </script>
</body>
</html>`


// main parses subcommands and dispatches to the appropriate handler.
// Supported commands: addfolder, removefolder, listfolders, serve
