package builder

import (
	"bytes"
	"encoding/json"
	"errors"
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
	"zen-board/internal/constant"
	"zen-board/internal/diag"
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
	// Findings collects compile-time diagnostics (malformed tags, unknown
	// keywords, missing assets). Warnings never block; errors do.
	Findings *diag.Report
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

	// findings accumulates diagnostics raised while compiling this timeline.
	findings *diag.Report
	// idCounters back the deterministic per-compile default id= values.
	idCounters map[string]int
}

// record adds a compile-time finding, attributed to the source line of the
// action that produced it when the parser supplied one.
func (c *timelineCompiler) record(f diag.Finding) {
	if c.findings == nil {
		c.findings = &diag.Report{}
	}
	if f.Stage == "" {
		f.Stage = "compile"
	}
	c.findings.Add(f)
}

// nextID returns the deterministic fallback identity for an event kind, e.g.
// "draw#3". Counters are per-compile, so the same script always yields the
// same ids.
func (c *timelineCompiler) nextID(kind string) string {
	if c.idCounters == nil {
		c.idCounters = map[string]int{}
	}
	c.idCounters[kind]++
	return fmt.Sprintf("%s#%d", kind, c.idCounters[kind])
}

func CompileTimeline(conf *model.Project, allWordTimings []model.WordTiming, pLines []model.ProcessedLine, exactDuration float64, audioTmp string) (*TimelineCompilation, error) {
	c := newTimelineCompiler(conf, allWordTimings, pLines, exactDuration, audioTmp)
	c.compile(pLines)
	if err := c.validateAssets(); err != nil {
		return nil, err
	}
	comp := c.result()
	// Until the check verb owns the presentation layer, keep findings visible on
	// the log channel the pipeline already uses.
	for _, f := range comp.Findings.Sorted() {
		log.Printf("%s", f.LegacyOrString())
	}
	return comp, nil
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
		currentStyle:     constant.STYLE_WHITEBOARD,
		currentZoomFocus: constant.FOCUS_RESET,
		marginX:          marginX,
		marginY:          marginY,
		colWidth:         (conf.Width - 2*marginX) / 3,
		rowHeight:        (conf.Height - 2*marginY) / 2,
		findings:         &diag.Report{},
		idCounters:       map[string]int{},
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
			if !strings.HasPrefix(action.Tag, constant.TAG_ZOOM) {
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
			c.record(diag.Finding{
				Code:     diag.W0004,
				Severity: diag.SeverityWarning,
				Stage:    "compile",
				File:     c.sourceName(action),
				Line:     action.SrcLine,
				Col:      action.SrcCol,
				Snippet:  action.SrcRaw,
				Message:  fmt.Sprintf("trigger word %d is outside the word timings of the line starting at %.2fs", action.WordIndex, pl.StartTime),
				FixHint:  "move the tag inside the sentence so it follows a real word",
			})
		}
	}

	rawStartFrame := int(triggerTime * float64(c.conf.FPS))

	// Handle custom duration parameters or defaults
	revealDuration := 2.0
	actionTag := action.Tag
	preset := ""
	if !isSpecialAction(action.Tag) {
		parts := tagParts(actionTag)
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
	if !isSpecialAction(action.Tag) || strings.HasPrefix(actionTag, constant.TAG_GEN) || strings.HasPrefix(actionTag, constant.TAG_TEXT) {
		startFrame = c.adjustForZoom(rawStartFrame)
	}
	if action.RevealDuration > 0 {
		revealDuration = action.RevealDuration
	}

	ctx := &actionContext{
		pl:             pl,
		action:         action,
		triggerTime:    triggerTime,
		rawStartFrame:  rawStartFrame,
		startFrame:     startFrame,
		endFrame:       startFrame + int(revealDuration*float64(c.conf.FPS)),
		actionTag:      actionTag,
		preset:         preset,
		revealDuration: revealDuration,
	}
	if err := c.dispatch(ctx); err != nil {
		c.recordArity(ctx, err)
	}
	c.stampEvents(ctx)
}

