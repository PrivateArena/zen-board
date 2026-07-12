# Architectural Blueprint: SVG Asset Pipeline & Variant Modification

This document defines the production design, objectives, and implementation phases to transition Zen-Board to SVG as the primary asset format. It addresses engineering constraints, security requirements, and concurrency concerns identified in the Red-Team review.

---

## 1. Objectives & Goals

- **Resolution Independence & Fidelity**: Prevent pixelation and quality loss due to raster scaling by rasterizing vector sources directly at target layout coordinates and output scale.
- **Dynamic asset variation**: Enable script-level customization of character and environment variables (e.g., skin color, hair color, clothing style, accessories) from high-quality modular templates (Open Peeps, Humaaans).
- **Environment Determinism**: Guarantee pixel-exact rendering reproducibility across developer local systems, headless CI pipelines, and production servers.
- **Zero Runtime Overhead**: Confine heavy XML parsing and vector rasterization to the sequential preparation phase (`PrepareAssets`), keeping the hot multi-threaded rendering loop (`RenderFrame`) locked to fast, concurrent map-lookup operations.

---

## 2. Zen-Board Grammar & Script Specification

We extend the tag parsing schema in `internal/script/parser.go` to support dynamic key-value properties.

### Grammar Syntax
```
[draw:<asset_id>:<key>=<value>[:<key>=<value>...][@<coords>]]
```
- `<asset_id>`: The logical file ID mapped in `index.json` (e.g., `peep_pointing`).
- `<key>=<value>`: Variant color or sub-group variables. Colors are specified as 3 or 6-digit hexadecimal strings.
- `<coords>`: Target layout configuration `x,y,w,h` (optional).

### Examples
- **Basic SVG Asset**: `[draw:peep_pointing@100,200,300,400]`
- **Customized Variant**: `[draw:peep_pointing:skin=#ffcc99:hair=#4a2a1b:shirt=#3c8dbc@100,200,300,400]`
- **Modular Component Swapping**: `[draw:peep_pointing:skin=#ffcc99:arms=wrench:expression=shocked@100,200,300,400]`

---

## 3. SVG Registry & Asset Indexing

The `internal/assets/indexer.go` structure is expanded. `AssetEntry` will support parsing and caching SVG variant capacities.

```go
type AssetEntry struct {
	ID         string    `json:"id"`
	File       string    `json:"file"`
	Tags       []string  `json:"tags"`
	HasBg      bool      `json:"has_bg"`
	Source     string    `json:"source"`       // "imported-svg" or "imported-png"
	Style      string    `json:"style"`        // "vector" or "realistic"
	Resolution [2]int    `json:"resolution"`   // Native SVG viewBox width/height
	CreatedAt  string    `json:"created_at"`
}
```

---

## 4. SVG Mutation Engine (`internal/svg/`)

We introduce a dedicated `internal/svg` package. It manages XML parsing and node modification in memory.

