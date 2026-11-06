package model

import "zen-board/internal/constant"

type Project struct {
	ScriptPath        string  `json:"script_path"`
	AssetsDir         string  `json:"assets_dir"`
	OutputPath        string  `json:"output_path"`
	FPS               int     `json:"fps"`
	Width             int     `json:"width"`
	Height            int     `json:"height"`
	TTSAddr           string  `json:"tts_addr"`
	Speed             float64 `json:"speed"`
	Cursor            string  `json:"cursor"`
	HandTipX          int     `json:"hand_tip_x"`
	HandTipY          int     `json:"hand_tip_y"`
	Voice             string  `json:"voice"`
	DisableTranscript bool    `json:"disable_transcript"`
	BGMPath           string  `json:"bgm_path"`
	BGMVolume         float64 `json:"bgm_volume"`
	CameraEnabled     bool    `json:"camera_enabled"`
	FreezeFrames      int     `json:"freeze_frames"`
	TTSCacheDir       string  `json:"tts_cache_dir"`
	TTSConcurrency    int     `json:"tts_concurrency"`
}

func NewDefaultProject() *Project {
	return &Project{
		AssetsDir:      "./assets",
		OutputPath:     "output.mp4",
		FPS:            30,
		Width:          1920,
		Height:         1080,
		Cursor:         constant.CURSOR_HAND,
		TTSAddr:        "http://localhost:5000",
		Speed:          1.0,
		HandTipX:       30,
		HandTipY:       20,
		Voice:          "am_adam",
		BGMVolume:      0.05,
		FreezeFrames:   60,
		TTSCacheDir:    "/tmp/zen-board/tts",
		TTSConcurrency: constant.TTS_MAX_CONCURRENCY,
	}
}

type ScriptLine struct {
	Text    string       // Clean text for TTS
	Actions []DrawAction // Embedded draw commands
}

type DrawAction struct {
	Tag              string // e.g. "king_death"
	WordIndex        int    // Trigger after this word finishes
	ImagePath        string // Resolved path to asset PNG
	X, Y             int
	W, H             int
	RevealDuration   float64           // custom duration in seconds
	TriggerAfterWord bool              // trigger after word end instead of start
	GenPrompt        string            // prompt for image generation
	AssetVariant     map[string]string // SVG variant modifications: key=value pairs
	// ID is the explicit id= identity from the tag; empty means the compiler
	// derives a deterministic per-compile identity. Never stored in AssetVariant:
	// that map is aliased between events and mutated during compile.
	ID string
	// Source location of the tag in the .zen file. Offsets are byte offsets into
	// the raw script (SrcOffset/SrcLength); Line/Col are 1-based and are what an
	// editor shows. All zero for actions built programmatically. Identity is
	// carried as strings/ints only - never inside AssetVariant, which is aliased
	// across events and mutated by extractCursor.
	SrcPath   string
	SrcOffset int
	SrcLength int
	SrcLine   int
	SrcCol    int
	SrcRaw    string
}

type WordTiming struct {
	Word  string
	Start float64 // seconds
	End   float64 // seconds
}

type FrameEvent struct {
	TargetImage   string
	StartFrame    int
	EndFrame      int
	X, Y          int    // Position on canvas
	Width, Height int    // Render dimensions
	EventType     string // "draw", "erase", "move", "text", "static", "slide", "banner", "overlay"
	MaskStyle     string // "diagonal", "ltr", "ttb"
	HandStyle     string // "pencil", "chalk", "eraser", "marker"
	Cursor        string // "hand", "magic_wand", "two_finger", ... ; empty defaults to "hand"
	DestX, DestY  int    // destination for "move" events
	ZoomFocus     string `json:"zoom_focus"`
	// Identity and provenance. ID is set at compile time: id= from the tag, else a
	// deterministic "<kind>#<n>" per compile. It is what @refs, [clear] survival and
	// character state switching address. SrcLine/SrcCol point at the tag that
	// produced the event. RevealDuration is the authored reveal window in seconds.
	ID             string  `json:"id,omitempty"`
	SrcLine        int     `json:"src_line,omitempty"`
	SrcCol         int     `json:"src_col,omitempty"`
	RevealDuration float64 `json:"reveal_duration,omitempty"`
	// Layer is the draw order and camera behaviour class: "board" (default, empty
	// string means the same) participates in the camera visibility test; "overlay" is
	// drawn above everything and is deliberately camera-independent, which is what a
	// mascot or a caption needs to survive a [zoom:] pointed elsewhere.
	Layer string
	// Sticky marks a layer that survives the board-wipe directives ([clear],
	// [erase:all], [transition:]) - the cut loops rewrite every earlier event's
	// EndFrame today, which is why an overlay cannot outlive a scene change.
	Sticky bool
	// ExitFrames is the length of the exit animation this event spawned, if any. An
	// exit is a second event rather than a longer window because the cut loops rewrite
	// EndFrame retroactively and would swallow it.
	ExitFrames int
	// ExitKind is the animation shape: "fade", "pop" or "slide". Only meaningful on
	// the event whose ExitFrames is non-zero.
	ExitKind string
	// Slide-specific fields
	Transition string // "none","fade","pop","slide-left","slide-right","slide-up","slide-down"
	FitMode    string // "fit","fill","stretch","native"
	// Align positions a pixel-exact raster inside its box.
	Align string
	// Extended media fields
	Opacity        float64
	ColorHex       string
	ArrowFrom      string
	ArrowTo        string
	ArrowStyle     string
	HighlightStyle string
	CompareLeft    string
	CompareRight   string
	LabelLeft      string
	LabelRight     string
	CounterStart   float64
	CounterEnd     float64
	CounterFormat  string
	TransitionType string
	// SVG variant modifications: key=value pairs for dynamic asset customization
	AssetVariant map[string]string
}

type SubtitleEvent struct {
	Time  float64
	State string // "top", "bottom", "off"
}

type Timeline struct {
	Events    []FrameEvent
	Words     []WordTiming
	AudioPath string
	Duration  float64 // total seconds
}

type ProcessedLine struct {
	StartTime  float64
	Duration   float64
	WordOffset int
	Actions    []DrawAction
}
