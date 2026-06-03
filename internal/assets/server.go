package assets

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// StartServer starts the Web GUI server on the given port.
func StartServer(assetsDir string, port int, lightsAddr string) error {
	// Serve static files from assets directory
	assetsFileServer := http.FileServer(http.Dir(assetsDir))

	http.HandleFunc("/assets/", func(w http.ResponseWriter, r *http.Request) {
		// Strip the "/assets/" prefix so it maps to assetsDir root
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/assets")
		// Add headers to allow caching or CORS if needed
		w.Header().Set("Access-Control-Allow-Origin", "*")
		assetsFileServer.ServeHTTP(w, r)
	})

	// API: Get all assets
	http.HandleFunc("/api/assets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		idx, err := LoadIndex(assetsDir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(idx)
	})

	// API: Trigger Indexing
	http.HandleFunc("/api/assets/index", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		idx, err := AutoIndex(assetsDir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(idx)
	})

	// API: Update Asset details
	http.HandleFunc("/api/assets/update", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID    string   `json:"id"`
			Tags  []string `json:"tags"`
			Style string   `json:"style"`
			HasBg bool     `json:"has_bg"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		idx, err := LoadIndex(assetsDir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		found := false
		for i, a := range idx.Assets {
			if a.ID == req.ID {
				idx.Assets[i].Tags = req.Tags
				idx.Assets[i].Style = req.Style
				idx.Assets[i].HasBg = req.HasBg
				found = true
				break
			}
		}

		if !found {
			http.Error(w, "Asset not found", http.StatusNotFound)
			return
		}

		if err := SaveIndex(assetsDir, idx); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	})

	// API: Process Background Removal for one asset
	http.HandleFunc("/api/assets/process-bg", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID      string `json:"id"`
			Backend string `json:"backend"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		idx, err := LoadIndex(assetsDir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var target AssetEntry
		found := false
		for _, a := range idx.Assets {
			if a.ID == req.ID {
				target = a
				found = true
				break
			}
		}

		if !found {
			http.Error(w, "Asset not found", http.StatusNotFound)
			return
		}

		path := filepath.Join(assetsDir, target.File)
		if req.Backend == "" {
			req.Backend = "chromakey"
		}

		var removeErr error
		if req.Backend == "zen-lights" {
			removeErr = removeBgLights(path, lightsAddr)
		} else if req.Backend == "rembg" {
			removeErr = removeBgRembg(path)
		} else {
			removeErr = removeBgChromaKey(path)
		}

		if removeErr != nil {
			http.Error(w, removeErr.Error(), http.StatusInternalServerError)
			return
		}

		// Update entry state
		for i, entry := range idx.Assets {
			if entry.ID == req.ID {
				idx.Assets[i].HasBg = false
				break
			}
		}
		if err := SaveIndex(assetsDir, idx); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "success"})
	})

	// API: Generate single asset
	http.HandleFunc("/api/assets/generate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Prompt string `json:"prompt"`
			Style  string `json:"style"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		slug := cleanSlug(req.Prompt)
		fileName := filepath.Join("categories", "generated", slug+".png")
		destPath := filepath.Join(assetsDir, fileName)

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		styledPrompt := req.Prompt
		if req.Style == "vector" {
			styledPrompt = fmt.Sprintf("vector icon of %s, hand-drawn whiteboard sketch, black ink lines, transparent background, educational, simple", req.Prompt)
		} else if req.Style == "realistic" {
			styledPrompt = fmt.Sprintf("%s, realistic style photograph, studio lighting, isolated on solid white background", req.Prompt)
		}

		if err := GenerateSingleAsset(styledPrompt, destPath, lightsAddr); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		hasBg := checkPNGHasBackground(destPath)
		width, height, _ := getImageDimensions(destPath)

		// Auto background removal for vector style
		if hasBg && req.Style == "vector" {
			if err := removeBgChromaKey(destPath); err == nil {
				hasBg = false
			} else if err := removeBgRembg(destPath); err == nil {
				hasBg = false
			} else if err := removeBgLights(destPath, lightsAddr); err == nil {
				hasBg = false
			}
		}

		entry := AssetEntry{
			ID:         slug,
			File:       fileName,
			Tags:       []string{"generated", req.Style},
			HasBg:      hasBg,
			Source:     "generated",
			Style:      req.Style,
			Prompt:     styledPrompt,
			Resolution: [2]int{width, height},
			CreatedAt:  time.Now().Format("2006-01-02"),
		}

		idx, err := LoadIndex(assetsDir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		idx.Assets = append(idx.Assets, entry)
		if err := SaveIndex(assetsDir, idx); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entry)
	})

	// HTML Dashboard page
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		html := strings.ReplaceAll(htmlTemplateRaw, "__BACKTICK__", "`")
		w.Write([]byte(html))
	})

	log.Printf("Starting zen-board Asset Console on http://localhost:%d", port)
	return http.ListenAndServe(":"+strconv.Itoa(port), nil)
}

