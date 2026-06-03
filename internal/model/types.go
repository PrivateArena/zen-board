package model

type Project struct {
	ScriptPath        string  `json:"script_path"`
	AssetsDir         string  `json:"assets_dir"`
	OutputPath        string  `json:"output_path"`
	FPS               int     `json:"fps"`
	Width             int     `json:"width"`
	Height            int     `json:"height"`
	TTSAddr           string  `json:"tts_addr"`
	Speed             float64 `json:"speed"`
	HandTipX          int     `json:"hand_tip_x"`
	HandTipY          int     `json:"hand_tip_y"`
	Voice             string  `json:"voice"`
	DisableTranscript bool    `json:"disable_transcript"`
	BGMPath           string  `json:"bgm_path"`
	BGMVolume         float64 `json:"bgm_volume"`
	CameraEnabled     bool    `json:"camera_enabled"`
	FreezeFrames      int     `json:"freeze_frames"`
}

func NewDefaultProject() *Project {
	return &Project{
		AssetsDir:    "./assets",
		OutputPath:   "output.mp4",
		FPS:          30,
		Width:        1920,
		Height:       1080,
		TTSAddr:      "http://localhost:5000",
		Speed:        1.0,
		HandTipX:     30,
		HandTipY:     20,
		Voice:        "am_adam",
		BGMVolume:    0.05,
		FreezeFrames: 60,
	}
}

type ScriptLine struct {
	Text    string       // Clean text for TTS
	Actions []DrawAction // Embedded draw commands
}

type DrawAction struct {
	Tag              string  // e.g. "king_death"
	WordIndex        int     // Trigger after this word finishes
	ImagePath        string  // Resolved path to asset PNG
	X, Y             int
	W, H             int
	RevealDuration   float64 // custom duration in seconds
	TriggerAfterWord bool    // trigger after word end instead of start
	GenPrompt        string  // prompt for image generation
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
	X, Y          int     // Position on canvas
	Width, Height int     // Render dimensions
	EventType     string  // "draw", "erase", "move", "text"
	MaskStyle     string  // "diagonal", "ltr", "ttb"
	HandStyle     string  // "pencil", "chalk", "eraser", "marker"
	DestX, DestY  int     // destination for "move" events
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