// stampEvents attributes every event this one action produced - the reveal and
// its persistence partner are one logical element - with the action's source
// position, reveal window and identity.
//
// It stamps whatever carries no attribution yet, rather than a slice tail index:
// the cut directives ([clear], [erase:*], [transition:]) rebuild the event slice
// and then append their marker, so an index captured before dispatch can point
// past the events that need stamping.
func (c *timelineCompiler) stampEvents(ctx *actionContext) {
	if ctx.action.SrcLine == 0 && ctx.action.SrcCol == 0 {
		// Compiled without a parsed script (unit tests, programmatic timelines):
		// invent no identity, so the golden event lists stay byte-identical.
		return
	}
	pending := false
	for i := range c.timeline.Events {
		if c.timeline.Events[i].SrcLine == 0 && c.timeline.Events[i].SrcCol == 0 {
			pending = true
			break
		}
	}
	if !pending {
		return // the action emitted nothing
	}
	id := ctx.action.ID
	if id == "" {
		id = c.nextID(ctx.kind())
	}
	for i := range c.timeline.Events {
		ev := &c.timeline.Events[i]
		if ev.SrcLine != 0 || ev.SrcCol != 0 {
			continue
		}
		ev.SrcLine = ctx.action.SrcLine
		ev.SrcCol = ctx.action.SrcCol
		if ev.RevealDuration == 0 {
			ev.RevealDuration = ctx.revealDuration
		}
		if ev.ID == "" {
			ev.ID = id
		}
	}
}

// recordArity turns a handler's errArity into an E0009 finding. Any other
// error class is reported as a generic compile finding.
func (c *timelineCompiler) recordArity(ctx *actionContext, err error) {
	f := diag.Finding{Stage: "compile", File: c.sourceName(ctx.action), Line: ctx.action.SrcLine, Col: ctx.action.SrcCol}
	var ar errArity
	if errors.As(err, &ar) {
		f.Code = diag.E0009
		f.Severity = diag.SeverityOf(diag.E0009)
		f.Message = ar.Error()
		f.FixHint = ar.fixHint
	} else {
		f.Code = diag.E0004
		f.Severity = diag.SeverityOf(diag.E0004)
		f.Message = err.Error()
	}
	f.Snippet = ctx.action.Tag
	c.record(f)
}