const htmlTemplateRaw = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>zen-board Asset Hub</title>
    <link href="https://fonts.googleapis.com/css2?family=Plus+Jakarta+Sans:wght@300;400;500;600;700&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg-color: #0b0c10;
            --panel-bg: rgba(22, 26, 37, 0.65);
            --border-color: rgba(255, 255, 255, 0.08);
            --text-main: #f3f4f6;
            --text-muted: #9ca3af;
            --accent-glow: rgba(99, 102, 241, 0.15);
            --accent-solid: #6366f1;
            --accent-hover: #4f46e5;
            --accent-emerald: #10b981;
            --accent-amber: #f59e0b;
        }

        * {
            box-sizing: border-box;
            margin: 0;
            padding: 0;
            font-family: 'Plus Jakarta Sans', sans-serif;
        }

        body {
            background-color: var(--bg-color);
            color: var(--text-main);
            min-height: 100vh;
            display: flex;
            flex-direction: column;
            overflow-x: hidden;
        }

        /* Glassmorphism Navigation */
        header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            padding: 1.5rem 4rem;
            border-bottom: 1px solid var(--border-color);
            background: rgba(11, 12, 16, 0.8);
            backdrop-filter: blur(12px);
            position: sticky;
            top: 0;
            z-index: 100;
        }

        .logo-section {
            display: flex;
            align-items: center;
            gap: 0.75rem;
        }

        .logo-icon {
            width: 2rem;
            height: 2rem;
            background: linear-gradient(135deg, #818cf8, #6366f1);
            border-radius: 8px;
            display: flex;
            align-items: center;
            justify-content: center;
            font-weight: 700;
            color: #fff;
            box-shadow: 0 0 15px rgba(99, 102, 241, 0.4);
        }

        .logo-title {
            font-size: 1.25rem;
            font-weight: 700;
            letter-spacing: -0.5px;
            background: linear-gradient(to right, #fff, var(--text-muted));
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
        }

        .nav-actions {
            display: flex;
            gap: 1rem;
        }

        /* Premium Buttons */
        .btn {
            background: var(--accent-solid);
            color: #fff;
            border: none;
            padding: 0.625rem 1.25rem;
            border-radius: 8px;
            font-weight: 600;
            font-size: 0.875rem;
            cursor: pointer;
            transition: all 0.2s ease;
            display: inline-flex;
            align-items: center;
            gap: 0.5rem;
            box-shadow: 0 4px 12px rgba(99, 102, 241, 0.2);
        }

        .btn:hover {
            background: var(--accent-hover);
            transform: translateY(-1px);
        }

        .btn-secondary {
            background: rgba(255, 255, 255, 0.05);
            border: 1px solid var(--border-color);
            color: var(--text-main);
            box-shadow: none;
        }

        .btn-secondary:hover {
            background: rgba(255, 255, 255, 0.1);
            transform: none;
        }

        /* Layout Container */
        .container {
            max-width: 1600px;
            width: 100%;
            margin: 0 auto;
            padding: 2.5rem 4rem;
            flex-grow: 1;
            display: grid;
            grid-template-columns: 320px 1fr;
            gap: 2.5rem;
        }

        /* Sidebar Generator Panel */
        .sidebar {
            background: var(--panel-bg);
            border: 1px solid var(--border-color);
            border-radius: 16px;
            padding: 1.75rem;
            height: fit-content;
            backdrop-filter: blur(10px);
            position: sticky;
            top: 100px;
        }

        .sidebar-title {
            font-size: 1.125rem;
            font-weight: 700;
            margin-bottom: 1.25rem;
            display: flex;
            align-items: center;
            gap: 0.5rem;
        }

        .form-group {
            margin-bottom: 1.25rem;
        }

        .form-label {
            display: block;
            font-size: 0.75rem;
            text-transform: uppercase;
            font-weight: 700;
            color: var(--text-muted);
            margin-bottom: 0.5rem;
            letter-spacing: 0.5px;
        }

        .input-text, .select-input {
            width: 100%;
            background: rgba(0, 0, 0, 0.2);
            border: 1px solid var(--border-color);
            border-radius: 8px;
            padding: 0.75rem;
            color: #fff;
            font-size: 0.875rem;
            outline: none;
            transition: border-color 0.2s ease;
        }

        .input-text:focus, .select-input:focus {
            border-color: var(--accent-solid);
        }

        .input-text::placeholder {
            color: #4b5563;
        }

        /* Main Content Grid */
        main {
            display: flex;
            flex-direction: column;
            gap: 1.75rem;
        }

        .filters-bar {
            display: flex;
            justify-content: space-between;
            align-items: center;
            background: var(--panel-bg);
            border: 1px solid var(--border-color);
            border-radius: 12px;
            padding: 1rem 1.5rem;
            backdrop-filter: blur(10px);
        }

        .tab-group {
            display: flex;
            gap: 0.5rem;
        }

        .tab-btn {
            background: transparent;
            border: none;
            color: var(--text-muted);
            padding: 0.5rem 1rem;
            border-radius: 6px;
            font-weight: 600;
            font-size: 0.875rem;
            cursor: pointer;
            transition: all 0.2s ease;
        }

        .tab-btn:hover {
            color: #fff;
            background: rgba(255, 255, 255, 0.03);
        }

        .tab-btn.active {
            color: #fff;
            background: rgba(255, 255, 255, 0.08);
        }

        .grid {
            display: grid;
            grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
            gap: 1.75rem;
        }

        /* Premium Asset Card */
        .card {
            background: var(--panel-bg);
            border: 1px solid var(--border-color);
            border-radius: 16px;
            overflow: hidden;
            display: flex;
            flex-direction: column;
            transition: all 0.25s cubic-bezier(0.4, 0, 0.2, 1);
            backdrop-filter: blur(10px);
        }

        .card:hover {
            transform: translateY(-4px);
            border-color: rgba(99, 102, 241, 0.3);
            box-shadow: 0 10px 25px -5px rgba(99, 102, 241, 0.1);
        }

        .preview-box {
            height: 180px;
            background-color: #111;
            background-image: 
                linear-gradient(45deg, #181c26 25%, transparent 25%), 
                linear-gradient(-45deg, #181c26 25%, transparent 25%), 
                linear-gradient(45deg, transparent 75%, #181c26 75%), 
                linear-gradient(-45deg, transparent 75%, #181c26 75%);
            background-size: 20px 20px;
            background-position: 0 0, 0 10px, 10px -10px, -10px 0px;
            display: flex;
            align-items: center;
            justify-content: center;
            position: relative;
            border-bottom: 1px solid var(--border-color);
            padding: 1rem;
        }

        .preview-img {
            max-width: 100%;
            max-height: 100%;
            object-fit: contain;
            filter: drop-shadow(0 8px 16px rgba(0,0,0,0.5));
        }

        .card-details {
            padding: 1.25rem;
            display: flex;
            flex-direction: column;
            gap: 0.75rem;
            flex-grow: 1;
        }

        .card-header-row {
            display: flex;
            justify-content: space-between;
            align-items: flex-start;
        }

        .card-title {
            font-size: 1rem;
            font-weight: 700;
            color: #fff;
            overflow: hidden;
            text-overflow: ellipsis;
            white-space: nowrap;
            max-width: 180px;
        }

        .badge {
            font-size: 0.65rem;
            text-transform: uppercase;
            font-weight: 700;
            padding: 0.25rem 0.5rem;
            border-radius: 4px;
            letter-spacing: 0.5px;
        }

        .badge-clean {
            background: rgba(16, 185, 129, 0.1);
            color: var(--accent-emerald);
            border: 1px solid rgba(16, 185, 129, 0.2);
        }

        .badge-needs {
            background: rgba(245, 158, 11, 0.1);
            color: var(--accent-amber);
            border: 1px solid rgba(245, 158, 11, 0.2);
        }

        .card-meta {
            font-size: 0.75rem;
            color: var(--text-muted);
            display: flex;
            flex-direction: column;
            gap: 0.25rem;
        }

        .tags-row {
            display: flex;
            flex-wrap: wrap;
            gap: 0.35rem;
            margin-top: 0.25rem;
        }

        .tag-pill {
            background: rgba(255, 255, 255, 0.05);
            border: 1px solid var(--border-color);
            font-size: 0.65rem;
            padding: 0.15rem 0.45rem;
            border-radius: 4px;
            color: var(--text-muted);
        }

        .card-actions {
            margin-top: auto;
            border-top: 1px solid var(--border-color);
            padding: 1rem 1.25rem;
            display: flex;
            gap: 0.75rem;
            background: rgba(0, 0, 0, 0.05);
        }

        /* Modal Dialog */
        .modal-overlay {
            position: fixed;
            top: 0;
            left: 0;
            right: 0;
            bottom: 0;
            background: rgba(0, 0, 0, 0.75);
            backdrop-filter: blur(8px);
            display: none;
            align-items: center;
            justify-content: center;
            z-index: 1000;
        }

        .modal {
            background: #11141f;
            border: 1px solid var(--border-color);
            border-radius: 20px;
            width: 550px;
            max-width: 90%;
            overflow: hidden;
            box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.5);
        }

        .modal-header {
            padding: 1.5rem 2rem;
            border-bottom: 1px solid var(--border-color);
            display: flex;
            justify-content: space-between;
            align-items: center;
        }

        .modal-title {
            font-size: 1.25rem;
            font-weight: 700;
        }

        .modal-body {
            padding: 2rem;
            display: flex;
            flex-direction: column;
            gap: 1.25rem;
        }

        .modal-footer {
            padding: 1.5rem 2rem;
            border-top: 1px solid var(--border-color);
            display: flex;
            justify-content: flex-end;
            gap: 1rem;
            background: rgba(0, 0, 0, 0.1);
        }

        .close-btn {
            background: transparent;
            border: none;
            color: var(--text-muted);
            font-size: 1.5rem;
            cursor: pointer;
        }

        .close-btn:hover {
            color: #fff;
        }

        /* Spinner / Loading */
        .spinner {
            border: 3px solid rgba(255, 255, 255, 0.1);
            width: 24px;
            height: 24px;
            border-radius: 50%;
            border-left-color: var(--accent-solid);
            animation: spin 1s linear infinite;
            display: inline-block;
            vertical-align: middle;
        }

        @keyframes spin {
            0% { transform: rotate(0deg); }
            100% { transform: rotate(360deg); }
        }
    </style>
</head>
<body>
    <header>
        <div class="logo-section">
            <div class="logo-icon">Z</div>
            <div class="logo-title">zen-board Asset Hub</div>
        </div>
        <div class="nav-actions">
            <button class="btn btn-secondary" onclick="runIndexer()">
                <svg width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path d="M4 4v5h.582m15.356 2A8.001 8.001 0 1121.21 7.89M9 11l3 3 8-8"/></svg>
                Sync Directory Index
            </button>
            <button class="btn" onclick="openBulkGen()">
                <svg width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path d="M12 4v16m8-8H4"/></svg>
                Batch Create
            </button>
        </div>
    </header>

    <div class="container">
        <!-- Sidebar: Quick Generate Panel -->
        <aside class="sidebar">
            <div class="sidebar-title">
                <svg width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24"><path d="M13 10V3L4 14h7v7l9-11h-7z"/></svg>
                Asset Creator
            </div>
            <div class="form-group">
                <label class="form-label">Prompt Description</label>
                <textarea id="gen-prompt" class="input-text" rows="3" placeholder="e.g. business woman with lightbulb"></textarea>
            </div>
            <div class="form-group">
                <label class="form-label">Style Style</label>
                <select id="gen-style" class="select-input">
                    <option value="vector">Whiteboard Sketch (Vector)</option>
                    <option value="realistic">Realistic Photograph (Studio)</option>
                </select>
            </div>
            <button class="btn" style="width: 100%; justify-content: center;" id="gen-btn" onclick="generateAsset()">
                Generate via zen-lights
            </button>
        </aside>

        <!-- Main Content -->
        <main>
            <div class="filters-bar">
                <div class="tab-group">
                    <button class="tab-btn active" id="tab-all" onclick="filterAssets('all')">All Assets</button>
                    <button class="tab-btn" id="tab-needs-bg" onclick="filterAssets('needs-bg')">Needs Bg Removal</button>
                    <button class="tab-btn" id="tab-transparent" onclick="filterAssets('transparent')">Transparent (Ready)</button>
                    <button class="tab-btn" id="tab-vector" onclick="filterAssets('vector')">Vector</button>
                </div>
                <div style="font-size: 0.875rem; color: var(--text-muted);">
                    <span id="filtered-count">0</span> matching assets
                </div>
            </div>

            <!-- Assets Grid -->
            <div class="grid" id="assets-grid">
                <!-- Dynamic cards populated here -->
            </div>
        </main>
    </div>

    <!-- Edit Asset Modal -->
    <div class="modal-overlay" id="edit-modal">
        <div class="modal">
            <div class="modal-header">
                <h3 class="modal-title">Edit Asset Properties</h3>
                <button class="close-btn" onclick="closeModal('edit-modal')">&times;</button>
            </div>
            <div class="modal-body">
                <input type="hidden" id="edit-id">
                <div class="form-group">
                    <label class="form-label">Asset ID</label>
                    <input type="text" id="edit-title-val" class="input-text" disabled>
                </div>
                <div class="form-group">
                    <label class="form-label">Style Preset</label>
                    <select id="edit-style" class="select-input">
                        <option value="vector">Vector</option>
                        <option value="realistic">Realistic</option>
                    </select>
                </div>
                <div class="form-group">
                    <label class="form-label">Tags (comma-separated)</label>
                    <input type="text" id="edit-tags" class="input-text" placeholder="e.g. tech, educational, icon">
                </div>
                <div class="form-group" style="display: flex; align-items: center; gap: 0.5rem;">
                    <input type="checkbox" id="edit-has-bg">
                    <label for="edit-has-bg" style="font-size: 0.875rem; font-weight: 600;">Requires Background Removal (has_bg)</label>
                </div>
            </div>
            <div class="modal-footer">
                <button class="btn btn-secondary" onclick="closeModal('edit-modal')">Cancel</button>
                <button class="btn" onclick="saveAssetProperties()">Save Changes</button>
            </div>
        </div>
    </div>

    <script>
        let allAssets = [];
        let currentFilter = 'all';

        // Load assets on start
        window.addEventListener('DOMContentLoaded', fetchAssets);

        async function fetchAssets() {
            try {
                const res = await fetch('/api/assets');
                const data = await res.json();
                allAssets = data.assets || [];
                renderGrid();
            } catch (err) {
                console.error("Failed to fetch assets", err);
            }
        }

        function renderGrid() {
            const grid = document.getElementById('assets-grid');
            grid.innerHTML = '';

            let filtered = allAssets;
            if (currentFilter === 'needs-bg') {
                filtered = allAssets.filter(a => a.has_bg);
            } else if (currentFilter === 'transparent') {
                filtered = allAssets.filter(a => !a.has_bg);
            } else if (currentFilter === 'vector') {
                filtered = allAssets.filter(a => a.style === 'vector');
            }

            document.getElementById('filtered-count').textContent = filtered.length;

             filtered.forEach(a => {
                const card = document.createElement('div');
                card.className = 'card';
                card.innerHTML = __BACKTICK__
                    <div class="preview-box">
                        <img src="/assets/\${a.file}" class="preview-img" alt="\${a.id}">
                    </div>
                    <div class="card-details">
                        <div class="card-header-row">
                            <span class="card-title" title="\${a.id}">\${a.id}</span>
                            <span class="badge \${a.has_bg ? 'badge-needs' : 'badge-clean'}">
                                \${a.has_bg ? 'has background' : 'transparent'}
                            </span>
                        </div>
                        <div class="card-meta">
                            <span>Res: \${a.resolution[0]}x\${a.resolution[1]} px</span>
                            <span>Source: \${a.source}</span>
                            <div class="tags-row">
                                \${a.tags.map(t => __BACKTICK__<span class="tag-pill">\${t}</span>__BACKTICK__).join('')}
                            </div>
                        </div>
                    </div>
                    <div class="card-actions">
                        <button class="btn btn-secondary" style="flex-grow: 1;" onclick="openEditModal('\${a.id}')">Edit</button>
                        \${a.has_bg ? __BACKTICK__
                            <button class="btn" style="flex-grow: 1;" id="bg-btn-\${a.id}" onclick="cleanBackground('\${a.id}')">Clean BG</button>
                        __BACKTICK__ : ''}
                    </div>
                __BACKTICK__;
                grid.appendChild(card);
            });
        }

        function filterAssets(type) {
            currentFilter = type;
            document.querySelectorAll('.tab-btn').forEach(btn => btn.classList.remove('active'));
            document.getElementById('tab-' + type).classList.add('active');
            renderGrid();
        }

        async function runIndexer() {
            try {
                const res = await fetch('/api/assets/index', { method: 'POST' });
                if (res.ok) {
                    alert("Directory scan complete!");
                    fetchAssets();
                } else {
                    const txt = await res.text();
                    alert("Error during indexing: " + txt);
                }
            } catch (err) {
                alert("Request failed: " + err);
            }
        }

        function openEditModal(id) {
            const asset = allAssets.find(a => a.id === id);
            if (!asset) return;

            document.getElementById('edit-id').value = asset.id;
            document.getElementById('edit-title-val').value = asset.id;
            document.getElementById('edit-style').value = asset.style || 'vector';
            document.getElementById('edit-tags').value = (asset.tags || []).join(', ');
            document.getElementById('edit-has-bg').checked = asset.has_bg;

            document.getElementById('edit-modal').style.display = 'flex';
        }

        function closeModal(id) {
            document.getElementById(id).style.display = 'none';
        }

        async function saveAssetProperties() {
            const id = document.getElementById('edit-id').value;
            const style = document.getElementById('edit-style').value;
            const tags = document.getElementById('edit-tags').value.split(',').map(s => s.trim()).filter(s => s !== '');
            const has_bg = document.getElementById('edit-has-bg').checked;

            try {
                const res = await fetch('/api/assets/update', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ id, style, tags, has_bg })
                });

                if (res.ok) {
                    closeModal('edit-modal');
                    fetchAssets();
                } else {
                    alert("Update failed.");
                }
            } catch (err) {
                alert("Request failed: " + err);
            }
        }

        async function cleanBackground(id) {
            const btn = document.getElementById('bg-btn-' + id);
            const originalText = btn.textContent;
            btn.disabled = true;
            btn.innerHTML = '<span class="spinner"></span> Working';

            try {
                const res = await fetch('/api/assets/process-bg', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ id, backend: 'chromakey' })
                });

                if (res.ok) {
                    fetchAssets();
                } else {
                    const txt = await res.text();
                    alert("Background removal failed: " + txt);
                    btn.disabled = false;
                    btn.textContent = originalText;
                }
            } catch (err) {
                alert("Request failed: " + err);
                btn.disabled = false;
                btn.textContent = originalText;
            }
        }

        async function generateAsset() {
            const promptInput = document.getElementById('gen-prompt');
            const styleSelect = document.getElementById('gen-style');
            const btn = document.getElementById('gen-btn');

            const prompt = promptInput.value.trim();
            const style = styleSelect.value;

            if (!prompt) {
                alert("Please write a prompt.");
                return;
            }

            btn.disabled = true;
            btn.innerHTML = '<span class="spinner"></span> Generating...';

            try {
                const res = await fetch('/api/assets/generate', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ prompt, style })
                });

                if (res.ok) {
                    promptInput.value = '';
                    fetchAssets();
                } else {
                    const txt = await res.text();
                    alert("Generation failed: " + txt);
                }
            } catch (err) {
                alert("Generation request failed: " + err);
            } finally {
                btn.disabled = false;
                btn.textContent = "Generate via zen-lights";
            }
        }

        function openBulkGen() {
            alert("To run batch operations, please run in CLI: zen-board assets generate --prompts path/to/prompts.txt");
        }
    </script>
</body>
</html>
`
