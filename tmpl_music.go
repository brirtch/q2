package main

import (
	"net/http"
)

func musicPageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(musicPageHTML))
}

// musicPageHTML is the HTML for the music library page.
const musicPageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Music - Q2</title>
    <script src="https://unpkg.com/vue@3/dist/vue.global.prod.js"></script>
    <style>
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: "Cascadia Code", "Fira Code", "JetBrains Mono", monospace; background: #0d1117; color: #c9d1d9; min-height: 100vh; padding-bottom: 90px; }

        .header { background: #161b22; border-bottom: 1px solid #30363d; padding: 15px 20px; display: flex; align-items: center; gap: 15px; }
        .header a { color: #58a6ff; text-decoration: none; font-size: 14px; }
        .header a:hover { text-decoration: underline; }
        .header h1 { color: #c9d1d9; font-size: 18px; flex: 1; }

        .tabs { display: flex; background: #161b22; border-bottom: 1px solid #30363d; padding: 0 20px; }
        .tab { padding: 12px 20px; cursor: pointer; color: #8b949e; border-bottom: 2px solid transparent; font-family: inherit; font-size: 14px; background: none; border-top: none; border-left: none; border-right: none; }
        .tab:hover { color: #c9d1d9; }
        .tab.active { color: #58a6ff; border-bottom-color: #58a6ff; }

        .content { padding: 20px; max-width: 1200px; margin: 0 auto; }

        /* List styles */
        .list-item { display: flex; align-items: center; padding: 12px 16px; border-bottom: 1px solid #21262d; cursor: pointer; gap: 15px; }
        .list-item:hover { background: #161b22; }
        .list-item.active { background: #1f6feb22; }
        .list-item .icon { font-size: 20px; color: #8b949e; width: 24px; text-align: center; }
        .list-item .info { flex: 1; min-width: 0; }
        .list-item .info .name { color: #c9d1d9; font-size: 14px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
        .list-item .info .detail { color: #8b949e; font-size: 12px; margin-top: 2px; }
        .list-item .count { color: #8b949e; font-size: 12px; }
        .list-item .duration { color: #8b949e; font-size: 12px; min-width: 50px; text-align: right; }
        .list-item .actions { display: flex; gap: 4px; }
        .list-item .action-btn { background: none; border: 1px solid #30363d; border-radius: 4px; padding: 4px 8px; cursor: pointer; font-size: 11px; color: #8b949e; }
        .list-item .action-btn:hover { background: #238636; color: white; border-color: #238636; }

        /* Song table */
        .song-table { width: 100%; border-collapse: collapse; }
        .song-table th { text-align: left; padding: 10px 16px; color: #8b949e; font-size: 12px; border-bottom: 1px solid #30363d; font-weight: 600; }
        .song-table td { padding: 10px 16px; border-bottom: 1px solid #21262d; font-size: 13px; }
        .song-table tr { cursor: pointer; }
        .song-table tr:hover { background: #161b22; }
        .song-table tr.playing { background: #1f6feb22; }
        .song-table .track-num { color: #8b949e; width: 40px; }
        .song-table .title-col { color: #c9d1d9; }
        .song-table .title-col .song-title { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 400px; display: block; }
        .song-table .artist-col { color: #8b949e; }
        .song-table .album-col { color: #8b949e; }
        .song-table .duration-col { color: #8b949e; width: 60px; text-align: right; }
        .song-table .actions-col { width: 160px; }
        .song-table .action-btns { display: flex; gap: 3px; justify-content: flex-end; }
        .song-table .action-btn { background: none; border: 1px solid #30363d; border-radius: 4px; padding: 3px 6px; cursor: pointer; font-size: 11px; color: #8b949e; white-space: nowrap; }
        .song-table .action-btn:hover { background: #238636; color: white; border-color: #238636; }
        .song-table .action-btn.play-btn:hover { background: #1f6feb; border-color: #1f6feb; }

        /* Sub-header for filtered views */
        .sub-header { display: flex; align-items: center; gap: 15px; padding: 15px 0; margin-bottom: 10px; border-bottom: 1px solid #30363d; }
        .sub-header .back-btn { background: none; border: 1px solid #30363d; color: #58a6ff; padding: 6px 12px; border-radius: 6px; cursor: pointer; font-size: 13px; font-family: inherit; }
        .sub-header .back-btn:hover { background: #30363d; }
        .sub-header h2 { color: #c9d1d9; font-size: 18px; flex: 1; }
        .sub-header .play-all { background: #238636; border: none; color: white; padding: 8px 16px; border-radius: 6px; cursor: pointer; font-size: 13px; font-family: inherit; }
        .sub-header .play-all:hover { background: #2ea043; }

        .empty-state { text-align: center; padding: 60px 20px; color: #8b949e; }
        .empty-state h2 { margin-bottom: 10px; font-size: 20px; }

        /* Audio Player */
        .audio-player { position: fixed; bottom: 0; left: 0; right: 0; background: #1a1a2e; color: white; padding: 12px 20px; display: flex; align-items: center; gap: 15px; z-index: 1000; box-shadow: 0 -2px 10px rgba(0,0,0,0.3); }
        .audio-player.hidden { display: none; }
        .player-controls { display: flex; align-items: center; gap: 8px; }
        .player-btn { background: none; border: none; color: white; font-size: 20px; cursor: pointer; padding: 8px; border-radius: 50%; transition: background 0.2s; }
        .player-btn:hover { background: rgba(255,255,255,0.1); }
        .player-btn.play-pause { font-size: 22px; background: #0066cc; width: 44px; height: 44px; padding: 0; display: flex; align-items: center; justify-content: center; }
        .player-btn.play-pause:hover { background: #0052a3; }
        .track-info { min-width: 0; max-width: 250px; }
        .track-name { font-weight: 500; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-size: 14px; }
        .track-artist { font-size: 12px; color: #aaa; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
        .progress-container { flex: 2; display: flex; align-items: center; gap: 10px; }
        .progress-bar { flex: 1; height: 6px; background: #333; border-radius: 3px; cursor: pointer; position: relative; }
        .progress-fill { height: 100%; background: #0066cc; border-radius: 3px; transition: width 0.1s; }
        .time-display { font-size: 12px; color: #aaa; min-width: 90px; text-align: center; }
        .volume-control { display: flex; align-items: center; gap: 5px; }
        .volume-slider { width: 80px; height: 4px; -webkit-appearance: none; background: #333; border-radius: 2px; outline: none; }
        .volume-slider::-webkit-slider-thumb { -webkit-appearance: none; width: 12px; height: 12px; border-radius: 50%; background: #0066cc; cursor: pointer; }
        .player-right { display: flex; align-items: center; gap: 10px; }

        /* Cast Button and Panel */
        .cast-btn { position: relative; }
        .cast-btn.casting { color: #58a6ff; }
        .cast-panel { position: fixed; bottom: 70px; right: 20px; width: 280px; background: #161b22; border: 1px solid #30363d; border-radius: 6px; box-shadow: 0 4px 20px rgba(0,0,0,0.4); z-index: 999; overflow: hidden; }
        .cast-panel.hidden { display: none; }
        .cast-header { padding: 15px; background: #0d1117; border-bottom: 1px solid #30363d; display: flex; justify-content: space-between; align-items: center; }
        .cast-header h3 { margin: 0; font-size: 14px; color: #c9d1d9; }
        .cast-refresh { background: none; border: 1px solid #30363d; color: #58a6ff; padding: 4px 10px; border-radius: 4px; cursor: pointer; font-size: 12px; font-family: inherit; }
        .cast-refresh:hover { background: #30363d; }
        .cast-scanning { font-size: 12px; color: #8b949e; }
        .cast-list { max-height: 300px; overflow-y: auto; }
        .cast-device { display: flex; align-items: center; padding: 12px 15px; border-bottom: 1px solid #21262d; gap: 10px; color: #c9d1d9; cursor: pointer; }
        .cast-device:hover { background: #1f2428; }
        .cast-device.active { background: #1f6feb22; color: #58a6ff; }
        .cast-device .icon { font-size: 18px; }
        .cast-device .name { flex: 1; font-size: 13px; }
        .cast-device .status { font-size: 11px; color: #6e7681; }
        .cast-unavailable { padding: 20px; text-align: center; color: #6e7681; font-size: 13px; }

        /* Queue panel */
        .queue-panel { position: fixed; bottom: 70px; right: 310px; width: 320px; background: #161b22; border: 1px solid #30363d; border-radius: 6px; box-shadow: 0 4px 20px rgba(0,0,0,0.4); z-index: 999; overflow: hidden; }
        .queue-panel.hidden { display: none; }
        .queue-header { padding: 12px 15px; background: #0d1117; border-bottom: 1px solid #30363d; display: flex; justify-content: space-between; align-items: center; }
        .queue-header h3 { margin: 0; font-size: 14px; color: #c9d1d9; }
        .queue-header .queue-count { font-size: 12px; color: #8b949e; }
        .queue-list { max-height: 340px; overflow-y: auto; }
        .queue-item { display: flex; align-items: center; padding: 8px 12px; border-bottom: 1px solid #21262d; gap: 10px; cursor: pointer; }
        .queue-item:hover { background: #1f2428; }
        .queue-item.current { background: #1f6feb22; }
        .queue-item .q-index { font-size: 11px; color: #484f58; min-width: 20px; text-align: right; }
        .queue-item .q-info { flex: 1; min-width: 0; }
        .queue-item .q-title { font-size: 13px; color: #c9d1d9; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
        .queue-item.current .q-title { color: #58a6ff; }
        .queue-item .q-artist { font-size: 11px; color: #8b949e; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
        .queue-item .q-remove { background: none; border: none; color: #484f58; cursor: pointer; font-size: 14px; padding: 2px 4px; border-radius: 3px; }
        .queue-item .q-remove:hover { color: #f85149; background: #f8514922; }
        .queue-empty { padding: 24px; text-align: center; color: #6e7681; font-size: 13px; }
        .q-clear { background: none; border: 1px solid #f8514944; color: #f85149; font-size: 11px; padding: 2px 8px; border-radius: 4px; cursor: pointer; font-family: inherit; }
        .q-clear:hover { background: #f8514922; }

        /* Now Playing overlay */
        .now-playing { position: fixed; top: 0; left: 0; right: 0; bottom: 80px; background: #0d1117; z-index: 990; display: flex; flex-direction: column; }
        .now-playing.hidden { display: none; }
        .np-header { display: flex; align-items: center; justify-content: space-between; padding: 15px 20px; background: #161b22; border-bottom: 1px solid #30363d; }
        .np-header h2 { font-size: 16px; color: #c9d1d9; margin: 0; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
        .np-header .np-artist { font-size: 13px; color: #8b949e; margin-top: 2px; }
        .np-close { background: none; border: 1px solid #30363d; color: #c9d1d9; padding: 6px 14px; border-radius: 6px; cursor: pointer; font-size: 13px; font-family: inherit; }
        .np-close:hover { background: #30363d; }
        .np-body { flex: 1; display: flex; overflow: hidden; }
        .np-visualiser { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: flex-end; padding: 20px; min-width: 0; }
        .np-visualiser canvas { width: 100%; height: 180px; display: block; }
        .np-lyrics { width: 340px; overflow-y: auto; padding: 20px 20px 20px 0; display: flex; flex-direction: column; gap: 2px; }
        .np-lyrics.no-sidebar { display: none; }
        .lyric-line { padding: 8px 12px; border-radius: 6px; font-size: 15px; color: #484f58; transition: color 0.3s, background 0.3s; cursor: pointer; line-height: 1.5; }
        .lyric-line.active { color: #c9d1d9; background: #161b22; font-size: 17px; }
        .lyric-line.past { color: #6e7681; }
        .np-plain-lyrics { padding: 20px; color: #6e7681; font-size: 14px; line-height: 1.8; overflow-y: auto; flex: 1; white-space: pre-wrap; }
        .np-no-lyrics { display: flex; align-items: center; justify-content: center; color: #484f58; font-size: 14px; height: 100%; }

        /* Playlist popup */
        .playlist-popup { position: fixed; width: 240px; background: #161b22; border: 1px solid #30363d; border-radius: 6px; box-shadow: 0 4px 20px rgba(0,0,0,0.4); z-index: 1001; overflow: hidden; }
        .playlist-popup.hidden { display: none; }
        .playlist-popup-header { padding: 10px 12px; background: #0d1117; border-bottom: 1px solid #30363d; display: flex; justify-content: space-between; align-items: center; font-size: 13px; color: #c9d1d9; }
        .playlist-popup-header button { background: none; border: none; color: #8b949e; cursor: pointer; font-size: 16px; padding: 0 4px; }
        .playlist-popup-list { max-height: 250px; overflow-y: auto; }
        .playlist-popup-item { padding: 8px 12px; cursor: pointer; font-size: 13px; color: #c9d1d9; border-bottom: 1px solid #21262d; }
        .playlist-popup-item:hover { background: #1f2428; }
        .playlist-popup-item .already-here { color: #484f58; font-size: 11px; }
        .playlist-popup-empty { padding: 12px; color: #484f58; font-size: 13px; text-align: center; }
        .playlist-popup-new { padding: 8px 12px; cursor: pointer; font-size: 13px; color: #58a6ff; border-top: 1px solid #30363d; }
        .playlist-popup-new:hover { background: #1f2428; }

        .search-box { padding: 12px 20px; background: #161b22; border-bottom: 1px solid #30363d; }
        .search-box input { width: 100%; padding: 10px 14px; background: #0d1117; border: 1px solid #30363d; border-radius: 6px; color: #c9d1d9; font-family: inherit; font-size: 14px; outline: none; }
        .search-box input:focus { border-color: #58a6ff; }
        .search-box input::placeholder { color: #6e7681; }

        /* Favourites filter */
        .fav-filter { display: flex; gap: 8px; padding: 10px 20px; background: #161b22; border-bottom: 1px solid #21262d; }
        .fav-filter-btn { background: none; border: 1px solid #30363d; color: #8b949e; padding: 4px 14px; border-radius: 20px; cursor: pointer; font-family: inherit; font-size: 13px; }
        .fav-filter-btn:hover { border-color: #58a6ff; color: #c9d1d9; }
        .fav-filter-btn.active { background: #1f6feb22; border-color: #58a6ff; color: #58a6ff; }

        /* Star button */
        .star-btn { background: none; border: none; cursor: pointer; font-size: 18px; padding: 0 4px; line-height: 1; color: #484f58; transition: color 0.15s, transform 0.15s; flex-shrink: 0; }
        .star-btn:hover { color: #e3b341; transform: scale(1.2); }
        .star-btn.favourited { color: #e3b341; }
    </style>
</head>
<body>
<div id="app">
    <div class="header">
        <a href="/">&larr; Home</a>
        <h1>Music Library</h1>
    </div>

    <div class="tabs">
        <button class="tab" :class="{ active: tab === 'artists' }" @click="switchTab('artists')">Artists</button>
        <button class="tab" :class="{ active: tab === 'albums' }" @click="switchTab('albums')">Albums</button>
        <button class="tab" :class="{ active: tab === 'genres' }" @click="switchTab('genres')">Genres</button>
        <button class="tab" :class="{ active: tab === 'songs' }" @click="switchTab('songs')">Songs</button>
        <button class="tab" :class="{ active: tab === 'playlists' }" @click="switchTab('playlists')">Playlists</button>
    </div>

    <div class="search-box">
        <input type="text" v-model="searchQuery" placeholder="Search music..." @input="onSearch">
    </div>

    <div class="fav-filter" v-if="tab === 'artists' || tab === 'albums' || tab === 'genres' || tab === 'songs'">
        <button class="fav-filter-btn" :class="{ active: !showFavsOnly }" @click="showFavsOnly = false">All</button>
        <button class="fav-filter-btn" :class="{ active: showFavsOnly }" @click="showFavsOnly = true">&#11088; Favourites</button>
    </div>

    <div class="content">
        <!-- Artists Tab -->
        <div v-if="tab === 'artists' && !selectedArtist">
            <div v-if="visibleArtists.length === 0" class="empty-state">
                <h2>{{ showFavsOnly ? 'No favourite artists' : 'No artists found' }}</h2>
                <p v-if="!showFavsOnly">Add music folders and run a metadata refresh from the Browse page.</p>
            </div>
            <div v-for="a in visibleArtists" :key="a.name" class="list-item" @click="selectArtist(a.name)">
                <span class="icon">👤</span>
                <div class="info">
                    <div class="name">{{ a.name }}</div>
                </div>
                <span class="count">{{ a.song_count }} {{ a.song_count === 1 ? 'song' : 'songs' }}</span>
                <button class="star-btn" :class="{ favourited: isFav('artist', a.name) }" @click.stop="toggleFav('artist', a.name)" :title="isFav('artist', a.name) ? 'Remove from favourites' : 'Add to favourites'">{{ isFav('artist', a.name) ? '★' : '☆' }}</button>
            </div>
        </div>

        <!-- Artist Detail -->
        <div v-if="tab === 'artists' && selectedArtist">
            <div class="sub-header">
                <button class="back-btn" @click="selectedArtist = null">&larr; Artists</button>
                <h2>{{ selectedArtist }}</h2>
                <button class="play-all" @click="playAllSongs">&#9654; Play All</button>
            </div>
            <table class="song-table" v-if="filteredSongs.length > 0">
                <thead><tr>
                    <th class="track-num">#</th>
                    <th>Title</th>
                    <th>Album</th>
                    <th class="duration-col">Time</th>
                    <th class="actions-col"></th>
                </tr></thead>
                <tbody>
                    <tr v-for="(s, i) in filteredSongs" :key="s.path" :class="{ playing: isCurrentSong(s) }" @click="playSong(s, i)">
                        <td class="track-num">{{ s.track_number || i + 1 }}</td>
                        <td class="title-col"><span class="song-title">{{ s.title || s.filename }}</span></td>
                        <td class="album-col">{{ s.album }}</td>
                        <td class="duration-col">{{ formatDuration(s.duration) }}</td>
                        <td class="actions-col"><div class="action-btns">
                            <button class="action-btn play-btn" @click.stop="playSongNow(s)" title="Play now">&#9654;</button>
                            <button class="action-btn" @click.stop="addToQueueTop(s)" title="Add to top of queue">&#11014;Q</button>
                            <button class="action-btn" @click.stop="addToQueueBottom(s)" title="Add to bottom of queue">Q&#11015;</button>
                            <button class="action-btn" @click.stop="openPlaylistMenu($event, s)" title="Add to playlist">...</button>
                        </div></td>
                    </tr>
                </tbody>
            </table>
        </div>

        <!-- Albums Tab -->
        <div v-if="tab === 'albums' && !selectedAlbum">
            <div v-if="visibleAlbums.length === 0" class="empty-state">
                <h2>{{ showFavsOnly ? 'No favourite albums' : 'No albums found' }}</h2>
                <p v-if="!showFavsOnly">Add music folders and run a metadata refresh from the Browse page.</p>
            </div>
            <div v-for="a in visibleAlbums" :key="a.name + '|' + a.artist" class="list-item" @click="selectAlbum(a)">
                <span class="icon">💿</span>
                <div class="info">
                    <div class="name">{{ a.name }}</div>
                    <div class="detail">{{ a.artist }}{{ a.year ? ' \u2022 ' + a.year : '' }}</div>
                </div>
                <span class="count">{{ a.song_count }} {{ a.song_count === 1 ? 'song' : 'songs' }}</span>
                <button class="star-btn" :class="{ favourited: isFav('album', a.artist + '|' + a.name) }" @click.stop="toggleFav('album', a.artist + '|' + a.name)" :title="isFav('album', a.artist + '|' + a.name) ? 'Remove from favourites' : 'Add to favourites'">{{ isFav('album', a.artist + '|' + a.name) ? '★' : '☆' }}</button>
            </div>
        </div>

        <!-- Album Detail -->
        <div v-if="tab === 'albums' && selectedAlbum">
            <div class="sub-header">
                <button class="back-btn" @click="selectedAlbum = null">&larr; Albums</button>
                <h2>{{ selectedAlbum.name }}</h2>
                <button class="play-all" @click="playAllSongs">&#9654; Play All</button>
            </div>
            <p style="color: #8b949e; margin-bottom: 15px; font-size: 13px;">{{ selectedAlbum.artist }}{{ selectedAlbum.year ? ' \u2022 ' + selectedAlbum.year : '' }}</p>
            <table class="song-table" v-if="filteredSongs.length > 0">
                <thead><tr>
                    <th class="track-num">#</th>
                    <th>Title</th>
                    <th>Artist</th>
                    <th class="duration-col">Time</th>
                    <th class="actions-col"></th>
                </tr></thead>
                <tbody>
                    <tr v-for="(s, i) in filteredSongs" :key="s.path" :class="{ playing: isCurrentSong(s) }" @click="playSong(s, i)">
                        <td class="track-num">{{ s.track_number || i + 1 }}</td>
                        <td class="title-col"><span class="song-title">{{ s.title || s.filename }}</span></td>
                        <td class="artist-col">{{ s.artist }}</td>
                        <td class="duration-col">{{ formatDuration(s.duration) }}</td>
                        <td class="actions-col"><div class="action-btns">
                            <button class="action-btn play-btn" @click.stop="playSongNow(s)" title="Play now">&#9654;</button>
                            <button class="action-btn" @click.stop="addToQueueTop(s)" title="Add to top of queue">&#11014;Q</button>
                            <button class="action-btn" @click.stop="addToQueueBottom(s)" title="Add to bottom of queue">Q&#11015;</button>
                            <button class="action-btn" @click.stop="openPlaylistMenu($event, s)" title="Add to playlist">...</button>
                        </div></td>
                    </tr>
                </tbody>
            </table>
        </div>

        <!-- Genres Tab -->
        <div v-if="tab === 'genres' && !selectedGenre">
            <div v-if="visibleGenres.length === 0" class="empty-state">
                <h2>{{ showFavsOnly ? 'No favourite genres' : 'No genres found' }}</h2>
                <p v-if="!showFavsOnly">Add music folders and run a metadata refresh from the Browse page.</p>
            </div>
            <div v-for="g in visibleGenres" :key="g.name" class="list-item" @click="selectGenre(g.name)">
                <span class="icon">🎵</span>
                <div class="info">
                    <div class="name">{{ g.name }}</div>
                </div>
                <span class="count">{{ g.song_count }} {{ g.song_count === 1 ? 'song' : 'songs' }}</span>
                <button class="star-btn" :class="{ favourited: isFav('genre', g.name) }" @click.stop="toggleFav('genre', g.name)" :title="isFav('genre', g.name) ? 'Remove from favourites' : 'Add to favourites'">{{ isFav('genre', g.name) ? '★' : '☆' }}</button>
            </div>
        </div>

        <!-- Genre Detail -->
        <div v-if="tab === 'genres' && selectedGenre">
            <div class="sub-header">
                <button class="back-btn" @click="selectedGenre = null">&larr; Genres</button>
                <h2>{{ selectedGenre }}</h2>
                <button class="play-all" @click="playAllSongs">&#9654; Play All</button>
            </div>
            <table class="song-table" v-if="filteredSongs.length > 0">
                <thead><tr>
                    <th class="track-num">#</th>
                    <th>Title</th>
                    <th>Artist</th>
                    <th>Album</th>
                    <th class="duration-col">Time</th>
                    <th class="actions-col"></th>
                </tr></thead>
                <tbody>
                    <tr v-for="(s, i) in filteredSongs" :key="s.path" :class="{ playing: isCurrentSong(s) }" @click="playSong(s, i)">
                        <td class="track-num">{{ s.track_number || i + 1 }}</td>
                        <td class="title-col"><span class="song-title">{{ s.title || s.filename }}</span></td>
                        <td class="artist-col">{{ s.artist }}</td>
                        <td class="album-col">{{ s.album }}</td>
                        <td class="duration-col">{{ formatDuration(s.duration) }}</td>
                        <td class="actions-col"><div class="action-btns">
                            <button class="action-btn play-btn" @click.stop="playSongNow(s)" title="Play now">&#9654;</button>
                            <button class="action-btn" @click.stop="addToQueueTop(s)" title="Add to top of queue">&#11014;Q</button>
                            <button class="action-btn" @click.stop="addToQueueBottom(s)" title="Add to bottom of queue">Q&#11015;</button>
                            <button class="action-btn" @click.stop="openPlaylistMenu($event, s)" title="Add to playlist">...</button>
                        </div></td>
                    </tr>
                </tbody>
            </table>
        </div>

        <!-- Songs Tab (all songs) -->
        <div v-if="tab === 'songs'">
            <div v-if="visibleSongs.length === 0" class="empty-state">
                <h2>{{ showFavsOnly ? 'No favourite songs' : 'No songs found' }}</h2>
                <p v-if="!showFavsOnly">Add music folders and run a metadata refresh from the Browse page.</p>
            </div>
            <div class="sub-header" v-if="visibleSongs.length > 0">
                <h2>{{ showFavsOnly ? 'Favourite Songs' : 'All Songs' }} ({{ visibleSongs.length }})</h2>
                <button class="play-all" @click="playAllSongs">&#9654; Play All</button>
            </div>
            <table class="song-table" v-if="visibleSongs.length > 0">
                <thead><tr>
                    <th class="track-num">#</th>
                    <th>Title</th>
                    <th>Artist</th>
                    <th>Album</th>
                    <th class="duration-col">Time</th>
                    <th class="actions-col"></th>
                </tr></thead>
                <tbody>
                    <tr v-for="(s, i) in visibleSongs" :key="s.path" :class="{ playing: isCurrentSong(s) }" @click="playSong(s, i)">
                        <td class="track-num">{{ i + 1 }}</td>
                        <td class="title-col"><span class="song-title">{{ s.title || s.filename }}</span></td>
                        <td class="artist-col">{{ s.artist }}</td>
                        <td class="album-col">{{ s.album }}</td>
                        <td class="duration-col">{{ formatDuration(s.duration) }}</td>
                        <td class="actions-col"><div class="action-btns">
                            <button class="star-btn" :class="{ favourited: isFav('song', s.path) }" @click.stop="toggleFav('song', s.path)" style="font-size:16px;">{{ isFav('song', s.path) ? '★' : '☆' }}</button>
                            <button class="action-btn play-btn" @click.stop="playSongNow(s)" title="Play now">&#9654;</button>
                            <button class="action-btn" @click.stop="addToQueueTop(s)" title="Add to top of queue">&#11014;Q</button>
                            <button class="action-btn" @click.stop="addToQueueBottom(s)" title="Add to bottom of queue">Q&#11015;</button>
                            <button class="action-btn" @click.stop="openPlaylistMenu($event, s)" title="Add to playlist">...</button>
                        </div></td>
                    </tr>
                </tbody>
            </table>
        </div>

        <!-- Playlists Tab -->
        <div v-if="tab === 'playlists' && !selectedPlaylist">
            <!-- Smart playlist: Most Played -->
            <div class="list-item" style="border-left: 3px solid #58a6ff;" @click="selectSmartPlaylist('top')">
                <span class="icon">&#11088;</span>
                <div class="info">
                    <div class="name" style="color:#58a6ff;">Most Played — Last 3 Months</div>
                    <div class="detail">Auto-generated</div>
                </div>
            </div>
            <div v-if="playlists.length === 0" style="padding: 12px 16px; color: #484f58; font-size: 13px;">No saved playlists yet</div>
            <div v-for="pl in playlists" :key="pl.path" class="list-item" @click="selectPlaylist(pl)">
                <span class="icon">&#9654;</span>
                <div class="info">
                    <div class="name">{{ pl.name }}</div>
                    <div class="detail">{{ pl.count }} song{{ pl.count !== 1 ? 's' : '' }}</div>
                </div>
            </div>
        </div>

        <div v-if="tab === 'playlists' && selectedPlaylist">
            <div class="sub-header">
                <button class="back-btn" @click="selectedPlaylist = null">&#8592; Playlists</button>
                <h2>{{ selectedPlaylist.name }}</h2>
                <button class="play-all" @click="playPlaylist" v-if="playlistSongs.length > 0">&#9654; Play All</button>
            </div>
            <div v-if="playlistSongs.length === 0" class="empty-state" style="padding:30px 0;">
                <p>This playlist is empty.</p>
            </div>
            <table class="song-table" v-if="playlistSongs.length > 0">
                <thead><tr>
                    <th class="track-num">#</th>
                    <th>Title</th>
                    <th v-if="selectedPlaylist.smart">Artist</th>
                    <th v-if="selectedPlaylist.smart" style="width:60px;text-align:right;">Plays</th>
                    <th class="duration-col">Time</th>
                    <th class="actions-col"></th>
                </tr></thead>
                <tbody>
                    <tr v-for="(s, i) in playlistSongs" :key="s.path" @click="playPlaylistSong(s, i)">
                        <td class="track-num">{{ i + 1 }}</td>
                        <td class="title-col">
                            <span class="song-title">{{ s.title }}</span>
                            <div v-if="!selectedPlaylist.smart" style="font-size:11px;color:#8b949e;">{{ s.artist }}</div>
                        </td>
                        <td v-if="selectedPlaylist.smart" class="artist-col">{{ s.artist }}</td>
                        <td v-if="selectedPlaylist.smart" style="text-align:right;color:#8b949e;font-size:12px;">{{ s.play_count }}</td>
                        <td class="duration-col">{{ formatDuration(s.duration) }}</td>
                        <td class="actions-col"><div class="action-btns">
                            <button class="action-btn play-btn" @click.stop="playSongNow(s)" title="Play now">&#9654;</button>
                            <button class="action-btn" @click.stop="addToQueueTop(s)" title="Add to top of queue">&#11014;Q</button>
                            <button class="action-btn" @click.stop="addToQueueBottom(s)" title="Add to bottom of queue">Q&#11015;</button>
                        </div></td>
                    </tr>
                </tbody>
            </table>
        </div>
    </div>

    <!-- Playlist Menu Popup -->
    <div class="playlist-popup" :class="{ hidden: !showPlaylistMenu }" :style="playlistMenuStyle">
        <div class="playlist-popup-header">
            <span>Add to Playlist</span>
            <button @click="closePlaylistMenu">&times;</button>
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

    <!-- Now Playing Overlay -->
    <div class="now-playing" :class="{ hidden: !showNowPlaying }">
        <div class="np-header">
            <div style="min-width:0; flex:1;">
                <h2>{{ currentTrack?.title || currentTrack?.filename || '' }}</h2>
                <div class="np-artist">{{ [currentTrack?.artist, currentTrack?.album].filter(Boolean).join(' · ') }}</div>
            </div>
            <button class="np-close" @click="showNowPlaying = false">Close ↓</button>
        </div>
        <div class="np-body">
            <div class="np-visualiser">
                <canvas ref="analyserCanvas" style="height:180px;"></canvas>
            </div>
            <div class="np-lyrics" v-if="lyricsLines.length > 0">
                <div v-for="(line, i) in lyricsLines" :key="i"
                     :ref="el => { if (el) lyricRefs[i] = el }"
                     class="lyric-line"
                     :class="{ active: i === activeLyricIndex, past: i < activeLyricIndex }"
                     @click="seekToLyric(line.time)">
                    {{ line.text }}
                </div>
            </div>
            <div class="np-lyrics" v-else-if="plainLyrics">
                <div class="np-plain-lyrics">{{ plainLyrics }}</div>
            </div>
            <div class="np-lyrics" v-else-if="lyricsLoading">
                <div class="np-no-lyrics">Loading lyrics…</div>
            </div>
            <div class="np-lyrics" v-else>
                <div class="np-no-lyrics">No lyrics found</div>
            </div>
        </div>
    </div>

    <!-- Cast Panel -->
    <!-- Queue Panel -->
    <div class="queue-panel" :class="{ hidden: !showQueuePanel }" @click.stop>
        <div class="queue-header">
            <h3>Queue</h3>
            <div style="display:flex; align-items:center; gap:10px;">
                <span class="queue-count">{{ queue.length }} song{{ queue.length !== 1 ? 's' : '' }}</span>
                <button v-if="queue.length > 0" class="q-clear" @click="clearQueue" title="Clear queue">Clear</button>
            </div>
        </div>
        <div class="queue-list">
            <div v-if="queue.length === 0" class="queue-empty">No songs in queue</div>
            <div v-for="(song, i) in queue" :key="i"
                 class="queue-item" :class="{ current: i === currentIndex }"
                 @click="jumpToQueueItem(i)">
                <span class="q-index">{{ i === currentIndex ? '▶' : i + 1 }}</span>
                <div class="q-info">
                    <div class="q-title">{{ song.title || song.filename }}</div>
                    <div class="q-artist">{{ song.artist || '' }}</div>
                </div>
                <button class="q-remove" @click.stop="removeFromQueue(i)" title="Remove">✕</button>
            </div>
        </div>
    </div>

    <div class="cast-panel" :class="{ hidden: !showCastPanel }">
        <div class="cast-header">
            <h3>Cast to device</h3>
            <button v-if="!castScanning" class="cast-refresh" @click="scanCastDevices">Refresh</button>
            <span v-else class="cast-scanning">Scanning...</span>
        </div>
        <div class="cast-list">
            <div class="cast-device" :class="{ active: !isCasting }" @click="stopCasting">
                <span class="icon">&#128187;</span>
                <span class="name">This device</span>
                <span v-if="!isCasting" class="status">Playing</span>
            </div>
            <div v-if="isCasting" class="cast-device active">
                <span class="icon">&#128250;</span>
                <span class="name">{{ castingTo }}</span>
                <span class="status">Casting</span>
            </div>
            <div v-for="device in castDevices" :key="device.uuid"
                 class="cast-device"
                 :class="{ active: isCasting && castingTo === device.name }"
                 @click="connectCastDevice(device)">
                <span class="icon">&#128250;</span>
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

    <!-- Audio Player -->
    <div class="audio-player" :class="{ hidden: !currentTrack }">
        <div class="player-controls">
            <button class="player-btn" @click="playPrevious" title="Previous">&#9198;</button>
            <button class="player-btn play-pause" @click="togglePlay" :title="isPlaying ? 'Pause' : 'Play'">
                {{ isPlaying ? '\u23F8' : '\u25B6' }}
            </button>
            <button class="player-btn" @click="playNext" title="Next">&#9197;</button>
        </div>
        <div class="track-info">
            <div class="track-name">{{ currentTrack?.title || currentTrack?.filename || 'No track' }}</div>
            <div class="track-artist">{{ currentTrack?.artist || '' }}</div>
        </div>
        <div class="progress-container">
            <span class="time-display">{{ formatDuration(Math.floor(currentTime)) }} / {{ formatDuration(Math.floor(audioDuration)) }}</span>
            <div class="progress-bar" @click="seek($event)">
                <div class="progress-fill" :style="{ width: progressPercent + '%' }"></div>
            </div>
        </div>
        <div class="player-right">
            <div class="volume-control">
                <button class="player-btn" @click="toggleMute" style="font-size:16px;">{{ isMuted ? '\uD83D\uDD07' : '\uD83D\uDD0A' }}</button>
                <input type="range" class="volume-slider" min="0" max="1" step="0.05" v-model.number="volume" @input="setVolume">
            </div>
            <button class="player-btn" @click="showNowPlaying = !showNowPlaying" :title="showNowPlaying ? 'Collapse' : 'Now Playing'" style="font-size:16px;">{{ showNowPlaying ? '↓' : '↑' }}</button>
            <button class="player-btn" :class="{ active: showQueuePanel }" @click="toggleQueuePanel" title="Queue" style="font-size:16px;">&#9776;</button>
            <button class="player-btn cast-btn" :class="{ casting: isCasting }" @click="toggleCastPanel" title="Cast">&#128250;</button>
        </div>
    </div>

    <audio ref="audioEl" @timeupdate="onTimeUpdate" @loadedmetadata="onLoaded" @ended="playNext"></audio>
</div>

<script>
const { createApp, ref, computed, watch, onMounted, onUnmounted, nextTick } = Vue;

createApp({
    setup() {
        const tab = ref('artists');
        const artists = ref([]);
        const albums = ref([]);
        const genres = ref([]);
        const songs = ref([]);
        const selectedArtist = ref(null);
        const selectedAlbum = ref(null);
        const selectedGenre = ref(null);
        const playlists = ref([]);
        const selectedPlaylist = ref(null);
        const playlistSongs = ref([]);
        const searchQuery = ref('');
        const showFavsOnly = ref(false);
        const favourites = ref({}); // { 'artist|name': true, 'song|path': true, ... }

        // Player state
        const queue = ref([]);
        const currentIndex = ref(-1);
        const isPlaying = ref(false);
        const currentTime = ref(0);
        const audioDuration = ref(0);
        const isMuted = ref(false);
        const volume = ref(1);
        const audioEl = ref(null);

        // Queue panel
        const showQueuePanel = ref(false);
        const toggleQueuePanel = () => { showQueuePanel.value = !showQueuePanel.value; };

        // Now Playing
        const showNowPlaying = ref(false);
        const analyserCanvas = ref(null);
        const lyricsLines = ref([]); // [{time: seconds, text: string}]
        const plainLyrics = ref('');
        const lyricsLoading = ref(false);
        const activeLyricIndex = ref(-1);
        const lyricRefs = {};
        let audioCtx = null;
        let analyserNode = null;
        let analyserSource = null;
        let animFrameId = null;
        const jumpToQueueItem = (i) => {
            currentIndex.value = i;
            startPlayback();
        };
        const removeFromQueue = (i) => {
            queue.value.splice(i, 1);
            if (i < currentIndex.value) {
                currentIndex.value--;
            } else if (i === currentIndex.value) {
                if (currentIndex.value >= queue.value.length) {
                    currentIndex.value = queue.value.length - 1;
                }
                if (currentIndex.value >= 0) startPlayback();
                else { currentIndex.value = -1; audioEl.value?.pause(); }
            }
        };

        // Cast state
        const showCastPanel = ref(false);
        const castDevices = ref([]);
        const castScanning = ref(false);
        const castScanError = ref(null);
        const isCasting = ref(false);
        const castingTo = ref(null);
        let castStatusInterval = null;

        const currentTrack = computed(() =>
            currentIndex.value >= 0 && currentIndex.value < queue.value.length
                ? queue.value[currentIndex.value] : null
        );
        const progressPercent = computed(() =>
            audioDuration.value > 0 ? (currentTime.value / audioDuration.value) * 100 : 0
        );

        const matchesSearch = (text) => {
            if (!searchQuery.value) return true;
            return text.toLowerCase().includes(searchQuery.value.toLowerCase());
        };
        const filteredArtists = computed(() => artists.value.filter(a => matchesSearch(a.name)));
        const filteredAlbums = computed(() => albums.value.filter(a => matchesSearch(a.name) || matchesSearch(a.artist)));
        const filteredGenres = computed(() => genres.value.filter(g => matchesSearch(g.name)));
        const filteredSongs = computed(() => songs.value.filter(s =>
            matchesSearch(s.title || s.filename) || matchesSearch(s.artist) || matchesSearch(s.album)
        ));

        // Favourites helpers
        const favKey = (type, key) => type + '|' + key;
        const isFav = (type, key) => !!favourites.value[favKey(type, key)];
        const toggleFav = async (type, key) => {
            const resp = await fetch('/api/favourites', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ type, key }),
            });
            if (!resp.ok) return;
            const data = await resp.json();
            const k = favKey(type, key);
            if (data.favourited) favourites.value[k] = true;
            else delete favourites.value[k];
            // Trigger reactivity
            favourites.value = { ...favourites.value };
        };
        const loadFavourites = async () => {
            const types = ['artist', 'album', 'genre', 'song'];
            const map = {};
            await Promise.all(types.map(async (type) => {
                const r = await fetch('/api/favourites?type=' + type);
                if (!r.ok) return;
                const data = await r.json();
                (data.keys || []).forEach(k => { map[favKey(type, k)] = true; });
            }));
            favourites.value = map;
        };

        // Visible lists (filtered + favourites filter applied)
        const visibleArtists = computed(() => showFavsOnly.value ? filteredArtists.value.filter(a => isFav('artist', a.name)) : filteredArtists.value);
        const visibleAlbums = computed(() => showFavsOnly.value ? filteredAlbums.value.filter(a => isFav('album', a.artist + '|' + a.name)) : filteredAlbums.value);
        const visibleGenres = computed(() => showFavsOnly.value ? filteredGenres.value.filter(g => isFav('genre', g.name)) : filteredGenres.value);
        const visibleSongs = computed(() => showFavsOnly.value ? filteredSongs.value.filter(s => isFav('song', s.path)) : filteredSongs.value);

        const formatDuration = (secs) => {
            if (!secs || secs <= 0) return '--:--';
            const m = Math.floor(secs / 60);
            const s = Math.floor(secs % 60);
            return m + ':' + (s < 10 ? '0' : '') + s;
        };

        const fetchArtists = async () => {
            const r = await fetch('/api/music/artists');
            if (!r.ok) return;
            artists.value = await r.json();
        };
        const fetchAlbums = async (artist) => {
            const url = artist ? '/api/music/albums?artist=' + encodeURIComponent(artist) : '/api/music/albums';
            const r = await fetch(url);
            if (!r.ok) return;
            albums.value = await r.json();
        };
        const fetchGenres = async () => {
            const r = await fetch('/api/music/genres');
            if (!r.ok) return;
            genres.value = await r.json();
        };
        const fetchSongs = async (params) => {
            let url = '/api/music/songs';
            const q = new URLSearchParams(params || {});
            if (q.toString()) url += '?' + q.toString();
            const r = await fetch(url);
            if (!r.ok) return;
            songs.value = await r.json();
        };

        const fetchPlaylists = async () => {
            const r = await fetch('/api/playlists');
            if (!r.ok) return;
            const data = await r.json();
            playlists.value = data.playlists || [];
        };

        const selectPlaylist = async (pl) => {
            selectedPlaylist.value = pl;
            const r = await fetch('/api/playlist?path=' + encodeURIComponent(pl.path));
            if (!r.ok) return;
            const data = await r.json();
            playlistSongs.value = (data.songs || []).map(s => ({ ...s, filename: s.title }));
        };

        const selectSmartPlaylist = async (type) => {
            if (type === 'top') {
                selectedPlaylist.value = { name: 'Most Played — Last 3 Months', smart: true };
                playlistSongs.value = [];
                const r = await fetch('/api/music/top');
                if (!r.ok) return;
                const data = await r.json();
                playlistSongs.value = (data || []).map(s => ({ ...s, filename: s.title || s.filename }));
            }
        };

        const songToQueueItem = (s) => ({ path: s.path, title: s.title || s.filename, filename: s.filename || s.title, artist: s.artist || '', album: s.album || '', duration: s.duration });

        const playPlaylist = () => {
            if (!playlistSongs.value.length) return;
            queue.value = playlistSongs.value.map(songToQueueItem);
            currentIndex.value = 0;
            startPlayback();
        };

        const playPlaylistSong = (s, i) => {
            queue.value = playlistSongs.value.map(songToQueueItem);
            currentIndex.value = i;
            startPlayback();
        };

        const switchTab = (t) => {
            tab.value = t;
            selectedArtist.value = null;
            selectedAlbum.value = null;
            selectedGenre.value = null;
            selectedPlaylist.value = null;
            if (t === 'artists') fetchArtists();
            else if (t === 'albums') fetchAlbums();
            else if (t === 'genres') fetchGenres();
            else if (t === 'songs') fetchSongs();
            else if (t === 'playlists') fetchPlaylists();
        };

        const selectArtist = (name) => {
            selectedArtist.value = name;
            fetchSongs({ artist: name });
        };

        const selectAlbum = (a) => {
            selectedAlbum.value = a;
            fetchSongs({ artist: a.artist, album: a.name });
        };

        const selectGenre = (name) => {
            selectedGenre.value = name;
            fetchSongs({ genre: name });
        };

        // Playback
        const playSong = (song, index) => {
            queue.value = [...filteredSongs.value];
            currentIndex.value = index;
            startPlayback();
        };

        const playAllSongs = () => {
            if (filteredSongs.value.length === 0) return;
            queue.value = [...filteredSongs.value];
            currentIndex.value = 0;
            startPlayback();
        };

        const addToQueue = (song) => {
            queue.value.push(song);
            if (currentIndex.value < 0) {
                currentIndex.value = 0;
                startPlayback();
            }
        };

        // Play a single song immediately without replacing the queue
        const playSongNow = (song) => {
            const insertAt = currentIndex.value >= 0 ? currentIndex.value + 1 : 0;
            queue.value.splice(insertAt, 0, song);
            currentIndex.value = insertAt;
            startPlayback();
        };

        const addToQueueTop = (song) => {
            queue.value.splice(0, 0, song);
            if (currentIndex.value < 0) {
                currentIndex.value = 0;
                startPlayback();
            } else {
                currentIndex.value++; // item inserted before current; shift index
            }
        };

        const addToQueueBottom = (song) => {
            queue.value.push(song);
            if (currentIndex.value < 0) {
                currentIndex.value = 0;
                startPlayback();
            }
        };

        const clearQueue = () => {
            queue.value = [];
            currentIndex.value = -1;
            if (audioEl.value) { audioEl.value.pause(); audioEl.value.src = ''; }
        };

        // Playlist menu state
        const showPlaylistMenu = ref(false);
        const availablePlaylists = ref([]);
        const playlistMenuSong = ref(null);
        const playlistMenuX = ref(0);
        const playlistMenuY = ref(0);
        const playlistMenuStyle = computed(() => ({
            left: playlistMenuX.value + 'px',
            top: playlistMenuY.value + 'px'
        }));

        const openPlaylistMenu = async (event, song) => {
            playlistMenuSong.value = song;
            const rect = event.target.getBoundingClientRect();
            playlistMenuX.value = Math.min(rect.left, window.innerWidth - 250);
            playlistMenuY.value = rect.bottom + 5;

            try {
                const resp = await fetch('/api/playlist/check?song=' + encodeURIComponent(song.path));
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
                        title: playlistMenuSong.value.title || playlistMenuSong.value.filename,
                        duration: playlistMenuSong.value.duration || 0
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

        const startPlayback = () => {
            const track = queue.value[currentIndex.value];
            if (!track) return;
            if (isCasting.value) {
                castCurrentTrack();
                return;
            }
            if (!audioEl.value) return;
            audioEl.value.src = '/api/stream?path=' + encodeURIComponent(track.path);
            audioEl.value.play().then(() => { isPlaying.value = true; }).catch(e => console.error(e));
            // Record the play in history
            fetch('/api/history/record?path=' + encodeURIComponent(track.path), { method: 'POST' }).catch(() => {});
        };

        const togglePlay = () => {
            if (isCasting.value) {
                castTogglePlay();
                return;
            }
            if (!audioEl.value) return;
            if (isPlaying.value) {
                audioEl.value.pause();
                isPlaying.value = false;
            } else if (currentTrack.value) {
                audioEl.value.play().then(() => { isPlaying.value = true; });
            }
        };

        const playNext = () => {
            if (currentIndex.value < queue.value.length - 1) {
                currentIndex.value++;
                startPlayback();
            } else {
                isPlaying.value = false;
            }
        };

        const playPrevious = () => {
            if (!audioEl.value) return;
            if (audioEl.value.currentTime > 3) {
                audioEl.value.currentTime = 0;
            } else if (currentIndex.value > 0) {
                currentIndex.value--;
                startPlayback();
            }
        };

        const seek = (e) => {
            const rect = e.currentTarget.getBoundingClientRect();
            const pct = (e.clientX - rect.left) / rect.width;
            if (isCasting.value) {
                castSeek(pct);
                return;
            }
            if (!audioEl.value || !audioDuration.value) return;
            audioEl.value.currentTime = pct * audioDuration.value;
        };

        const toggleMute = () => {
            if (!audioEl.value) return;
            isMuted.value = !isMuted.value;
            audioEl.value.muted = isMuted.value;
        };

        const setVolume = () => {
            if (isCasting.value) {
                castSetVolume(volume.value);
                return;
            }
            if (!audioEl.value) return;
            audioEl.value.volume = volume.value;
        };

        const onTimeUpdate = () => {
            if (audioEl.value) currentTime.value = audioEl.value.currentTime;
        };
        const onLoaded = () => {
            if (audioEl.value) audioDuration.value = audioEl.value.duration;
        };

        const isCurrentSong = (s) => currentTrack.value && currentTrack.value.path === s.path;

        // Cast functions
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
                    if (audioEl.value && !audioEl.value.paused) {
                        audioEl.value.pause();
                    }
                    startCastStatusPolling();
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
                        } else if (status.player_state === 'IDLE' && status.idle_reason === 'FINISHED') {
                            playNext();
                        }
                        currentTime.value = status.current_time;
                        if (status.duration > 0) audioDuration.value = status.duration;
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

        const toggleCastPanel = () => {
            showCastPanel.value = !showCastPanel.value;
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
            // Resume local playback
            if (currentTrack.value && audioEl.value) {
                if (!audioEl.value.src || !audioEl.value.src.includes(encodeURIComponent(currentTrack.value.path))) {
                    audioEl.value.src = '/api/stream?path=' + encodeURIComponent(currentTrack.value.path);
                }
                audioEl.value.currentTime = resumeTime;
                if (wasPlaying) {
                    audioEl.value.play().then(() => { isPlaying.value = true; }).catch(e => console.error(e));
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
                        title: currentTrack.value.title || currentTrack.value.filename
                    })
                });
                const data = await resp.json();
                if (data.success) {
                    isPlaying.value = true;
                }
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
            const seekTime = percent * audioDuration.value;
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

        const castSetVolume = async (level) => {
            if (!isCasting.value) return;
            try {
                await fetch('/api/cast/volume', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ level: level })
                });
            } catch (e) {
                console.error('Failed to set cast volume:', e);
            }
        };

        // Spectrum analyser
        const setupAnalyser = () => {
            if (!audioEl.value) return;
            if (!audioCtx) {
                audioCtx = new (window.AudioContext || window.webkitAudioContext)();
            }
            if (audioCtx.state === 'suspended') audioCtx.resume();
            if (!analyserSource) {
                analyserSource = audioCtx.createMediaElementSource(audioEl.value);
                analyserNode = audioCtx.createAnalyser();
                analyserNode.fftSize = 256;
                analyserSource.connect(analyserNode);
                analyserNode.connect(audioCtx.destination);
            }
        };

        const drawAnalyser = () => {
            animFrameId = requestAnimationFrame(drawAnalyser);
            if (!analyserNode || !analyserCanvas.value || !showNowPlaying.value) return;
            const canvas = analyserCanvas.value;
            const ctx = canvas.getContext('2d');
            const bufLen = analyserNode.frequencyBinCount;
            const data = new Uint8Array(bufLen);
            analyserNode.getByteFrequencyData(data);

            canvas.width = canvas.offsetWidth;
            canvas.height = canvas.offsetHeight;
            ctx.clearRect(0, 0, canvas.width, canvas.height);

            // Block style: fixed-width columns with gaps, each split into segments
            const numBars = 48;
            const gap = 3;
            const blockGap = 2;
            const numBlocks = 12;
            const barW = (canvas.width - gap * (numBars - 1)) / numBars;
            const blockH = (canvas.height - blockGap * (numBlocks - 1)) / numBlocks;

            for (let i = 0; i < numBars; i++) {
                // Average a slice of frequency bins for this bar
                const start = Math.floor(i / numBars * bufLen);
                const end = Math.floor((i + 1) / numBars * bufLen);
                let sum = 0;
                for (let j = start; j < end; j++) sum += data[j];
                const avg = end > start ? sum / (end - start) : 0;
                const filledBlocks = Math.round((avg / 255) * numBlocks);

                const x = i * (barW + gap);
                for (let b = 0; b < filledBlocks; b++) {
                    const y = canvas.height - (b + 1) * (blockH + blockGap) + blockGap;
                    // Colour: green at bottom, yellow in middle, blue-white at top
                    const t = b / numBlocks;
                    const hue = t < 0.6 ? 140 - t * 60 : 200 + (t - 0.6) * 100;
                    const light = 45 + t * 20;
                    ctx.fillStyle = 'hsl(' + hue + ', 70%, ' + light + '%)';
                    ctx.fillRect(x, y, barW, blockH);
                }
            }
        };

        watch(showNowPlaying, (val) => {
            if (val) {
                nextTick(() => {
                    setupAnalyser();
                    if (!animFrameId) drawAnalyser();
                });
            } else {
                if (animFrameId) { cancelAnimationFrame(animFrameId); animFrameId = null; }
            }
        });

        // Lyrics
        const parseLRC = (lrc) => {
            if (!lrc) return [];
            return lrc.split('\n')
                .map(line => {
                    const m = line.match(/^\[(\d+):(\d+\.\d+)\](.*)/);
                    if (!m) return null;
                    return { time: parseInt(m[1]) * 60 + parseFloat(m[2]), text: m[3].trim() };
                })
                .filter(l => l && l.text);
        };

        const fetchLyrics = async (track) => {
            if (!track) return;
            lyricsLines.value = [];
            plainLyrics.value = '';
            activeLyricIndex.value = -1;
            lyricsLoading.value = true;
            try {
                const params = new URLSearchParams({ path: track.path });
                const resp = await fetch('/api/lyrics?' + params);
                const data = await resp.json();
                lyricsLines.value = parseLRC(data.synced_lyrics);
                if (lyricsLines.value.length === 0) plainLyrics.value = data.plain_lyrics || '';
            } catch (e) {
                console.error('Failed to fetch lyrics:', e);
            }
            lyricsLoading.value = false;
        };

        watch(currentTrack, (track) => { fetchLyrics(track); }, { immediate: true });

        // Active lyric line tracking
        watch(currentTime, (t) => {
            if (!lyricsLines.value.length) return;
            let idx = -1;
            for (let i = 0; i < lyricsLines.value.length; i++) {
                if (lyricsLines.value[i].time <= t) idx = i;
                else break;
            }
            if (idx !== activeLyricIndex.value) {
                activeLyricIndex.value = idx;
                nextTick(() => {
                    const el = lyricRefs[idx];
                    if (el) el.scrollIntoView({ behavior: 'smooth', block: 'center' });
                });
            }
        });

        const seekToLyric = (time) => {
            if (audioEl.value) { audioEl.value.currentTime = time; currentTime.value = time; }
        };

        const handleQueueClickOutside = (e) => {
            if (!showQueuePanel.value) return;
            const panel = document.querySelector('.queue-panel');
            const btn = document.querySelector('[title="Queue"]');
            if (panel && !panel.contains(e.target) && btn && !btn.contains(e.target)) {
                showQueuePanel.value = false;
            }
        };

        onMounted(() => {
            loadFavourites();

            const hash = window.location.hash.replace('#', '');
            const validTabs = ['artists', 'albums', 'genres', 'songs', 'playlists'];
            if (validTabs.includes(hash)) {
                switchTab(hash);
            } else {
                fetchArtists();
            }

            document.addEventListener('click', handleQueueClickOutside);
        });

        onUnmounted(() => {
            document.removeEventListener('click', handleQueueClickOutside);
        });

        return {
            tab, artists, albums, genres, songs, searchQuery,
            filteredArtists, filteredAlbums, filteredGenres, filteredSongs,
            visibleArtists, visibleAlbums, visibleGenres, visibleSongs,
            showFavsOnly, isFav, toggleFav,
            selectedArtist, selectedAlbum, selectedGenre,
            playlists, selectedPlaylist, playlistSongs,
            switchTab, selectArtist, selectAlbum, selectGenre,
            selectPlaylist, selectSmartPlaylist, playPlaylist, playPlaylistSong,
            queue, currentIndex, currentTrack, isPlaying, currentTime, audioDuration,
            isMuted, volume, audioEl, progressPercent,
            formatDuration, playSong, playAllSongs, addToQueue,
            playSongNow, addToQueueTop, addToQueueBottom,
            showPlaylistMenu, availablePlaylists, playlistMenuStyle,
            openPlaylistMenu, closePlaylistMenu, addToPlaylist, createNewPlaylist,
            togglePlay, playNext, playPrevious, seek, toggleMute, setVolume,
            onTimeUpdate, onLoaded, isCurrentSong,
            showQueuePanel, toggleQueuePanel, jumpToQueueItem, removeFromQueue, clearQueue,
            showCastPanel, castDevices, castScanning, castScanError, isCasting, castingTo,
            toggleCastPanel, scanCastDevices, connectCastDevice, stopCasting,
            showNowPlaying, analyserCanvas, lyricsLines, plainLyrics, lyricsLoading,
            activeLyricIndex, lyricRefs, seekToLyric,
        };
    }
}).mount('#app');
</script>
</body>
</html>` + ""

// makeSchemaHandler creates a handler for /schema.
