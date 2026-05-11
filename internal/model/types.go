package model

type Project struct {
	ScriptPath string  `json:"script_path"`
	AssetsDir  string  `json:"assets_dir"`
	OutputPath string  `json:"output_path"`
	FPS        int     `json:"fps"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	TTSAddr    string  `json:"tts_addr"`
	Speed      float64 `json:"speed"`
}

func NewDefaultProject() *Project {
	return &Project{
		AssetsDir:  "./assets",
		OutputPath: "output.mp4",
		FPS:        30,
		Width:      1920,
		Height:     1080,
		TTSAddr:    "http://localhost:5000",
		Speed:      1.0,
	}
}

type ScriptLine struct {
	Text    string       // Clean text for TTS
	Actions []DrawAction // Embedded draw commands
}

type DrawAction struct {
	Tag       string  // e.g. "king_death"
	WordIndex int     // Trigger after this word finishes
	ImagePath string  // Resolved path to asset PNG
}

type WordTiming struct {
	Word  string
	Start float64 // seconds
	End   float64 // seconds
}

type FrameEvent struct {
	TargetImage string
	StartFrame  int
	EndFrame    int
	X, Y        int     // Position on canvas
	Width, Height int   // Render dimensions
}

type Timeline struct {
	Events    []FrameEvent
	Words     []WordTiming
	AudioPath string
	Duration  float64 // total seconds
}
