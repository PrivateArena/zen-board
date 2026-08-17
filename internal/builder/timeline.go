package builder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"zen-board/internal/assets"
	"zen-board/internal/model"
	"zen-board/internal/render"
	"zen-board/internal/svg"
)

type TextRenderJob struct {
	AssetID    string
	Content    string
	FontFamily string
	FontSize   float64
	IsBold     bool
	Style      string
}

type GenRenderJob struct {
	AssetID string
	Prompt  string
}

type StyleKeyframe struct {
	Frame int
	Style string
}

type ChapterMarker struct {
	StartTime float64
	Title     string
}

type TimelineCompilation struct {
	Timeline       *model.Timeline
	TextJobs       []TextRenderJob
	GenJobs        []GenRenderJob
	StyleKeyframes []StyleKeyframe
	Chapters       []ChapterMarker
	SubtitleEvents []model.SubtitleEvent
}

// zoomInterval is a camera-transition window in frames during which draw
// reveals must be delayed.
type zoomInterval struct{ start, end int }

// timelineCompiler owns the mutable state threaded through CompileTimeline.
// Each action tag is routed by dispatch (see compile_actions.go) to a dedicated
// handler method that mutates this state.
type timelineCompiler struct {
	conf           *model.Project
	timeline       *model.Timeline
	allWordTimings []model.WordTiming

	textJobs       []TextRenderJob
	textAssetCount int
	genJobs        []GenRenderJob
	genAssetCount  int
	styleKeyframes []StyleKeyframe
	chapters       []ChapterMarker
	subtitleEvents []model.SubtitleEvent

	currentStyle     string
	gridIndex        int
	currentZoomFocus string

	marginX       int
	marginY       int
	colWidth      int
	rowHeight     int
	zoomIntervals []zoomInterval
}

func CompileTimeline(conf *model.Project, allWordTimings []model.WordTiming, pLines []model.ProcessedLine, exactDuration float64, audioTmp string) (*TimelineCompilation, error) {
	c := newTimelineCompiler(conf, allWordTimings, pLines, exactDuration, audioTmp)
	c.compile(pLines)
	if err := c.validateAssets(); err != nil {
		return nil, err
	}
	return c.result(), nil
}

func newTimelineCompiler(conf *model.Project, allWordTimings []model.WordTiming, pLines []model.ProcessedLine, exactDuration float64, audioTmp string) *timelineCompiler {
	marginX := int(float64(conf.Width) * 0.05)
	marginY := int(float64(conf.Height) * 0.05)
	c := &timelineCompiler{
		conf: conf,
		timeline: &model.Timeline{
			Words:     allWordTimings,
			AudioPath: audioTmp,
			Duration:  exactDuration,
		},
		allWordTimings:   allWordTimings,
		currentStyle:     "whiteboard",
		currentZoomFocus: "reset",
		marginX:          marginX,
		marginY:          marginY,
		colWidth:         (conf.Width - 2*marginX) / 3,
		rowHeight:        (conf.Height - 2*marginY) / 2,
	}
	c.collectZoomIntervals(pLines)
	return c
}

// collectZoomIntervals pre-scans lines for zoom transitions so draw reveals can
// be delayed until the camera has settled. Must match renderer.go's
// transitionDuration (1 second).
func (c *timelineCompiler) collectZoomIntervals(pLines []model.ProcessedLine) {
	zoomTransFrames := int(1.0 * float64(c.conf.FPS))
	for _, pl := range pLines {
		for _, action := range pl.Actions {
			if !strings.HasPrefix(action.Tag, "zoom:") {
				continue
			}
			zt := pl.StartTime
			if action.WordIndex > 0 {
				idx := pl.WordOffset + action.WordIndex - 1
				if idx >= 0 && idx < len(c.allWordTimings) {
					zt = c.allWordTimings[idx].Start // match renderer.go: always .Start for zooms
				}
			}
			zf := int(zt * float64(c.conf.FPS))
			c.zoomIntervals = append(c.zoomIntervals, zoomInterval{zf, zf + zoomTransFrames})
		}
	}
}

// adjustForZoom pushes startFrame past any active camera-transition window.
// Handles cascading zooms by re-checking until stable.
func (c *timelineCompiler) adjustForZoom(sf int) int {
	changed := true
	for changed {
		changed = false
		for _, zi := range c.zoomIntervals {
			if sf >= zi.start && sf < zi.end {
				sf = zi.end
				changed = true
				break
			}
		}
	}
	return sf
}

