package inspect

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"

	xdraw "golang.org/x/image/draw"

	"zen-board/internal/envelope"
)

// PNGSink writes wanted frames as PNG files, then a manifest and a contact sheet.
// It implements the frame sink contract: it copies every frame it is handed, because
// the collector owns the buffer, and it never starts an encoder process.
type PNGSink struct {
	dir      string
	fps      int
	maxWidth int

	// Frames collected so far, in the order they arrived.
	frames []envelope.Artifact
	// SheetWidth caps the contact sheet so it stays a single tile for a vision model.
	sheetWidth int
	sheetFmt   SheetFormat

	wanted map[int]bool
	labels map[int]string

	scriptHash string
	timing     *Timing
	sheets     []envelope.Artifact
}

// SheetFormat selects the contact-sheet encoding.
type SheetFormat string

// Sheet formats. JPEG is the default: a sheet is the single most-read artifact and
// PNG costs several times the tokens for a vision model.
const (
	SheetJPEG SheetFormat = "jpg"
	SheetPNG  SheetFormat = "png"
	SheetNone SheetFormat = "none"
)

// SheetFormatOf validates a sheet format name.
func SheetFormatOf(v string) (SheetFormat, error) {
	switch SheetFormat(v) {
	case SheetJPEG, SheetPNG, SheetNone:
		return SheetFormat(v), nil
	case "":
		return SheetJPEG, nil
	}
	return "", fmt.Errorf("unknown -sheet value %q: use jpg, png or none", v)
}

// MaxSheetWidth is the widest contact sheet produced. 1568px is a common native tile
// size for vision models, so the sheet is read without further resizing.
const MaxSheetWidth = 1568

// MinSheetTileWidth is the narrowest tile a contact sheet will use.
const MinSheetTileWidth = 256

// the frame sink contract, checked without importing the builder (which would be a
// cycle): the method set must match builder.FrameSink exactly.
var _ interface {
	Wants(int) bool
	WriteFrame(int, *image.RGBA) error
	Close() error
} = (*PNGSink)(nil)

// NewPNGSink prepares the output directory. A stale sheet is removed with the rest
// of the directory contents: a PNG from an older script would otherwise survive a
// run whose hash differs and be read as if it were current.
func NewPNGSink(dir string, fps, maxWidth int, sheet SheetFormat, plan *FramePlan, meta Manifest) (*PNGSink, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("snapshot output dir: %w", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("snapshot output dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !stringsLikeSnapshotArtifact(name) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return nil, fmt.Errorf("clear stale %s: %w", name, err)
		}
	}
	wanted := map[int]bool{}
	labels := map[int]string{}
	if plan != nil {
		for _, f := range plan.Frames {
			wanted[f] = true
			labels[f] = plan.Labels[f]
		}
	}
	return &PNGSink{
		dir: dir, fps: fps, maxWidth: maxWidth, sheetFmt: sheet,
		sheetWidth: MaxSheetWidth, wanted: wanted, labels: labels,
		scriptHash: meta.ScriptHash, timing: meta.Timing,
	}, nil
}

func stringsLikeSnapshotArtifact(name string) bool {
	return (len(name) > 4 && name[len(name)-4:] == ".png") ||
		(len(name) > 4 && name[len(name)-4:] == ".jpg") ||
		name == "manifest.json" || name == "sheet.jpg" || name == "sheet.png"
}

// Wants reports whether the frame plan asked for this frame. The plan was resolved
// before any pixel work started.
func (s *PNGSink) Wants(frame int) bool { return s.wanted[frame] }

// WantsEverything is false: a snapshot always samples.
func (s *PNGSink) WantsEverything() bool { return false }

// WriteFrame copies, optionally downscales, and encodes one frame. The incoming
// buffer is copied immediately: the collector owns it and will recycle it.
func (s *PNGSink) WriteFrame(index int, frame *image.RGBA) error {
	label := ""
	if s.labels != nil {
		label = s.labels[index]
	}
	return s.write(index, frame, label)
}

