package assets

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
)

// ProcessBackgrounds finds all assets where HasBg == true and cleans their background.
func ProcessBackgrounds(assetsDir, backend, lightsAddr string) error {
	idx, err := LoadIndex(assetsDir)
	if err != nil {
		return err
	}

	var unprocessed []AssetEntry
	for _, a := range idx.Assets {
		if a.HasBg {
			unprocessed = append(unprocessed, a)
		}
	}

	if len(unprocessed) == 0 {
		fmt.Println("All assets are already transparent. Nothing to process.")
		return nil
	}

	fmt.Printf("Found %d assets needing background removal using backend %q.\n", len(unprocessed), backend)

	for _, a := range unprocessed {
		path := filepath.Join(assetsDir, a.File)
		fmt.Printf("Processing %s (%s)...\n", a.ID, a.File)

		var err error
		if backend == "zen-lights" {
			err = removeBgLights(path, lightsAddr)
		} else if backend == "rembg" {
			err = removeBgRembg(path)
		} else {
			err = removeBgChromaKey(path)
		}

		if err != nil {
			fmt.Printf("  Error processing %s: %v. Skipping.\n", a.ID, err)
			continue
		}

		// Update entry state
		for i, entry := range idx.Assets {
			if entry.ID == a.ID {
				idx.Assets[i].HasBg = false
				break
			}
		}

		// Save index incrementally to avoid losing progress
		if err := SaveIndex(assetsDir, idx); err != nil {
			return fmt.Errorf("failed to save index: %w", err)
		}
		fmt.Printf("  Successfully cleaned background for %s.\n", a.ID)
	}

	return nil
}

func removeBgRembg(path string) error {
	// check if rembg is in path
	_, err := exec.LookPath("rembg")
	if err != nil {
		return fmt.Errorf("rembg CLI not found in PATH. Please install it using 'pip install rembg'")
	}

	tempOut := path + ".clean.png"
	cmd := exec.Command("rembg", "i", path, tempOut)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rembg error: %w (stderr: %s)", err, errBuf.String())
	}

	// Overwrite original
	if err := os.Rename(tempOut, path); err != nil {
		return fmt.Errorf("failed to overwrite asset file: %w", err)
	}
	return nil
}

func removeBgLights(path string, lightsAddr string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("image", filepath.Base(path))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, file); err != nil {
		return err
	}
	writer.Close()

	req, err := http.NewRequest("POST", lightsAddr+"/paint/remove-bg", body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("zen-lights background removal failed (%d): %s", resp.StatusCode, string(respBody))
	}

	// Write clean image back
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// removeBgChromaKey performs a native Go color-key background removal.
// It detects near-white pixels (threshold >= 230) and sets their alpha with a soft blend factor.
func removeBgChromaKey(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}

	img, _, err := image.Decode(file)
	file.Close() // Close early so we can overwrite it
	if err != nil {
		return fmt.Errorf("failed to decode image: %w", err)
	}

	bounds := img.Bounds()
	newImg := image.NewRGBA(bounds)

	const threshold = 230

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.At(x, y)
			r, g, b, a := c.RGBA()
			
			// Convert 16-bit color channel to 8-bit
			r8 := uint8(r >> 8)
			g8 := uint8(g >> 8)
			b8 := uint8(b >> 8)
			a8 := uint8(a >> 8)

			brightness := (int(r8) + int(g8) + int(b8)) / 3
			if brightness >= threshold {
				// Soft transparency transition between threshold and 255
				factor := float64(255-brightness) / float64(255-threshold)
				newA := uint8(float64(a8) * factor)
				newImg.Set(x, y, color.RGBA{R: r8, G: g8, B: b8, A: newA})
			} else {
				newImg.Set(x, y, color.RGBA{R: r8, G: g8, B: b8, A: a8})
			}
		}
	}

	outFile, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	if err := png.Encode(outFile, newImg); err != nil {
		return fmt.Errorf("failed to encode PNG: %w", err)
	}

	return nil
}