### Go API Signatures (`internal/svg/edit.go`)
```go
package svg

import (
	"fmt"
	"strings"
	"github.com/beevik/etree"
)

type Variant map[string]string

// ModifySVG parses the raw SVG XML, applies the style map variables, 
// and returns the modified XML payload.
func ModifySVG(rawXML []byte, variants Variant) ([]byte, error) {
	doc := etree.NewDocument()
	
	// XXE mitigation: disable external entities in DOM parsing
	doc.ReadSettings = etree.ReadSettings{
		Entity: func(name string) string { return "" },
	}
	
	if err := doc.ReadFromBytes(rawXML); err != nil {
		return nil, fmt.Errorf("parsing xml document: %w", err)
	}

	// 1. Process inline attributes (elements styled by ID or Class)
	for key, val := range variants {
		// Verify parameters to block XML Injection
		sanitizedVal := sanitizeValue(val)
		
		// Find elements carrying id="key"
		// E.g., <g id="skin"> or <path id="skin-layer">
		elements := doc.FindElements(fmt.Sprintf("//*[@id='%s']", key))
		for _, el := range elements {
			if el.SelectAttr("fill") != nil {
				el.CreateAttr("fill", sanitizedVal)
			}
			// Apply fill attribute to children if parent is a group container
			for _, child := range el.FindElements(".//") {
				if child.SelectAttr("fill") != nil {
					child.CreateAttr("fill", sanitizedVal)
				}
			}
		}
	}

	// 2. Process embedded CSS stylesheet blocks if they define class overrides
	if styleEl := doc.FindElement("//style"); styleEl != nil {
		cssText := styleEl.Text()
		for key, val := range variants {
			sanitizedVal := sanitizeValue(val)
			// Replace class definitions: E.g., ".skin { fill: #000; }" -> ".skin { fill: #val; }"
			cssText = replaceCSSClassFill(cssText, key, sanitizedVal)
		}
		styleEl.SetText(cssText)
	}

	doc.Indent(0) // Minimize spacing/indentation bloat
	return doc.WriteToBytes()
}

func sanitizeValue(v string) string {
	// Restrict to safe hex colors (#fff, #ffffff) or basic alphanumeric tokens (component names)
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "#") {
		hex := v[1:]
		if len(hex) == 3 || len(hex) == 6 {
			return v
		}
	}
	// Alphanumeric sanitization for modular group IDs
	var out strings.Builder
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func replaceCSSClassFill(css, className, fillHex string) string {
	// Simple CSS parser to substitute colors matching class names
	// E.g., replaces fill values inside .skin { ... }
	targetPattern := "." + className
	idx := strings.Index(css, targetPattern)
	if idx == -1 {
		return css
	}
	// Find curly braces
	startBrace := strings.Index(css[idx:], "{")
	if startBrace == -1 {
		return css
	}
	endBrace := strings.Index(css[idx+startBrace:], "}")
	if endBrace == -1 {
		return css
	}
	
	body := css[idx+startBrace : idx+startBrace+endBrace]
	// Locate fill declaration
	fillIdx := strings.Index(body, "fill:")
	if fillIdx == -1 {
		return css
	}
	// Extract the actual fill color declaration up to semicolon
	semiIdx := strings.Index(body[fillIdx:], ";")
	if semiIdx == -1 {
		return css
	}
	
	oldFill := body[fillIdx : fillIdx+semiIdx]
	newFill := "fill: " + fillHex
	
	return strings.Replace(css, oldFill, newFill, 1)
}
```

---

## 5. Process-Bounded SVG Rasterization

SVG rasterization executes the native `resvg` CLI utility using a strict process pool and timeout handler. 

### Go API Signatures (`internal/svg/render.go`)
```go
package svg

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	"image/draw"
	_ "image/png"
	"os/exec"
	"runtime"
	"time"
)

var (
	// Bound total concurrent resvg processes to CPU thread count
	processSemaphore = make(chan struct{}, runtime.NumCPU())
)

type RasterConfig struct {
	MaxDimension int           // Clamp dimensions to mitigate decompression bombs (default: 4096)
	CLIPath      string        // Path to resvg binary
	Timeout      time.Duration // Maximum process execution time
}

// RasterizeSVG executes the resvg CLI tool using process boundaries
func RasterizeSVG(ctx context.Context, svgXML []byte, w, h int, cfg RasterConfig) (*image.RGBA, error) {
	if w > cfg.MaxDimension || h > cfg.MaxDimension {
		return nil, fmt.Errorf("requested layout size (%dx%d) exceeds maximum dimensions (%d)", w, h, cfg.MaxDimension)
	}
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("invalid layout coordinates (%dx%d)", w, h)
	}

	// 1. Acquire process execution token (blocks if NumCPU active)
	select {
	case processSemaphore <- struct{}{}:
		defer func() { <-processSemaphore }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// 2. Set strict command timeout context
	execCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	// 3. Assemble subprocess arguments safely (No shell wrappers)
	cmd := exec.CommandContext(execCtx, cfg.CLIPath,
		"--width", fmt.Sprintf("%d", w),
		"--height", fmt.Sprintf("%d", h),
		"-", "-", // Direct stdin to stdout piping
	)

	cmd.Stdin = bytes.NewReader(svgXML)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	// 4. Run process
	if err := cmd.Run(); err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("resvg rasterization timed out: %w", execCtx.Err())
		}
		return nil, fmt.Errorf("resvg failed: %v (stderr: %q)", err, stderrBuf.String())
	}

	// 5. Decode output stream
	img, _, err := image.Decode(&stdoutBuf)
	if err != nil {
		return nil, fmt.Errorf("decoding output bitmap: %w", err)
	}

	// 6. Normalize output image pointer to *image.RGBA
	rgba, ok := img.(*image.RGBA)
	if !ok {
		rgba = image.NewRGBA(img.Bounds())
		draw.Draw(rgba, rgba.Bounds(), img, image.Point{}, draw.Src)
	}

	return rgba, nil
}

// CacheKey generates a secure variant/size hash
func CacheKey(rawXML []byte, variants Variant, w, h int) string {
	hasher := sha256.New()
	hasher.Write(rawXML)
	for k, v := range variants {
		hasher.Write([]byte(fmt.Sprintf("%s:%s", k, v)))
	}
	return fmt.Sprintf("%x_%d_%d", hasher.Sum(nil), w, h)
}
```

