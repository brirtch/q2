package main

import (
	"net/http"
)

const albumsPageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Albums - Q2</title>
    <script src="https://unpkg.com/vue@3/dist/vue.global.prod.js"></script>
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: "Cascadia Code", "Fira Code", "JetBrains Mono", monospace; background: #0d1117; color: #c9d1d9; min-height: 100vh; }

        .header { background: #161b22; border-bottom: 1px solid #30363d; padding: 15px 20px; display: flex; align-items: center; gap: 15px; }
        .header a { color: #58a6ff; text-decoration: none; font-size: 14px; }
        .header a:hover { text-decoration: underline; }
        .header h1 { color: #c9d1d9; font-size: 18px; flex: 1; }
        .header .actions { display: flex; gap: 10px; }
        .btn { background: #238636; border: none; color: white; padding: 8px 16px; border-radius: 6px; cursor: pointer; font-size: 13px; font-family: inherit; }
        .btn:hover { background: #2ea043; }
        .btn.secondary { background: #30363d; }
        .btn.secondary:hover { background: #484f58; }
        .btn.danger { background: #da3633; }
        .btn.danger:hover { background: #f85149; }

        .content { padding: 20px; max-width: 1400px; margin: 0 auto; }

        .albums-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 20px; }
        .album-card { background: #161b22; border: 1px solid #30363d; border-radius: 8px; overflow: hidden; cursor: pointer; transition: border-color 0.2s, transform 0.2s; }
        .album-card:hover { border-color: #58a6ff; transform: translateY(-2px); }
        .album-cover { width: 100%; aspect-ratio: 1; background: #21262d; display: flex; align-items: center; justify-content: center; }
        .album-cover img { width: 100%; height: 100%; object-fit: cover; }
        .album-cover .placeholder { font-size: 48px; opacity: 0.5; }
        .album-info { padding: 12px; }
        .album-info h3 { font-size: 14px; color: #c9d1d9; margin-bottom: 4px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
        .album-info .count { font-size: 12px; color: #8b949e; }

        .empty-state { text-align: center; padding: 60px 20px; color: #8b949e; }
        .empty-state h2 { margin-bottom: 10px; font-size: 20px; }
        .empty-state p { margin-bottom: 20px; }

        /* Album viewer overlay */
        .album-viewer { position: fixed; top: 0; left: 0; right: 0; bottom: 0; background: #0d1117; z-index: 100; display: flex; flex-direction: column; }
        .album-viewer.hidden { display: none; }
        .album-viewer-header { background: #161b22; border-bottom: 1px solid #30363d; padding: 15px 20px; display: flex; align-items: center; gap: 15px; }
        .album-viewer-header h2 { flex: 1; font-size: 18px; color: #c9d1d9; }
        .album-viewer-content { flex: 1; overflow-y: auto; padding: 20px; }
        .album-items-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: 15px; }
        .album-item { position: relative; aspect-ratio: 1; background: #21262d; border-radius: 6px; overflow: hidden; cursor: pointer; }
        .album-item img { width: 100%; height: 100%; object-fit: cover; }
        .album-item:hover .item-overlay { opacity: 1; }
        .item-overlay { position: absolute; top: 0; left: 0; right: 0; bottom: 0; background: rgba(0,0,0,0.5); display: flex; align-items: center; justify-content: center; gap: 10px; opacity: 0; transition: opacity 0.2s; }
        .item-overlay button { background: rgba(255,255,255,0.2); border: none; color: white; width: 36px; height: 36px; border-radius: 50%; cursor: pointer; font-size: 16px; }
        .item-overlay button:hover { background: rgba(255,255,255,0.3); }
        .item-overlay button.danger:hover { background: #da3633; }

        .album-empty { text-align: center; padding: 60px; color: #8b949e; }

        /* Image viewer */
        .image-viewer { position: fixed; top: 0; left: 0; right: 0; bottom: 0; background: rgba(0,0,0,0.95); z-index: 200; display: flex; align-items: center; justify-content: center; }
        .image-viewer.hidden { display: none; }
        .image-viewer img { max-width: 95%; max-height: 95%; object-fit: contain; }
        .image-viewer .close-btn { position: absolute; top: 20px; right: 20px; background: rgba(255,255,255,0.2); border: none; color: white; width: 40px; height: 40px; border-radius: 50%; cursor: pointer; font-size: 20px; }
        .image-viewer .close-btn:hover { background: rgba(255,255,255,0.3); }
        .image-viewer .nav-btn { position: absolute; top: 50%; transform: translateY(-50%); background: rgba(255,255,255,0.2); border: none; color: white; width: 50px; height: 50px; border-radius: 50%; cursor: pointer; font-size: 24px; }
        .image-viewer .nav-btn:hover { background: rgba(255,255,255,0.3); }
        .image-viewer .nav-btn.prev { left: 20px; }
        .image-viewer .nav-btn.next { right: 20px; }
        .image-viewer .nav-btn:disabled { opacity: 0.3; cursor: not-allowed; }
    </style>
</head>
<body>
    <div id="app">
        <div class="header">
            <a href="/">&larr; Home</a>
            <h1>Albums</h1>
            <div class="actions">
                <button class="btn" @click="createAlbum">+ New Album</button>
            </div>
        </div>

        <div class="content">
            <div v-if="loading" style="text-align: center; padding: 40px; color: #8b949e;">Loading...</div>

            <div v-else-if="albums.length === 0" class="empty-state">
                <h2>No albums yet</h2>
                <p>Create your first album to start organizing your photos.</p>
                <button class="btn" @click="createAlbum">+ Create Album</button>
            </div>

            <div v-else class="albums-grid">
                <div v-for="album in albums" :key="album.id" class="album-card" @click="openAlbum(album)">
                    <div class="album-cover">
                        <img v-if="album.cover_path" :src="'/api/thumbnail?path=' + encodeURIComponent(album.cover_path) + '&size=small'" @error="$event.target.style.display='none'">
                        <span v-else class="placeholder">🖼️</span>
                    </div>
                    <div class="album-info">
                        <h3>{{ album.name }}</h3>
                        <span class="count">{{ album.item_count }} {{ album.item_count === 1 ? 'photo' : 'photos' }}</span>
                    </div>
                </div>
            </div>
        </div>

        <!-- Album viewer overlay -->
        <div class="album-viewer" :class="{ hidden: !viewingAlbum }">
            <div class="album-viewer-header">
                <button class="btn secondary" @click="closeAlbum">&larr; Back</button>
                <h2>{{ viewingAlbum?.name }}</h2>
                <span style="color: #8b949e; font-size: 13px;">{{ albumItems.length }} photos</span>
                <div style="flex: 1;"></div>
                <button class="btn danger" @click="deleteAlbum">Delete Album</button>
            </div>
            <div class="album-viewer-content">
                <div v-if="albumItems.length === 0" class="album-empty">
                    <p>This album is empty.</p>
                    <p style="font-size: 13px; margin-top: 10px;">Add photos from the Browse page using the album button on image files.</p>
                </div>
                <div v-else class="album-items-grid">
                    <div v-for="(item, index) in albumItems" :key="item.id" class="album-item" @click="viewImage(index)">
                        <img :src="item.thumbnail_small || '/api/thumbnail?path=' + encodeURIComponent(item.path) + '&size=small'" @error="handleThumbError">
                        <div class="item-overlay">
                            <button @click.stop="removeItem(item)" class="danger" title="Remove from album">🗑️</button>
                        </div>
                    </div>
                </div>
            </div>
        </div>

        <!-- Image viewer -->
        <div class="image-viewer" :class="{ hidden: viewingImageIndex === null }" @click="closeImage">
            <button class="close-btn" @click.stop="closeImage">&times;</button>
            <button class="nav-btn prev" @click.stop="prevImage" :disabled="viewingImageIndex === 0">&larr;</button>
            <img v-if="viewingImageIndex !== null && albumItems[viewingImageIndex]"
                 :src="albumItems[viewingImageIndex].thumbnail_large || '/api/file?path=' + encodeURIComponent(albumItems[viewingImageIndex].path)"
                 @click.stop>
            <button class="nav-btn next" @click.stop="nextImage" :disabled="viewingImageIndex === albumItems.length - 1">&rarr;</button>
        </div>
    </div>

    <script>
        const { createApp, ref, onMounted } = Vue;

        createApp({
            setup() {
                const loading = ref(true);
                const albums = ref([]);
                const viewingAlbum = ref(null);
                const albumItems = ref([]);
                const viewingImageIndex = ref(null);

                const loadAlbums = async () => {
                    try {
                        const resp = await fetch('/api/albums');
                        const data = await resp.json();
                        albums.value = data.albums || [];
                    } catch (e) {
                        console.error('Failed to load albums:', e);
                    }
                    loading.value = false;
                };

                const createAlbum = async () => {
                    const name = prompt('Album name:');
                    if (!name) return;

                    try {
                        await fetch('/api/album', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ name })
                        });
                        loadAlbums();
                    } catch (e) {
                        console.error('Failed to create album:', e);
                    }
                };

                const openAlbum = async (album) => {
                    viewingAlbum.value = album;
                    try {
                        const resp = await fetch('/api/album?id=' + album.id);
                        const data = await resp.json();
                        albumItems.value = data.items || [];
                    } catch (e) {
                        console.error('Failed to load album:', e);
                    }
                };

                const closeAlbum = () => {
                    viewingAlbum.value = null;
                    albumItems.value = [];
                    loadAlbums();
                };

                const deleteAlbum = async () => {
                    if (!viewingAlbum.value) return;
                    if (!confirm('Delete album "' + viewingAlbum.value.name + '"? Photos will not be deleted.')) return;

                    try {
                        await fetch('/api/album?id=' + viewingAlbum.value.id, { method: 'DELETE' });
                        closeAlbum();
                    } catch (e) {
                        console.error('Failed to delete album:', e);
                    }
                };

                const removeItem = async (item) => {
                    if (!viewingAlbum.value) return;

                    try {
                        await fetch('/api/album/remove', {
                            method: 'POST',
                            headers: { 'Content-Type': 'application/json' },
                            body: JSON.stringify({ album_id: viewingAlbum.value.id, item_id: item.id })
                        });
                        openAlbum(viewingAlbum.value);
                    } catch (e) {
                        console.error('Failed to remove item:', e);
                    }
                };

                const viewImage = (index) => {
                    viewingImageIndex.value = index;
                };

                const closeImage = () => {
                    viewingImageIndex.value = null;
                };

                const prevImage = () => {
                    if (viewingImageIndex.value > 0) {
                        viewingImageIndex.value--;
                    }
                };

                const nextImage = () => {
                    if (viewingImageIndex.value < albumItems.value.length - 1) {
                        viewingImageIndex.value++;
                    }
                };

                const handleThumbError = (e) => {
                    e.target.src = 'data:image/svg+xml,' + encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100" viewBox="0 0 100 100"><rect fill="%2321262d" width="100" height="100"/><text x="50" y="55" text-anchor="middle" fill="%236e7681" font-size="12">No Preview</text></svg>');
                };

                // Keyboard navigation
                const handleKeydown = (e) => {
                    if (viewingImageIndex.value !== null) {
                        if (e.key === 'Escape') closeImage();
                        if (e.key === 'ArrowLeft') prevImage();
                        if (e.key === 'ArrowRight') nextImage();
                    } else if (viewingAlbum.value) {
                        if (e.key === 'Escape') closeAlbum();
                    }
                };

                onMounted(() => {
                    loadAlbums();
                    document.addEventListener('keydown', handleKeydown);
                });

                return {
                    loading, albums, viewingAlbum, albumItems, viewingImageIndex,
                    loadAlbums, createAlbum, openAlbum, closeAlbum, deleteAlbum,
                    removeItem, viewImage, closeImage, prevImage, nextImage,
                    handleThumbError
                };
            }
        }).mount('#app');
    </script>
</body>
</html>`

// albumsPageHandler serves the albums page.
func albumsPageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(albumsPageHTML))
}

// makeMusicArtistsHandler returns all distinct artists from audio_metadata.
