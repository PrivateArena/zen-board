package assets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type PaintGenRequest struct {
	Prompt string `json:"prompt"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Steps  int    `json:"steps,omitempty"`
}

type PaintGenResponse struct {
	Path string `json:"path"`
}

// BatchGenerate reads prompts from promptsFile, calls zen-lights, saves PNGs to assetsDir, and updates index.json.
func BatchGenerate(assetsDir, promptsFile, style, lightsAddr string) error {
	data, err := os.ReadFile(promptsFile)
	if err != nil {
		return fmt.Errorf("failed to read prompts file: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	var prompts []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		prompts = append(prompts, trimmed)
	}

	if len(prompts) == 0 {
		fmt.Println("No valid prompts found in prompts file.")
		return nil
	}

	fmt.Printf("Batch generating %d assets using style %q via zen-lights at %s...\n", len(prompts), style, lightsAddr)

	idx, err := LoadIndex(assetsDir)
	if err != nil {
		return err
	}

	genDir := filepath.Join(assetsDir, "categories", "generated")
	if err := os.MkdirAll(genDir, 0755); err != nil {
		return fmt.Errorf("failed to create generated assets folder: %w", err)
	}

	for _, p := range prompts {
		slug := cleanSlug(p)
		fileName := filepath.Join("categories", "generated", slug+".png")
		destPath := filepath.Join(assetsDir, fileName)

		// Skip if already generated and exists
		if _, err := os.Stat(destPath); err == nil {
			fmt.Printf("Asset %q already exists. Skipping.\n", slug)
			continue
		}

		// Apply style prompting
		styledPrompt := p
		if style == "vector" {
			styledPrompt = fmt.Sprintf("vector icon of %s, hand-drawn whiteboard sketch, black ink lines, transparent background, educational, simple", p)
		} else if style == "realistic" {
			styledPrompt = fmt.Sprintf("%s, realistic style photograph, studio lighting, isolated on solid white background", p)
		}

		fmt.Printf("Generating %q using prompt: %q\n", slug, styledPrompt)
		err := GenerateSingleAsset(styledPrompt, destPath, lightsAddr)
		if err != nil {
			fmt.Printf("  Generation failed for %q: %v. Skipping.\n", slug, err)
			continue
		}

		// Detect alpha and auto-check dimensions
		hasBg := checkPNGHasBackground(destPath)
		width, height, _ := getImageDimensions(destPath)

		// Create entry
		entry := AssetEntry{
			ID:         slug,
			File:       fileName,
			Tags:       []string{"generated", style},
			HasBg:      hasBg,
			Source:     "generated",
			Style:      style,
			Prompt:     styledPrompt,
			Resolution: [2]int{width, height},
			CreatedAt:  time.Now().Format("2006-01-02"),
		}

		// Try to run auto-background removal if we just generated it and it has a background
		if hasBg && style == "vector" {
			fmt.Printf("  Vector asset has background, attempting background removal...\n")
			// Try lights or rembg
			if err := removeBgRembg(destPath); err == nil {
				entry.HasBg = false
				fmt.Println("  Successfully removed background.")
			} else if err := removeBgLights(destPath, lightsAddr); err == nil {
				entry.HasBg = false
				fmt.Println("  Successfully removed background.")
			} else {
				fmt.Println("  Background removal failed. Flagged as has_bg: true.")
			}
		}

		// Register in index
		idx.Assets = append(idx.Assets, entry)
		if err := SaveIndex(assetsDir, idx); err != nil {
			return err
		}
	}

	return nil
}

func GenerateSingleAsset(prompt, destPath string, lightsAddr string) error {
	reqBody, err := json.Marshal(PaintGenRequest{
		Prompt: prompt,
		Width:  512,
		Height: 512,
		Steps:  4,
	})
	if err != nil {
		return err
	}

	resp, err := http.Post(lightsAddr+"/paint/generate", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("zen-lights API error (%d): %s", resp.StatusCode, string(body))
	}

	var genResp PaintGenResponse
	if err := json.NewDecoder(resp.Body).Decode(&genResp); err != nil {
		return err
	}

	// Copy from generated location to final destination
	src, err := os.Open(genResp.Path)
	if err != nil {
		return fmt.Errorf("failed to open generated temp image: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

var slugRegex = regexp.MustCompile(`[^a-z0-9_]+`)

func cleanSlug(p string) string {
	s := strings.ToLower(p)
	s = slugRegex.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	// limit slug length
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}