// sourceName labels findings with the script the action came from, falling back
// to the configured path while older call sites carry no source metadata.
func (c *timelineCompiler) sourceName(action *model.DrawAction) string {
	if action.SrcPath != "" {
		return action.SrcPath
	}
	return c.conf.ScriptPath
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
		if ev.EventType == constant.EVENT_COMPARE {
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
		if ev.TargetImage == "" || ev.TargetImage == constant.TAG_CLEAR || strings.HasPrefix(ev.TargetImage, "__text_") || strings.HasPrefix(ev.TargetImage, "__gen_") || strings.HasPrefix(ev.TargetImage, "__banner_") || strings.HasPrefix(ev.TargetImage, "__watermark_") {
			continue
		}
		if ev.EventType == constant.EVENT_ARROW || ev.EventType == constant.EVENT_ARROW_STATIC || ev.EventType == constant.EVENT_HIGHLIGHT || ev.EventType == constant.EVENT_COUNTER || ev.EventType == constant.EVENT_TRANSITION {
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
				c.record(assetFinding(diag.W0005, ev,
					fmt.Sprintf("asset %q is indexed with has_bg: true, so its rectangle carries a background", ev.TargetImage),
					"run `zen-board assets process-bg` first"))
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
		if ev.EventType == constant.EVENT_ERASE {
			if _, hasAsset := assetMap[ev.TargetImage]; !hasAsset {
				assetPath := filepath.Join(c.conf.AssetsDir, ev.TargetImage+".png")
				if _, err := os.Stat(assetPath); os.IsNotExist(err) {
					c.record(assetFinding(diag.W0006, ev,
						fmt.Sprintf("[erase:%s] erases an asset that was never placed on screen", ev.TargetImage),
						"check the asset name, or erase the element that is actually on screen"))
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
		Findings:       c.findings,
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

// PrepareAssets loads and rasterises everything the timeline references. Findings
// are accumulated in `findings` when the caller passes one (so `render` can report
// them), and fall back to the log channel when it does not.
func PrepareAssets(conf *model.Project, engine *render.Engine, timeline *model.Timeline, textJobs []TextRenderJob, genJobs []GenRenderJob, findings *diag.Report) error {
	warn := func(f diag.Finding) {
		if findings == nil {
			log.Printf("%s", f.LegacyOrString())
			return
		}
		findings.Add(f)
	}
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
		if ev.EventType == constant.EVENT_COMPARE {
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
				if err := engine.LoadAsset(imgID, assetPath); err != nil {
					warn(diag.Finding{
						Code: diag.W0003, Severity: diag.SeverityWarning, Stage: "prepare",
						Message: fmt.Sprintf("compare asset %q could not be loaded: %v", imgID, err),
						FixHint: "check the file exists and is a decodable image",
					})
				}
			}
			continue
		}
		if ev.TargetImage == "" || ev.TargetImage == constant.TAG_CLEAR || strings.HasPrefix(ev.TargetImage, "__text_") || strings.HasPrefix(ev.TargetImage, "__gen_") || strings.HasPrefix(ev.TargetImage, "__banner_") || strings.HasPrefix(ev.TargetImage, "__watermark_") {
			continue
		}
		if ev.EventType == constant.EVENT_ARROW || ev.EventType == constant.EVENT_ARROW_STATIC || ev.EventType == constant.EVENT_HIGHLIGHT || ev.EventType == constant.EVENT_COUNTER || ev.EventType == constant.EVENT_TRANSITION {
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

		if err := engine.LoadAsset(ev.TargetImage, assetPath); err != nil {
			warn(assetFinding(diag.W0003, ev,
				fmt.Sprintf("asset %q could not be loaded: %v", ev.TargetImage, err),
				"check the file exists and is a decodable image"))
		}
	}

	// Process SVG assets (both variant and non-variant)
	for i := range timeline.Events {
		ev := &timeline.Events[i]
		if ev.TargetImage == "" || ev.TargetImage == constant.TAG_CLEAR || strings.HasPrefix(ev.TargetImage, "__") {
			continue
		}
		if ev.EventType == constant.EVENT_ARROW || ev.EventType == constant.EVENT_ARROW_STATIC || ev.EventType == constant.EVENT_HIGHLIGHT || ev.EventType == constant.EVENT_COUNTER || ev.EventType == constant.EVENT_TRANSITION {
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
			warn(assetFinding(diag.W0003, *ev,
				fmt.Sprintf("SVG asset %q could not be rasterised: %v", ev.TargetImage, err),
				"check the SVG parses and its layout box has a positive size"))
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
		if job.Style == constant.STYLE_BLACKBOARD {
			textColor = color.RGBA{R: 255, G: 255, B: 255, A: 255}
		}
		img, err := render.RenderText(job.Content, job.FontFamily, job.FontSize, job.IsBold, textColor)
		if err != nil {
			warn(diag.Finding{
				Code: diag.W0008, Severity: diag.SeverityWarning, Stage: "prepare",
				Message: fmt.Sprintf("text %q failed to rasterise: %v", job.Content, err),
				FixHint: "check the font preset and that the content is not empty",
			})
			continue
		}
		engine.RegisterAsset(job.AssetID, img)
	}

	// Generate and load all neural paint assets
	for _, job := range genJobs {
		fmt.Printf("Generating paint asset for prompt %q...\n", job.Prompt)
		img, err := GeneratePaintAsset(job.Prompt)
		if err != nil {
			warn(diag.Finding{
				Code: diag.W0007, Severity: diag.SeverityWarning, Stage: "prepare",
				Message: fmt.Sprintf("paint generation failed for %q: %v", job.Prompt, err),
				FixHint: "is zen-lights reachable? a transparent placeholder stands in for this asset",
			})
			placeholder := image.NewRGBA(image.Rect(0, 0, 1, 1))
			engine.RegisterAsset(job.AssetID, placeholder)
		} else {
			engine.RegisterAsset(job.AssetID, img)
		}
	}

	return nil
}

// assetFinding builds a finding attached to an already-compiled event, so a
// preparation-stage problem still points at the line of script that placed it.
func assetFinding(code string, ev model.FrameEvent, message, hint string) diag.Finding {
	return diag.Finding{
		Code:      code,
		Severity:  diag.SeverityOf(code),
		Stage:     "prepare",
		Line:      ev.SrcLine,
		Col:       ev.SrcCol,
		Snippet:   ev.TargetImage,
		Message:   message,
		FixHint:   hint,
		LegacyMsg: "Warning: " + message,
	}
}