---

## 6. Integration Architecture

### Timeline Parsing Integration (`internal/builder/timeline.go`)
During visual timeline compilation, tags like `peep_pointing:skin=#ffcc99` are parsed. The timeline events carry raw metadata:

- TargetImage contains the base asset name (e.g., `peep_pointing`).
- A new `AssetVariant` property (type `map[string]string`) is added to the `FrameEvent` struct to store dynamic styles.

### Extend `PrepareAssets` pipeline
In `builder.PrepareAssets`, we intercept SVG formats, modify their properties, compile them using the process-bounded rasterizer, and cache the bitmaps under the generated variant hash.

```go
func PrepareAssets(conf *model.Project, engine *render.Engine, timeline *model.Timeline, textJobs []TextRenderJob, genJobs []GenRenderJob) error {
	// 1. Index available resources
	assetIndex, _ := assets.LoadIndex(conf.AssetsDir)
	assetMap := make(map[string]assets.AssetEntry)
	if assetIndex != nil {
		for _, a := range assetIndex.Assets {
			assetMap[a.ID] = a
		}
	}

	// 2. Iterate through events in the timeline
	seenVariants := make(map[string]bool)
	for _, ev := range timeline.Events {
		if ev.TargetImage == "" || strings.HasPrefix(ev.TargetImage, "__") {
			continue
		}

		entry, ok := assetMap[ev.TargetImage]
		if !ok || !strings.HasSuffix(entry.File, ".svg") {
			continue // Handle fallback standard PNG loading
		}

		// Calculate cache key based on asset path + dynamic variants + output dimensions
		rawSVG, err := os.ReadFile(filepath.Join(conf.AssetsDir, entry.File))
		if err != nil {
			return fmt.Errorf("reading svg asset: %w", err)
		}

		// Calculate key based on modifications
		key := svg.CacheKey(rawSVG, ev.AssetVariant, ev.Width, ev.Height)
		if seenVariants[key] {
			// Update the timeline event target to point directly to the cached variant
			ev.TargetImage = key
			continue
		}
		seenVariants[key] = true

		// Apply modifications in memory
		modifiedXML, err := svg.ModifySVG(rawSVG, ev.AssetVariant)
		if err != nil {
			return fmt.Errorf("modifying svg XML: %w", err)
		}

		// Rasterize direct to target width/height
		cfg := svg.RasterConfig{
			MaxDimension: 4096,
			CLIPath:      "resvg",
			Timeout:      2 * time.Second,
		}
		img, err := svg.RasterizeSVG(context.Background(), modifiedXML, ev.Width, ev.Height, cfg)
		if err != nil {
			return fmt.Errorf("rasterizing svg: %w", err)
		}

		// Cache direct to Engine Assets map under the hash
		engine.RegisterAsset(key, img)

		// Point timeline event targeting to the cached variant ID
		ev.TargetImage = key
	}

	// ... continue standard PNG and text asset parsing ...
	return nil
}
```

---

## 7. Implementation Checklist & Target Phases

### Phase 1: Library & Dependencies Init
- [ ] Install `resvg` on target CI and local workstations. Add executable checks in `main.go`.
- [ ] Add `github.com/beevik/etree` to `go.mod`.

### Phase 2: Parser & Model Updates
- [ ] Update `model.DrawAction` and `model.FrameEvent` to include the `AssetVariant map[string]string` field.
- [ ] Update parsing regex and text extraction loop in `internal/script/parser.go` to isolate trailing variant parameters from the base asset ID.

### Phase 3: Core SVG Mutation & Rasterization
- [ ] Implement `internal/svg/edit.go` with XXE entity constraints and CSS variable replacements.
- [ ] Implement `internal/svg/render.go` using a bounded semaphore channel. Include context timeout handling.

### Phase 4: PrepareAssets Refactoring
- [ ] Update `builder.PrepareAssets` in `internal/builder/timeline.go` to scan frame event variants.
- [ ] Implement hash-based asset variant caching keys to deduplicate rasterizer subprocess executions.

### Phase 5: Verification & Golden Testing
- [ ] Write integration test verifying that dynamic skin modifications map correctly to DOM fill tags.
- [ ] Implement golden-image regression test pipeline. Compare SVG layouts generated inside tests against static golden references to ensure rendering determinism.
