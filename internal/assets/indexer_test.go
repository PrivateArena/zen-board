package assets

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestAutoIndex(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "zen-assets-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create subfolder structure
	techDir := filepath.Join(tempDir, "categories", "technology")
	if err := os.MkdirAll(techDir, 0755); err != nil {
		t.Fatalf("failed to create tech dir: %v", err)
	}

	// 1. Opaque PNG image (has background)
	opaqueImg := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			opaqueImg.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	opaquePath := filepath.Join(techDir, "opaque_robot.png")
	f1, err := os.Create(opaquePath)
	if err != nil {
		t.Fatalf("failed to create opaque_robot.png: %v", err)
	}
	png.Encode(f1, opaqueImg)
	f1.Close()

	// 2. Transparent PNG image (has transparency)
	transImg := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			if x == 5 && y == 5 {
				transImg.Set(x, y, color.RGBA{0, 0, 0, 0}) // Transparent pixel
			} else {
				transImg.Set(x, y, color.RGBA{255, 255, 255, 255})
			}
		}
	}
	transPath := filepath.Join(tempDir, "trans_pyramids.png")
	f2, err := os.Create(transPath)
	if err != nil {
		t.Fatalf("failed to create trans_pyramids.png: %v", err)
	}
	png.Encode(f2, transImg)
	f2.Close()

	// Run AutoIndex
	idx, err := AutoIndex(tempDir)
	if err != nil {
		t.Fatalf("AutoIndex failed: %v", err)
	}

	if len(idx.Assets) != 2 {
		t.Errorf("expected 2 assets, got %d", len(idx.Assets))
	}

	// Find the opaque asset
	var opaqueEntry, transEntry AssetEntry
	for _, a := range idx.Assets {
		if a.ID == "opaque_robot" {
			opaqueEntry = a
		} else if a.ID == "trans_pyramids" {
			transEntry = a
		}
	}

	if opaqueEntry.ID == "" || transEntry.ID == "" {
		t.Fatal("could not find expected entries by ID")
	}

	// Verify categories tag matching
	foundTechTag := false
	for _, tag := range opaqueEntry.Tags {
		if tag == "technology" {
			foundTechTag = true
		}
	}
	if !foundTechTag {
		t.Errorf("expected opaque_robot to have tag 'technology', got %v", opaqueEntry.Tags)
	}

	// Verify background detection
	if !opaqueEntry.HasBg {
		t.Error("expected opaque_robot to have HasBg=true")
	}
	if transEntry.HasBg {
		t.Error("expected trans_pyramids to have HasBg=false")
	}

	// Verify resolution
	if opaqueEntry.Resolution[0] != 10 || opaqueEntry.Resolution[1] != 10 {
		t.Errorf("expected 10x10 resolution, got %v", opaqueEntry.Resolution)
	}
}