func (c *timelineCompiler) compile(pLines []model.ProcessedLine) {
	for _, pl := range pLines {
		for _, action := range pl.Actions {
			c.processAction(&pl, &action)
		}
	}
}

// processAction computes the values shared by every action handler (trigger
// time, frame bounds, parsed tag/preset) and dispatches to the tag-specific
// handler.
func (c *timelineCompiler) processAction(pl *model.ProcessedLine, action *model.DrawAction) {
	// Find trigger time
	triggerTime := pl.StartTime
	if action.WordIndex > 0 {
		triggerWordIdx := pl.WordOffset + action.WordIndex - 1
		if triggerWordIdx >= 0 && triggerWordIdx < len(c.allWordTimings) {
			if action.TriggerAfterWord {
				triggerTime = c.allWordTimings[triggerWordIdx].End
			} else {
				triggerTime = c.allWordTimings[triggerWordIdx].Start
			}
		} else {
			log.Printf("Warning: WordIndex %d OOB for line starting at %.2fs", action.WordIndex, pl.StartTime)
		}
	}

	rawStartFrame := int(triggerTime * float64(c.conf.FPS))

	// Handle custom duration parameters or defaults
	revealDuration := 2.0
	actionTag := action.Tag
	preset := ""
	if !isSpecialAction(action.Tag) {
		parts := strings.Split(actionTag, ":")
		if len(parts) > 1 {
			if dur, err := strconv.ParseFloat(parts[len(parts)-1], 64); err == nil {
				revealDuration = dur
				parts = parts[:len(parts)-1]
			}
		}
		if len(parts) > 0 {
			actionTag = parts[0]
		}
		if len(parts) > 1 {
			preset = parts[1]
		}
	}

	// For draw/gen/text reveals: delay past any concurrent zoom transition so
	// the hand-draw animation never plays while the camera is still panning.
	// zoom/style/erase/move events keep their raw startFrame.
	startFrame := rawStartFrame
	if !isSpecialAction(action.Tag) || strings.HasPrefix(actionTag, "gen:") || strings.HasPrefix(actionTag, "text:") {
		startFrame = c.adjustForZoom(rawStartFrame)
	}

	c.dispatch(&actionContext{
		pl:             pl,
		action:         action,
		triggerTime:    triggerTime,
		rawStartFrame:  rawStartFrame,
		startFrame:     startFrame,
		endFrame:       startFrame + int(revealDuration*float64(c.conf.FPS)),
		actionTag:      actionTag,
		preset:         preset,
		revealDuration: revealDuration,
	})
}

// validateAssets checks that every referenced asset exists on disk before
// rendering begins.
func (c *timelineCompiler) validateAssets() error {
	assetIndex, _ := assets.LoadIndex(c.conf.AssetsDir)
	assetMap := make(map[string]assets.AssetEntry)
	if assetIndex != nil {
		for _, a := range assetIndex.Assets {
			assetMap[a.ID] = a
		}
	}

	var missingAssets []string
	seenAssets := make(map[string]bool)
	for _, ev := range c.timeline.Events {
		if ev.EventType == "compare" {
			for _, imgID := range []string{ev.CompareLeft, ev.CompareRight} {
				if imgID == "" || seenAssets[imgID] {
					continue
				}
				seenAssets[imgID] = true
				var assetPath string
				if entry, ok := assetMap[imgID]; ok {
					assetPath = filepath.Join(c.conf.AssetsDir, entry.File)
				} else {
					assetPath = filepath.Join(c.conf.AssetsDir, imgID+".png")
				}
				if _, err := os.Stat(assetPath); os.IsNotExist(err) {
					missingAssets = append(missingAssets, imgID)
				}
			}
			continue
		}
		if ev.TargetImage == "" || ev.TargetImage == "clear" || strings.HasPrefix(ev.TargetImage, "__text_") || strings.HasPrefix(ev.TargetImage, "__gen_") || strings.HasPrefix(ev.TargetImage, "__banner_") {
			continue
		}
		if ev.EventType == "arrow" || ev.EventType == "arrow_static" || ev.EventType == "highlight" || ev.EventType == "counter" || ev.EventType == "transition" {
			continue
		}
		if seenAssets[ev.TargetImage] {
			continue
		}
		seenAssets[ev.TargetImage] = true

		var assetPath string
		if entry, ok := assetMap[ev.TargetImage]; ok {
			assetPath = filepath.Join(c.conf.AssetsDir, entry.File)
			if entry.HasBg {
				log.Printf("Warning: Asset %q is marked as having a background (has_bg: true). It is recommended to run background removal processing first.", ev.TargetImage)
			}
		} else {
			assetPath = filepath.Join(c.conf.AssetsDir, ev.TargetImage+".png")
		}

		if _, err := os.Stat(assetPath); os.IsNotExist(err) {
			missingAssets = append(missingAssets, ev.TargetImage)
		}
	}

	if len(missingAssets) > 0 {
		return fmt.Errorf("missing asset files in %s: %s (please make sure they exist as .png files)", c.conf.AssetsDir, strings.Join(missingAssets, ", "))
	}

	for _, ev := range c.timeline.Events {
		if ev.EventType == "erase" {
			if _, hasAsset := assetMap[ev.TargetImage]; !hasAsset {
				assetPath := filepath.Join(c.conf.AssetsDir, ev.TargetImage+".png")
				if _, err := os.Stat(assetPath); os.IsNotExist(err) {
					log.Printf("Warning: [erase:%s] erasing an asset that was not placed on screen; check asset name", ev.TargetImage)
				}
			}
		}
	}

	return nil
}

