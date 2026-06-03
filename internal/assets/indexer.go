package assets

import (
	"encoding/json"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type AssetEntry struct {
	ID         string    `json:"id"`
	File       string    `json:"file"`
	Tags       []string  `json:"tags"`
	HasBg      bool      `json:"has_bg"`
	Source     string    `json:"source"` // "manual", "generated", "imported-svg", "imported-png"
	Style      string    `json:"style"`  // "vector", "realistic"
	Prompt     string    `json:"prompt"`
	Resolution [2]int    `json:"resolution"`
	CreatedAt  string    `json:"created_at"`
}

type AssetIndex struct {
	Version int          `json:"version"`
	Assets  []AssetEntry `json:"assets"`
}

// LoadIndex loads assets/index.json from assetsDir
func LoadIndex(assetsDir string) (*AssetIndex, error) {
	path := filepath.Join(assetsDir, "index.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &AssetIndex{Version: 1, Assets: []AssetEntry{}}, nil
		}
		return nil, err
	}
	var idx AssetIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	return &idx, nil
}

// SaveIndex saves AssetIndex to assetsDir
func SaveIndex(assetsDir string, idx *AssetIndex) error {
	path := filepath.Join(assetsDir, "index.json")
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// AutoIndex scans assetsDir, updates index.json, and returns the updated index.
func AutoIndex(assetsDir string) (*AssetIndex, error) {
	currentIdx, err := LoadIndex(assetsDir)
	if err != nil {
		return nil, err
	}

	existingMap := make(map[string]AssetEntry)
	for _, a := range currentIdx.Assets {
		existingMap[a.File] = a
	}

	var newAssets []AssetEntry

	err = filepath.Walk(assetsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(assetsDir, path)
		if err != nil {
			return err
		}

		// Skip index.json, hand.png, and hidden files/folders
		if rel == "index.json" || rel == "hand.png" || strings.HasPrefix(info.Name(), ".") {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(rel))
		if ext != ".png" && ext != ".svg" {
			return nil
		}

		id := strings.TrimSuffix(info.Name(), filepath.Ext(info.Name()))

		// Determine tags from path segments
		dir := filepath.Dir(rel)
		var tags []string
		if dir != "." {
			parts := strings.Split(dir, string(filepath.Separator))
			for _, p := range parts {
				if p != "" && p != "categories" {
					tags = append(tags, strings.ToLower(p))
				}
			}
		}

		// Keep existing entry if present, but update tags/resolution
		entry, exists := existingMap[rel]
		if !exists {
			entry = AssetEntry{
				ID:        id,
				File:      rel,
				Source:    "manual",
				Style:     "vector",
				CreatedAt: time.Now().Format("2006-01-02"),
			}
			if ext == ".svg" {
				entry.HasBg = false
				entry.Source = "imported-svg"
				entry.Style = "vector"
			} else {
				// Detect if PNG has background
				entry.HasBg = checkPNGHasBackground(path)
				entry.Source = "imported-png"
			}
		}

		// Always update resolution if we can read it
		if w, h, err := getImageDimensions(path); err == nil {
			entry.Resolution = [2]int{w, h}
		}

		// Merge tags
		tagMap := make(map[string]bool)
		for _, t := range tags {
			tagMap[t] = true
		}
		for _, t := range entry.Tags {
			tagMap[t] = true
		}
		var finalTags []string
		for t := range tagMap {
			finalTags = append(finalTags, t)
		}
		entry.Tags = finalTags

		newAssets = append(newAssets, entry)
		return nil
	})

	if err != nil {
		return nil, err
	}

	currentIdx.Assets = newAssets
	err = SaveIndex(assetsDir, currentIdx)
	return currentIdx, err
}

func getImageDimensions(path string) (int, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()

	img, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0, err
	}
	return img.Width, img.Height, nil
}

func checkPNGHasBackground(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return true // safe default
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return true
	}

	// Read alpha channel of pixels. If any pixel has alpha < 255, we assume it's transparent.
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a < 65535 { // 16-bit alpha representation in Go color.Color
				return false
			}
		}
	}
	return true
}