func (s *PNGSink) write(index int, frame *image.RGBA, label string) error {
	if s.fps <= 0 {
		return fmt.Errorf("PNG sink needs a positive fps")
	}
	copyFrame := copyRGBA(frame)
	if s.maxWidth > 0 && copyFrame.Bounds().Dx() > s.maxWidth {
		copyFrame = downscale(copyFrame, s.maxWidth)
	}
	name := fmt.Sprintf("f%06d_%07.2fs.png", index, float64(index)/float64(s.fps))
	path := filepath.Join(s.dir, name)
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(f, copyFrame); err != nil {
		f.Close()
		return fmt.Errorf("encode %s: %w", name, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}
	s.frames = append(s.frames, envelope.Artifact{
		Path:    path,
		Frame:   index,
		TimeSec: float64(index) / float64(s.fps),
		Kind:    "engine",
		Label:   label,
	})
	return nil
}

// Frames are the artifacts written so far.
func (s *PNGSink) Frames() []envelope.Artifact { return s.frames }

// Close writes manifest.json and the contact sheet. The frame loop owns this call:
// it happens once, after the last frame, so the artifacts the verb reports are already
// on disk by the time the envelope is written.
func (s *PNGSink) Close() error {
	scriptHash, timing := s.scriptHash, s.timing
	manifest := manifestDoc{
		ScriptHash: scriptHash,
		Version:    envelope.Version,
		FPS:        s.fps,
		Frames:     s.frames,
	}
	if timing != nil {
		manifest.Timing = timing
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(s.dir, "manifest.json"), data, 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	if s.sheetFmt == SheetNone || len(s.frames) == 0 {
		return nil
	}
	return s.writeSheet()
}

// SheetArtifacts lists the sheet this sink wrote, if any.
func (s *PNGSink) SheetArtifacts() []envelope.Artifact { return s.sheets }

// Manifest is what a reader needs to trust the PNGs: the script hash they came from
// and the timing their frame numbers are derived from.
type Manifest struct {
	ScriptHash string
	Timing     *Timing
}

type manifestDoc struct {
	ScriptHash string              `json:"script_hash"`
	Version    string              `json:"version"`
	FPS        int                 `json:"fps"`
	Timing     *Timing             `json:"timing,omitempty"`
	Frames     []envelope.Artifact `json:"frames"`
}

// Timing is what the snapshot's frame numbering is derived from, recorded so a
// reader can tell whether a stale cache produced these frames.
type Timing struct {
	DurationSec float64 `json:"duration_sec"`
	TotalFrames int     `json:"total_frames"`
	Source      string  `json:"source"`
}

func (s *PNGSink) writeSheet() error {
	tiles := make([]image.Image, 0, len(s.frames))
	for _, art := range s.frames {
		img, err := decodeFile(art.Path)
		if err != nil {
			return err
		}
		tiles = append(tiles, img)
	}
	sheet := contactSheet(tiles, s.sheetWidth)
	if sheet == nil {
		return nil
	}
	if s.sheetFmt == SheetPNG {
		path := filepath.Join(s.dir, "sheet.png")
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := png.Encode(f, sheet); err != nil {
			return err
		}
		s.sheets = append(s.sheets, envelope.Artifact{Path: path, Kind: "sheet", Label: "contact sheet"})
		return nil
	}
	path := filepath.Join(s.dir, "sheet.jpg")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := jpeg.Encode(f, sheet, &jpeg.Options{Quality: 82}); err != nil {
		return err
	}
	s.sheets = append(s.sheets, envelope.Artifact{Path: path, Kind: "sheet", Label: "contact sheet"})
	return nil
}

// contactSheet tiles frames into a grid that fits the width budget, so one image
// shows the whole run.
func contactSheet(tiles []image.Image, maxWidth int) image.Image {
	if len(tiles) == 0 {
		return nil
	}
	cols := 1
	for cols*cols < len(tiles) {
		cols++
	}
	rows := (len(tiles) + cols - 1) / cols

	// Tiles are the frame's own size when that fits the width budget, and never smaller
	// than MinSheetTileWidth: a grid of 120px slivers is unreadable, and a sheet is
	// only useful if a vision model can actually read the slide text in it.
	tileW := tiles[0].Bounds().Dx()
	if tileW < MinSheetTileWidth {
		tileW = MinSheetTileWidth
	}
	if cols*tileW > maxWidth {
		tileW = maxWidth / cols
	}
	if tileW < 1 {
		tileW = 1
	}
	tileH := tileW * 9 / 16
	if tileH < 1 {
		tileH = 1
	}
	sheet := image.NewRGBA(image.Rect(0, 0, cols*tileW, rows*tileH))
	for y := range sheet.Pix {
		sheet.Pix[y] = 24
	}
	for i, tile := range tiles {
		x := (i % cols) * tileW
		yy := (i / cols) * tileH
		dst := image.Rect(x, yy, x+tileW, yy+tileH)
		xdraw.ApproxBiLinear.Scale(sheet, dst, tile, tile.Bounds(), xdraw.Src, nil)
	}
	return sheet
}

func copyRGBA(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, src.Bounds().Dx(), src.Bounds().Dy()))
	copy(dst.Pix, src.Pix)
	return dst
}

func downscale(src *image.RGBA, maxWidth int) *image.RGBA {
	w := src.Bounds().Dx()
	h := src.Bounds().Dy()
	newW := maxWidth
	newH := h * newW / w
	if newH < 1 {
		newH = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

func decodeFile(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read back %s: %w", path, err)
	}
	defer f.Close()
	if filepath.Ext(path) == ".png" {
		return png.Decode(f)
	}
	return jpeg.Decode(f)
}

// OpaqueColour is used by tests that check a sheet was actually drawn.
func OpaqueColour() color.RGBA { return color.RGBA{R: 24, G: 24, B: 24, A: 255} }
