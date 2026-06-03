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
	"sort"
	"strconv"
	"strings"
	"zen-board/internal/assets"
	"zen-board/internal/model"
	"zen-board/internal/render"
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
	Timeline        *model.Timeline
	TextJobs        []TextRenderJob
	GenJobs         []GenRenderJob
	StyleKeyframes  []StyleKeyframe
	Chapters        []ChapterMarker
	SubtitleEvents  []model.SubtitleEvent
}

func CompileTimeline(conf *model.Project, allWordTimings []model.WordTiming, pLines []model.ProcessedLine, exactDuration float64, audioTmp string) (*TimelineCompilation, error) {
	timeline := &model.Timeline{
		Words:     allWordTimings,
		AudioPath: audioTmp,
		Duration:  exactDuration,
	}

	var textJobs []TextRenderJob
	textAssetCount := 0

	var genJobs []GenRenderJob
	genAssetCount := 0

	var styleKeyframes []StyleKeyframe
	var chapters []ChapterMarker
	var subtitleEvents []model.SubtitleEvent

	currentStyle := "whiteboard"
	gridIndex := 0
	marginX := int(float64(conf.Width) * 0.05)
	marginY := int(float64(conf.Height) * 0.05)
	colWidth := (conf.Width - 2*marginX) / 3
	rowHeight := (conf.Height - 2*marginY) / 2

	for _, pl := range pLines {
		for _, action := range pl.Actions {
			// Find trigger time
			triggerTime := pl.StartTime
			if action.WordIndex > 0 {
				triggerWordIdx := pl.WordOffset + action.WordIndex - 1
				if triggerWordIdx >= 0 && triggerWordIdx < len(allWordTimings) {
					if action.TriggerAfterWord {
						triggerTime = allWordTimings[triggerWordIdx].End
					} else {
						triggerTime = allWordTimings[triggerWordIdx].Start
					}
				} else {
					log.Printf("Warning: WordIndex %d OOB for line starting at %.2fs", action.WordIndex, pl.StartTime)
				}
			}

			startFrame := int(triggerTime * float64(conf.FPS))
			
			// Handle custom duration parameters or defaults
			revealDuration := 2.0
			actionTag := action.Tag
			preset := ""
			
			isSpecialPrefix := false
			specialPrefixes := []string{"WAIT:", "zoom:", "style:", "chapter:", "sfx:", "subtitle:", "text:", "erase:", "move:", "gen:"}
			for _, prefix := range specialPrefixes {
				if strings.HasPrefix(actionTag, prefix) {
					isSpecialPrefix = true
					break
				}
			}
			
			if !isSpecialPrefix {
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

			endFrame := startFrame + int(revealDuration*float64(conf.FPS))

			if strings.HasPrefix(action.Tag, "WAIT:") {
				continue
			}

			if strings.HasPrefix(action.Tag, "zoom:") {
				continue
			}

			if strings.HasPrefix(action.Tag, "style:") {
				styleName := strings.TrimPrefix(action.Tag, "style:")
				currentStyle = styleName
				styleKeyframes = append(styleKeyframes, StyleKeyframe{
					Frame: startFrame,
					Style: styleName,
				})
				continue
			}

			if strings.HasPrefix(action.Tag, "chapter:") {
				title := strings.TrimPrefix(action.Tag, "chapter:")
				title = strings.Trim(title, "\"")
				chapters = append(chapters, ChapterMarker{
					StartTime: triggerTime,
					Title:     title,
				})
				continue
			}

			if strings.HasPrefix(action.Tag, "subtitle:") {
				state := strings.TrimPrefix(action.Tag, "subtitle:")
				subtitleEvents = append(subtitleEvents, model.SubtitleEvent{
					Time:  triggerTime,
					State: state,
				})
				continue
			}

			if strings.HasPrefix(action.Tag, "sfx:") {
				continue
			}

			if strings.HasPrefix(action.Tag, "text:") {
				rest := strings.TrimPrefix(action.Tag, "text:")
				firstQuote := strings.Index(rest, "\"")
				lastQuote := strings.LastIndex(rest, "\"")
				if firstQuote != -1 && lastQuote != -1 && lastQuote > firstQuote {
					content := rest[firstQuote+1 : lastQuote]
					remainder := rest[lastQuote+1:]
					
					preset := ""
					fontFamily := "sans"
					fontSize := 48.0
					fontWeight := "regular"
					
					parts := strings.Split(remainder, ":")
					if len(parts) > 1 {
						preset = parts[1]
					}
					if len(parts) > 2 {
						fontFamily = parts[2]
					}
					if len(parts) > 3 {
						if sz, err := strconv.ParseFloat(parts[3], 64); err == nil {
							fontSize = sz
						}
					}
					if len(parts) > 4 {
						fontWeight = parts[4]
					}

					textAssetID := fmt.Sprintf("__text_%d", textAssetCount)
					textAssetCount++

					textJobs = append(textJobs, TextRenderJob{
						AssetID:    textAssetID,
						Content:    content,
						FontFamily: fontFamily,
						FontSize:   fontSize,
						IsBold:     fontWeight == "bold",
						Style:      currentStyle,
					})

					tx, ty, tw, th := action.X, action.Y, action.W, action.H
					if preset != "" && tx == 0 && ty == 0 && tw == 0 && th == 0 {
						px, py, pw, ph := getPresetLayout(preset, conf.Width, conf.Height)
						padW := int(float64(pw) * 0.1)
						padH := int(float64(ph) * 0.1)
						tx = px + padW
						ty = py + padH
						tw = pw - 2*padW
						th = ph - 2*padH
					}

					event := model.FrameEvent{
						TargetImage: textAssetID,
						StartFrame:  startFrame,
						EndFrame:    endFrame,
						X:           tx,
						Y:           ty,
						Width:       tw,
						Height:      th,
						EventType:   "text",
						MaskStyle:   "ltr",
						HandStyle:   "marker",
					}
					timeline.Events = append(timeline.Events, event)
				}
				continue
			}

			if action.Tag == "erase:*" {
				clearFrame := startFrame
				for i := range timeline.Events {
					if timeline.Events[i].EndFrame > clearFrame {
						timeline.Events[i].EndFrame = clearFrame
					}
				}
				gridIndex = 0
				continue
			}

			if strings.HasPrefix(action.Tag, "erase:") {
				targetAsset := strings.TrimPrefix(action.Tag, "erase:")
				eraseEvent := model.FrameEvent{
					TargetImage: targetAsset,
					StartFrame:  startFrame,
					EndFrame:    endFrame,
					EventType:   "erase",
					HandStyle:   "eraser",
					MaskStyle:   "ttb",
				}
				
				for i := len(timeline.Events) - 1; i >= 0; i-- {
					if timeline.Events[i].TargetImage == targetAsset && timeline.Events[i].EventType == "draw" {
						eraseEvent.X = timeline.Events[i].X
						eraseEvent.Y = timeline.Events[i].Y
						eraseEvent.Width = timeline.Events[i].Width
						eraseEvent.Height = timeline.Events[i].Height
						if timeline.Events[i].EndFrame > startFrame {
							timeline.Events[i].EndFrame = startFrame
						}
						break
					}
				}
				timeline.Events = append(timeline.Events, eraseEvent)
				continue
			}

			if strings.HasPrefix(action.Tag, "move:") {
				parts := strings.Split(strings.TrimPrefix(action.Tag, "move:"), ":")
				targetAsset := parts[0]
				destPreset := ""
				if len(parts) > 1 {
					destPreset = parts[1]
				}

				var startX, startY int
				var startW, startH int
				found := false
				for i := len(timeline.Events) - 1; i >= 0; i-- {
					if timeline.Events[i].TargetImage == targetAsset {
						if timeline.Events[i].EventType == "move" {
							startX = timeline.Events[i].DestX
							startY = timeline.Events[i].DestY
						} else {
							startX = timeline.Events[i].X
							startY = timeline.Events[i].Y
						}
						startW = timeline.Events[i].Width
						startH = timeline.Events[i].Height
						found = true
						if timeline.Events[i].EndFrame > startFrame {
							timeline.Events[i].EndFrame = startFrame
						}
						break
					}
				}

				if found {
					destX, destY := startX, startY
					if destPreset != "" {
						px, py, pw, ph := getPresetLayout(destPreset, conf.Width, conf.Height)
						padW := int(float64(pw) * 0.1)
						padH := int(float64(ph) * 0.1)
						destX = px + padW
						destY = py + padH
						startW = pw - 2*padW
						startH = ph - 2*padH
					} else if action.X != 0 || action.Y != 0 {
						destX = action.X
						destY = action.Y
						if action.W != 0 && action.H != 0 {
							startW = action.W
							startH = action.H
						}
					}

					moveEvent := model.FrameEvent{
						TargetImage: targetAsset,
						StartFrame:  startFrame,
						EndFrame:    endFrame,
						EventType:   "move",
						X:           startX,
						Y:           startY,
						Width:       startW,
						Height:      startH,
						DestX:       destX,
						DestY:       destY,
						HandStyle:   "pencil",
					}
					timeline.Events = append(timeline.Events, moveEvent)

					staticDrawEvent := model.FrameEvent{
						TargetImage: targetAsset,
						StartFrame:  endFrame,
						EndFrame:    999999,
						X:           destX,
						Y:           destY,
						Width:       startW,
						Height:      startH,
						EventType:   "draw",
					}
					timeline.Events = append(timeline.Events, staticDrawEvent)
				}
				continue
			}

			if strings.HasPrefix(action.Tag, "gen:") {
				rest := strings.TrimPrefix(action.Tag, "gen:")
				parts := strings.Split(rest, ":")
				prompt := parts[0]
				preset := ""
				if len(parts) > 1 {
					preset = parts[1]
				}

				genAssetID := fmt.Sprintf("__gen_%d", genAssetCount)
				genAssetCount++

				genJobs = append(genJobs, GenRenderJob{
					AssetID: genAssetID,
					Prompt:  prompt,
				})

				tx, ty, tw, th := action.X, action.Y, action.W, action.H
				if preset != "" && tx == 0 && ty == 0 && tw == 0 && th == 0 {
					px, py, pw, ph := getPresetLayout(preset, conf.Width, conf.Height)
					padW := int(float64(pw) * 0.1)
					padH := int(float64(ph) * 0.1)
					tx = px + padW
					ty = py + padH
					tw = pw - 2*padW
					th = ph - 2*padH
				}

				event := model.FrameEvent{
					TargetImage: genAssetID,
					StartFrame:  startFrame,
					EndFrame:    endFrame,
					X:           tx,
					Y:           ty,
					Width:       tw,
					Height:      th,
					EventType:   "draw",
					MaskStyle:   "diagonal",
					HandStyle:   "pencil",
				}
				timeline.Events = append(timeline.Events, event)
				continue
			}

			if action.Tag == "clear" {
				clearFrame := startFrame
				for i := range timeline.Events {
					if timeline.Events[i].EndFrame > clearFrame {
						timeline.Events[i].EndFrame = clearFrame
					}
				}
				gridIndex = 0
				continue
			}

			x, y := action.X, action.Y
			w, h := action.W, action.H

			if preset != "" && x == 0 && y == 0 && w == 0 && h == 0 {
				px, py, pw, ph := getPresetLayout(preset, conf.Width, conf.Height)
				padW := int(float64(pw) * 0.1)
				padH := int(float64(ph) * 0.1)
				w = pw - 2*padW
				h = ph - 2*padH
				x = px + padW
				y = py + padH
			} else if x == 0 && y == 0 {
				col := gridIndex % 3
				row := (gridIndex / 3) % 2
				cellX := marginX + col*colWidth
				cellY := marginY + row*rowHeight

				if w == 0 && h == 0 {
					w = int(float64(colWidth) * 0.8)
					h = int(float64(rowHeight) * 0.8)
				}

				x = cellX + (colWidth-w)/2
				y = cellY + (rowHeight-h)/2
				gridIndex++
			}

			timeline.Events = append(timeline.Events, model.FrameEvent{
				TargetImage: actionTag,
				StartFrame:  startFrame,
				EndFrame:    endFrame,
				X:           x,
				Y:           y,
				Width:       w,
				Height:      h,
				EventType:   "draw",
				MaskStyle:   "ttb",
				HandStyle:   "pencil",
			})
		}
	}

	// Validate Assets exist upfront
	assetIndex, _ := assets.LoadIndex(conf.AssetsDir)
	assetMap := make(map[string]assets.AssetEntry)
	if assetIndex != nil {
		for _, a := range assetIndex.Assets {
			assetMap[a.ID] = a
		}
	}

	var missingAssets []string
	seenAssets := make(map[string]bool)
	for _, ev := range timeline.Events {
		if ev.TargetImage == "clear" || strings.HasPrefix(ev.TargetImage, "__text_") || strings.HasPrefix(ev.TargetImage, "__gen_") {
			continue
		}
		if seenAssets[ev.TargetImage] {
			continue
		}
		seenAssets[ev.TargetImage] = true
		
		var assetPath string
		if entry, ok := assetMap[ev.TargetImage]; ok {
			assetPath = filepath.Join(conf.AssetsDir, entry.File)
			if entry.HasBg {
				log.Printf("Warning: Asset %q is marked as having a background (has_bg: true). It is recommended to run background removal processing first.", ev.TargetImage)
			}
		} else {
			assetPath = filepath.Join(conf.AssetsDir, ev.TargetImage+".png")
		}

		if _, err := os.Stat(assetPath); os.IsNotExist(err) {
			missingAssets = append(missingAssets, ev.TargetImage)
		}
	}

	if len(missingAssets) > 0 {
		return nil, fmt.Errorf("missing asset files in %s: %s (please make sure they exist as .png files)", conf.AssetsDir, strings.Join(missingAssets, ", "))
	}

	return &TimelineCompilation{
		Timeline:       timeline,
		TextJobs:       textJobs,
		GenJobs:        genJobs,
		StyleKeyframes: styleKeyframes,
		Chapters:       chapters,
		SubtitleEvents: subtitleEvents,
	}, nil
}

func getPresetLayout(preset string, canvasW, canvasH int) (x, y, w, h int) {
	halfW := canvasW / 2
	halfH := canvasH / 2
	switch preset {
	case "TL":
		return 0, 0, halfW, halfH
	case "TR":
		return halfW, 0, halfW, halfH
	case "BL":
		return 0, halfH, halfW, halfH
	case "BR":
		return halfW, halfH, halfW, halfH
	case "HT":
		return 0, 0, canvasW, halfH
	case "HB":
		return 0, halfH, canvasW, halfH
	case "LH":
		return 0, 0, halfW, canvasH
	case "RH":
		return halfW, 0, halfW, canvasH
	default:
		return 0, 0, canvasW, canvasH
	}
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

	// Load standard assets
	fmt.Println("Loading assets...")
	seenAssets := make(map[string]bool)
	for _, ev := range timeline.Events {
		if ev.TargetImage == "clear" || strings.HasPrefix(ev.TargetImage, "__text_") || strings.HasPrefix(ev.TargetImage, "__gen_") {
			continue
		}
		if seenAssets[ev.TargetImage] {
			continue
		}
		seenAssets[ev.TargetImage] = true
		
		var assetPath string
		if entry, ok := assetMap[ev.TargetImage]; ok {
			assetPath = filepath.Join(conf.AssetsDir, entry.File)
		} else {
			assetPath = filepath.Join(conf.AssetsDir, ev.TargetImage+".png")
		}

		err := engine.LoadAsset(ev.TargetImage, assetPath)
		if err != nil {
			log.Printf("Warning: Could not load asset %s: %v", ev.TargetImage, err)
		}
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
