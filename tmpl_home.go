package main

import (
	"fmt"
	"net/http"
)


// homePageHTML is the HTML for the home page.
const homePageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Q2</title>
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: "Cascadia Code", "Fira Code", "JetBrains Mono", "SF Mono", Consolas, monospace; padding: 40px; background: #0d1117; color: #c9d1d9; }
        .container { max-width: 600px; margin: 0 auto; }
        .title-row { display: flex; align-items: center; justify-content: space-between; }
        h1 { color: #58a6ff; font-size: 48px; }
        .settings-btn { display: flex; align-items: center; justify-content: center; width: 40px; height: 40px; border-radius: 6px; background: #161b22; border: 1px solid #30363d; color: #8b949e; text-decoration: none; transition: all 0.2s; }
        .settings-btn:hover { background: #1f2428; border-color: #58a6ff; color: #58a6ff; }
        .settings-btn svg { width: 20px; height: 20px; fill: currentColor; }
        .subtitle { color: #8b949e; font-size: 16px; margin-bottom: 30px; }
        .nav-cards { display: flex; flex-direction: column; gap: 15px; }
        .nav-card { display: flex; align-items: center; gap: 20px; padding: 25px; background: #161b22; border: 1px solid #30363d; border-radius: 6px; text-decoration: none; color: inherit; transition: border-color 0.2s, background 0.2s; }
        .nav-card:hover { background: #1f2428; border-color: #58a6ff; }
        .nav-card .icon { font-size: 32px; }
        .nav-card .info h2 { margin: 0 0 5px 0; color: #58a6ff; font-size: 18px; }
        .nav-card .info p { margin: 0; color: #8b949e; font-size: 13px; }

        /* Inbox */
        .inbox-section { margin-top: 30px; }
        .inbox-section h2 { color: #58a6ff; font-size: 16px; margin-bottom: 12px; }
        .inbox-dropzone {
            border: 2px dashed #30363d; border-radius: 6px; padding: 40px 20px;
            text-align: center; color: #484f58; font-size: 14px;
            transition: all 0.2s; cursor: pointer; background: #161b22;
        }
        .inbox-dropzone.dragover { border-color: #58a6ff; background: #58a6ff11; color: #58a6ff; }
        .inbox-dropzone input[type="file"] { display: none; }
        .inbox-files { margin-top: 12px; }
        .inbox-file { display: flex; align-items: center; gap: 10px; padding: 8px 12px; background: #161b22; border: 1px solid #21262d; border-radius: 4px; margin-bottom: 6px; font-size: 13px; }
        .inbox-file .fname { flex: 1; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
        .inbox-file .fstatus { font-size: 11px; padding: 2px 8px; border-radius: 3px; white-space: nowrap; }
        .inbox-file .fstatus.pending { color: #8b949e; background: #21262d; }
        .inbox-file .fstatus.processing { color: #d29922; background: #d2992222; }
        .inbox-file .fstatus.done { color: #3fb950; background: #3fb95022; }
        .inbox-file .fstatus.error { color: #f85149; background: #f8514922; }
        .inbox-file .fdest { color: #484f58; font-size: 11px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 200px; }
        .inbox-file .ferror { color: #f85149; font-size: 11px; }
        .inbox-clear { margin-top: 8px; }
        .inbox-progress { margin-top: 8px; font-size: 12px; color: #8b949e; }
    </style>
</head>
<body>
    <div class="container" id="app">
        <div class="title-row">
            <h1>&gt; Q2_</h1>
            <a href="/settings" class="settings-btn" title="Settings">
                <svg viewBox="0 0 24 24"><path d="M12 15.5A3.5 3.5 0 0 1 8.5 12 3.5 3.5 0 0 1 12 8.5a3.5 3.5 0 0 1 3.5 3.5 3.5 3.5 0 0 1-3.5 3.5m7.43-2.53c.04-.32.07-.64.07-.97s-.03-.66-.07-1l2.11-1.63c.19-.15.24-.42.12-.64l-2-3.46c-.12-.22-.39-.3-.61-.22l-2.49 1c-.52-.4-1.08-.73-1.69-.98l-.38-2.65C14.46 2.18 14.25 2 14 2h-4c-.25 0-.46.18-.49.42l-.38 2.65c-.61.25-1.17.59-1.69.98l-2.49-1c-.23-.09-.49 0-.61.22l-2 3.46c-.13.22-.07.49.12.64L4.57 11c-.04.34-.07.67-.07 1s.03.65.07.97l-2.11 1.66c-.19.15-.25.42-.12.64l2 3.46c.12.22.39.3.61.22l2.49-1.01c.52.4 1.08.73 1.69.98l.38 2.65c.03.24.24.42.49.42h4c.25 0 .46-.18.49-.42l.38-2.65c.61-.25 1.17-.58 1.69-.98l2.49 1.01c.22.08.49 0 .61-.22l2-3.46c.12-.22.07-.49-.12-.64L19.43 12.97Z"/></svg>
            </a>
        </div>
        <p class="subtitle">// media folder manager</p>
        <div class="nav-cards">
            <a href="/browse" class="nav-card">
                <span class="icon">📁</span>
                <div class="info">
                    <h2>Browse</h2>
                    <p>Navigate through monitored folders and view files</p>
                </div>
            </a>
            <a href="/music#songs" class="nav-card">
                <span class="icon">🎵</span>
                <div class="info">
                    <h2>Music</h2>
                    <p>Browse your music library by artist, album, or genre</p>
                </div>
            </a>
            <a href="/albums" class="nav-card">
                <span class="icon">🖼️</span>
                <div class="info">
                    <h2>Photo Albums</h2>
                    <p>View and manage photo albums</p>
                </div>
            </a>
            <a href="/schema" class="nav-card">
                <span class="icon">📊</span>
                <div class="info">
                    <h2>Schema</h2>
                    <p>View database tables, columns, and indexes</p>
                </div>
            </a>
        </div>

        <div class="inbox-section">
            <h2>Inbox</h2>
            <div class="inbox-dropzone" :class="{ dragover: isDragover }"
                 @dragover.prevent="isDragover = true"
                 @dragleave.prevent="isDragover = false"
                 @drop.prevent="handleDrop"
                 @click="$refs.fileInput.click()">
                Drop audio files here to auto-organise, or click to browse
                <input type="file" ref="fileInput" multiple accept="audio/*" @change="handleFileSelect" />
            </div>
            <div class="inbox-progress" v-if="inboxFiles.length">
                {{ doneCount }}/{{ inboxFiles.length }} processed
                <span v-if="hasErrors"> &middot; {{ errorCount }} failed</span>
            </div>
            <div class="inbox-files">
                <div class="inbox-file" v-for="(f, i) in inboxFiles" :key="i">
                    <span class="fname">{{ f.name }}</span>
                    <span class="fdest" v-if="f.dest" :title="f.dest">{{ f.dest }}</span>
                    <span class="ferror" v-if="f.error" :title="f.error">{{ f.error }}</span>
                    <span class="fstatus" :class="f.status">{{ f.status }}</span>
                </div>
            </div>
            <button class="btn inbox-clear" v-if="allDone && inboxFiles.length" @click="clearInbox"
                    style="padding:4px 12px; border:1px solid #30363d; border-radius:4px; background:#21262d; color:#8b949e; cursor:pointer; font-family:inherit; font-size:12px;">
                Clear
            </button>
        </div>
    </div>

    <script type="module">
    import { createApp, ref, computed } from 'https://unpkg.com/vue@3/dist/vue.esm-browser.prod.js';

    createApp({
        setup() {
            const isDragover = ref(false);
            const inboxFiles = ref([]);
            let pollTimer = null;

            const doneCount = computed(() => inboxFiles.value.filter(f => f.status === 'done').length);
            const errorCount = computed(() => inboxFiles.value.filter(f => f.status === 'error').length);
            const hasErrors = computed(() => errorCount.value > 0);
            const allDone = computed(() => inboxFiles.value.length > 0 && inboxFiles.value.every(f => f.status === 'done' || f.status === 'error'));

            const uploadFiles = async (fileList) => {
                if (!fileList || fileList.length === 0) return;

                const formData = new FormData();
                for (const file of fileList) {
                    formData.append('files', file);
                }

                // Add placeholder entries immediately
                const startIdx = inboxFiles.value.length;
                for (const file of fileList) {
                    inboxFiles.value.push({ name: file.name, status: 'pending', dest: '', error: '' });
                }

                try {
                    const res = await fetch('/api/inbox/upload', { method: 'POST', body: formData });
                    const data = await res.json();
                    if (!res.ok) {
                        for (let i = startIdx; i < inboxFiles.value.length; i++) {
                            inboxFiles.value[i].status = 'error';
                            inboxFiles.value[i].error = data.error || 'Upload failed';
                        }
                        return;
                    }
                    // Start polling for status
                    startPolling();
                } catch (e) {
                    for (let i = startIdx; i < inboxFiles.value.length; i++) {
                        inboxFiles.value[i].status = 'error';
                        inboxFiles.value[i].error = e.message;
                    }
                }
            };

            const startPolling = () => {
                if (pollTimer) return;
                pollTimer = setInterval(async () => {
                    try {
                        const res = await fetch('/api/inbox/status');
                        const data = await res.json();
                        if (data.files) {
                            inboxFiles.value = data.files;
                        }
                        // Stop polling when all done
                        if (data.files && data.files.every(f => f.status === 'done' || f.status === 'error')) {
                            clearInterval(pollTimer);
                            pollTimer = null;
                        }
                    } catch (e) {
                        clearInterval(pollTimer);
                        pollTimer = null;
                    }
                }, 500);
            };

            const handleDrop = (e) => {
                isDragover.value = false;
                uploadFiles(e.dataTransfer.files);
            };

            const handleFileSelect = (e) => {
                uploadFiles(e.target.files);
                e.target.value = '';
            };

            const clearInbox = async () => {
                await fetch('/api/inbox/clear', { method: 'POST' });
                inboxFiles.value = [];
            };

            return { isDragover, inboxFiles, doneCount, errorCount, hasErrors, allDone, handleDrop, handleFileSelect, clearInbox };
        }
    }).mount('#app');
    </script>
</body>
</html>`

// homeEndpoint serves the home page with navigation links.
func homeEndpoint(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, homePageHTML)
}