func (c *timelineCompiler) result() *TimelineCompilation {
	return &TimelineCompilation{
		Timeline:       c.timeline,
		TextJobs:       c.textJobs,
		GenJobs:        c.genJobs,
		StyleKeyframes: c.styleKeyframes,
		Chapters:       c.chapters,
		SubtitleEvents: c.subtitleEvents,
	}
}

func extractCursor(variants map[string]string, defaultCursor string) string {
	if v, ok := variants["cursor"]; ok {
		delete(variants, "cursor")
		return v
	}
	return defaultCursor
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

type PaintGenRequest struct {
	Prompt string `json:"prompt"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Steps  int    `json:"steps,omitempty"`
}

type PaintGenResponse struct {
	Path string `json:"path"`
}

func GeneratePaintAsset(prompt string) (image.Image, error) {
	reqBody, err := json.Marshal(PaintGenRequest{
		Prompt: prompt,
		Width:  512,
		Height: 512,
		Steps:  4,
	})
	if err != nil {
		return nil, err
	}

	resp, err := http.Post("http://localhost:8765/paint/generate", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var genResp PaintGenResponse
	if err := json.Unmarshal(body, &genResp); err != nil {
		return nil, err
	}

	f, err := os.Open(genResp.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	return img, err
}

func PrepareAssets(conf *model.Project, engine *render.Engine, timeline *model.Timeline, textJobs []TextRenderJob, genJobs []GenRenderJob) error {
	assetIndex, _ := assets.LoadIndex(conf.AssetsDir)
	assetMap := make(map[string]assets.AssetEntry)
	if assetIndex != nil {
		for _, a := range assetIndex.Assets {
			assetMap[a.ID] = a
		}
	}

	fmt.Println("Loading assets...")
	seenAssets := make(map[string]bool)
	svgCache := make(map[string]*image.RGBA)

	for _, ev := range timeline.Events {
		if ev.EventType == "compare" {
			for _, imgID := range []string{ev.CompareLeft, ev.CompareRight} {
				if imgID == "" || seenAssets[imgID] {
					continue
				}
				seenAssets[imgID] = true
				var assetPath string
				if entry, ok := assetMap[imgID]; ok {
					if strings.HasSuffix(entry.File, ".svg") {
						continue
					}
					assetPath = filepath.Join(conf.AssetsDir, entry.File)
				} else {
					assetPath = filepath.Join(conf.AssetsDir, imgID+".png")
				}
				err := engine.LoadAsset(imgID, assetPath)
				if err != nil {
					log.Printf("Warning: Could not load compare asset %s: %v", imgID, err)
				}
			}
			continue
		}
		if ev.TargetImage == "" || ev.TargetImage == "clear" || strings.HasPrefix(ev.TargetImage, "__text_") || strings.HasPrefix(ev.TargetImage, "__gen_") || strings.HasPrefix(ev.TargetImage, "__banner_") {
			continue
		}
		if ev.EventType == "arrow" || ev.EventType == "arrow_static" || ev.EventType == "highlight" || ev.EventType == "counter" || ev.EventType == "transition" {
			continue
		}
		if seenAssets[ev.TargetImage] {
			continue
		}
		seenAssets[ev.TargetImage] = true

		var assetPath string
		if entry, ok := assetMap[ev.TargetImage]; ok {
			if strings.HasSuffix(entry.File, ".svg") {
				continue
			}
			assetPath = filepath.Join(conf.AssetsDir, entry.File)
		} else {
			assetPath = filepath.Join(conf.AssetsDir, ev.TargetImage+".png")
		}

		err := engine.LoadAsset(ev.TargetImage, assetPath)
		if err != nil {
			log.Printf("Warning: Could not load asset %s: %v", ev.TargetImage, err)
		}
	}

	// Process SVG assets (both variant and non-variant)
	for i := range timeline.Events {
		ev := &timeline.Events[i]
		if ev.TargetImage == "" || ev.TargetImage == "clear" || strings.HasPrefix(ev.TargetImage, "__") {
			continue
		}
		if ev.EventType == "arrow" || ev.EventType == "arrow_static" || ev.EventType == "highlight" || ev.EventType == "counter" || ev.EventType == "transition" {
			continue
		}

		entry, ok := assetMap[ev.TargetImage]
		if !ok || !strings.HasSuffix(entry.File, ".svg") {
			continue
		}

		fmt.Printf("  SVG event: type=%s target=%s entry=%s w=%d h=%d variants=%v\n",
			ev.EventType, ev.TargetImage, entry.File, ev.Width, ev.Height, ev.AssetVariant)

		rawSVG, err := os.ReadFile(filepath.Join(conf.AssetsDir, entry.File))
		if err != nil {
			return fmt.Errorf("reading svg asset: %w", err)
		}

		key := svg.CacheKey(rawSVG, ev.AssetVariant, ev.Width, ev.Height)
		if cached, ok := svgCache[key]; ok {
			fmt.Printf("  SVG cache hit: %s (type=%s, target=%s)\n", key, ev.EventType, ev.TargetImage)
			engine.RegisterAsset(key, cached)
			ev.TargetImage = key
			continue
		}

		var rasterXML []byte
		if len(ev.AssetVariant) > 0 {
			modifiedXML, err := svg.ModifySVG(rawSVG, ev.AssetVariant)
			if err != nil {
				return fmt.Errorf("modifying svg XML: %w", err)
			}
			rasterXML = modifiedXML
		} else {
			processed, err := svg.PreprocessSVG(rawSVG)
			if err != nil {
				return fmt.Errorf("preprocessing svg: %w", err)
			}
			rasterXML = processed
		}

		if ev.Width <= 0 || ev.Height <= 0 {
			continue
		}

		cfg := svg.RasterConfig{
			MaxDimension: 4096,
		}
		img, err := svg.RasterizeSVG(rasterXML, ev.Width, ev.Height, cfg)
		if err != nil {
			log.Printf("Warning: Could not rasterize SVG %s: %v", ev.TargetImage, err)
			continue
		}

		svgCache[key] = img
		engine.RegisterAsset(key, img)
		ev.TargetImage = key
		fmt.Printf("  SVG registered: %s (%dx%d)\n", key, ev.Width, ev.Height)
	}

	// Render and load all text assets
	for _, job := range textJobs {
		textColor := color.RGBA{R: 0, G: 0, B: 0, A: 255}
		if job.Style == "blackboard" {
			textColor = color.RGBA{R: 255, G: 255, B: 255, A: 255}
		}
		img, err := render.RenderText(job.Content, job.FontFamily, job.FontSize, job.IsBold, textColor)
		if err != nil {
			log.Printf("Warning: failed to render text %q: %v", job.Content, err)
			continue
		}
		engine.RegisterAsset(job.AssetID, img)
	}

	// Generate and load all neural paint assets
	for _, job := range genJobs {
		fmt.Printf("Generating paint asset for prompt %q...\n", job.Prompt)
		img, err := GeneratePaintAsset(job.Prompt)
		if err != nil {
			log.Printf("Warning: Paint generation failed for %q: %v. Using transparent 1x1 placeholder.", job.Prompt, err)
			placeholder := image.NewRGBA(image.Rect(0, 0, 1, 1))
			engine.RegisterAsset(job.AssetID, placeholder)
		} else {
			engine.RegisterAsset(job.AssetID, img)
		}
	}

	return nil
}
