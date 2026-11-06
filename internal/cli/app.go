package cli

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"zen-board/internal/constant"
	"zen-board/internal/envelope"
	"zen-board/internal/model"
)

// Version is the CLI revision, stamped into every envelope. Override at build
// time with -ldflags "-X zen-board/internal/cli.Version=...".
var Version = "dev"

// App routes the command line to a verb. It owns argument parsing, the result
// envelope and the exit code; the pipeline stages do not know it exists.
type App struct {
	// Config holds the defaults (config.json) that flag values override.
	Config *model.Project
	// BinaryDir locates config.json and the shipped assets.
	BinaryDir string
	Stdout    io.Writer
	Stderr    io.Writer

	jsonMode bool
}

// New builds an App over the defaults found in config.json.
func New() *App {
	binDir := BinaryDir()
	return &App{
		Config:    LoadConfig(binDir),
		BinaryDir: binDir,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
	}
}

// Verb is one command word.
type Verb struct {
	Name        string
	Summary     string
	NeedsScript bool
	// Run executes the verb. A verb that produces only its own text output returns
	// a nil envelope and the error is reported verbatim.
	Run func(*App, *Options, []string) (*envelope.Envelope, error)
}

// Verbs is the command surface. `render` is also the implicit verb, because the
// existing CLI takes flags with no verb at all and that form must keep working.
func Verbs() []Verb {
	return []Verb{
		{Name: "render", Summary: "render a script to an MP4 (TTS + encode)", Run: (*App).render},
		{Name: "check", Summary: "validate a script; no TTS, no network, no render", NeedsScript: true, Run: (*App).check},
		{Name: "snapshot", Summary: "write PNGs of selected frames; no ffmpeg, no TTS", NeedsScript: true, Run: (*App).snapshot},
		{Name: "timeline", Summary: "dump the compiled event timeline as JSON", NeedsScript: true, Run: (*App).timeline},
		{Name: "assets", Summary: "asset index, background removal, generation, web UI", Run: passThrough((*App).assets)},
	}
}

func passThrough(fn func(*App, *Options, []string) error) func(*App, *Options, []string) (*envelope.Envelope, error) {
	return func(a *App, opts *Options, argv []string) (*envelope.Envelope, error) {
		return nil, fn(a, opts, argv)
	}
}

// Run routes argv (without the program name) and returns the process exit code.
func (a *App) Run(argv []string) int {
	env, err := a.execute(argv)
	if err != nil {
		fmt.Fprintf(a.Stderr, "Error: %v\n", err)
		if env != nil && !a.jsonMode {
			fmt.Fprint(a.Stderr, env.Findings.String())
		}
		return envelope.ExitCode(err)
	}
	if env != nil && env.Findings.Blocked() {
		if !a.jsonMode {
			fmt.Fprint(a.Stderr, env.Findings.String())
		}
		return envelope.ExitBlocked
	}
	return envelope.ExitOK
}

// RunInProcess runs one command and returns its error, without writing an
// envelope or touching the exit status. The integration suite calls this in a loop
// inside one process, which is why nothing below ever calls os.Exit.
func (a *App) RunInProcess(argv []string) error {
	_, err := a.execute(argv)
	return err
}

// execute is the routing itself, shared by Run and RunInProcess. Under -json it
// also writes the envelope, after the stream redirect is undone: the document is
// the last thing to leave stdout, which is what makes two in-process invocations
// each produce exactly one.
func (a *App) execute(argv []string) (result *envelope.Envelope, err error) {
	if len(argv) >= 1 && (argv[0] == "help" || argv[0] == "-h" || argv[0] == "--help") {
		// `help` on its own lists the verbs; `help <verb>` lists that verb's flags, and
		// both go to stdout because they are the requested output, not diagnostics.
		if err := a.Usage(argv[1:]); err != nil {
			return nil, err
		}
		return nil, nil
	}
	name, rest := a.splitVerb(argv)
	for _, v := range Verbs() {
		if v.Name != name {
			continue
		}
		if name == "assets" {
			// The asset CLI owns its flag set, its output and its exit semantics.
			_, assetsErr := v.Run(a, &Options{Project: a.Config}, rest)
			return nil, assetsErr
		}
		opts, parseErr := a.parse(v, rest)
		if parseErr != nil {
			return nil, parseErr
		}
		env := envelope.New(v.Name)
		if opts.JSON {
			// Under -json, stdout carries exactly one document for the duration of
			// this command; the pipeline's progress printing goes to stderr.
			a.jsonMode = true
			stdout := a.Stdout
			undefer := RedirectForJSON()
			defer func() {
				undefer()
				a.jsonMode = false
				if result != nil {
					_ = result.Write(stdout)
				}
			}()
		}
		out, runErr := v.Run(a, opts, rest)
		if out != nil {
			env = out
		}
		if runErr != nil {
			env.Fail(runErr)
		}
		return env, runErr
	}
	return nil, envelope.Usage("unknown command %q\n\nRun 'zen-board help' for the command list", name)
}

// splitVerb accepts both `zen-board <verb> [flags]` and the legacy
// `zen-board [flags]`, which means `render`.
func (a *App) splitVerb(argv []string) (string, []string) {
	if len(argv) == 0 {
		return "render", nil
	}
	if strings.HasPrefix(argv[0], "-") {
		return "render", argv
	}
	return argv[0], argv[1:]
}

// Options are the resolved settings of one command invocation.
type Options struct {
	Project *model.Project
	JSON    bool

	// render
	HandPath     string
	HandExplicit bool
	EventLogPath string
	Preview      bool
	Fast         bool

	// check
	ShowDSL  bool
	LintOnly bool

	// snapshot
	At       string
	Every    string
	Events   string
	Frame    int
	Max      int
	MaxWidth int
	OutDir   string
	Sheet    string
}

// flagSet builds the flags of one verb over the loaded config defaults. Help
// output goes to out, so `help <verb>` prints on stdout while a parse error goes to
// stderr.
func (a *App) flagSet(v Verb, out io.Writer) (*flag.FlagSet, *Options) {
	conf := a.Config
	fs := flag.NewFlagSet(v.Name, flag.ContinueOnError)
	fs.SetOutput(out)
	opt := &Options{Project: conf, Frame: -1, Max: 24, Sheet: "jpg"}

	fs.StringVar(&conf.ScriptPath, "script", conf.ScriptPath, "Path to .zen script file")
	fs.StringVar(&conf.AssetsDir, "assets", conf.AssetsDir, "Directory containing image assets")
	fs.StringVar(&conf.OutputPath, "o", conf.OutputPath, "Output video path")
	fs.IntVar(&conf.FPS, "fps", conf.FPS, "Frames per second")
	fs.IntVar(&conf.Width, "w", conf.Width, "Canvas width")
	fs.IntVar(&conf.Height, "h", conf.Height, "Canvas height")
	fs.StringVar(&conf.TTSAddr, "tts", conf.TTSAddr, "zen-lights server address")
	fs.Float64Var(&conf.Speed, "speed", conf.Speed, "TTS speed multiplier")
	fs.StringVar(&conf.Voice, "voice", conf.Voice, "TTS voice ID")
	fs.BoolVar(&conf.DisableTranscript, "disable-transcript", conf.DisableTranscript, "Disable transcript/subtitle rendering")
	fs.StringVar(&conf.BGMPath, "bgm", conf.BGMPath, "Path to background music file")
	fs.Float64Var(&conf.BGMVolume, "bgm-vol", conf.BGMVolume, "Background music volume multiplier")
	fs.BoolVar(&conf.CameraEnabled, "camera", conf.CameraEnabled, "Enable camera zoom effects")
	fs.IntVar(&conf.FreezeFrames, "freeze", conf.FreezeFrames, "Freeze frames held at the end")
	fs.StringVar(&conf.TTSCacheDir, "tts-cache", conf.TTSCacheDir, "Directory caching TTS audio and timings")
	fs.StringVar(&conf.Cursor, "cursor", conf.Cursor, "Default cursor preset")
	fs.IntVar(&conf.TTSConcurrency, "tts-concurrency", conf.TTSConcurrency, "Max simultaneous TTS requests (0 = engine default)")
	fs.BoolVar(&opt.JSON, "json", false, "Emit one JSON envelope on stdout")

	switch v.Name {
	case "render":
		fs.StringVar(&opt.HandPath, "hand", DefaultHandFlagDefault, "Path to the hand.png sprite used when a cursor preset is not installed")
		fs.StringVar(&opt.EventLogPath, "eventlog", "", "Path to write the event transition log")
		fs.BoolVar(&opt.Preview, "preview", false, "Preview render in real time via ffplay")
		fs.BoolVar(&opt.Fast, "fast", false, "Fast preview encode (nearest-neighbour camera scale, ultrafast preset)")
	case "check":
		fs.BoolVar(&opt.ShowDSL, "dsl", false, "Print the DSL table and exit")
		fs.BoolVar(&opt.LintOnly, "fast", false, "Lint only: skip asset resolution")
	case "snapshot":
		fs.BoolVar(&opt.Fast, "fast", false, "Rejected: -fast rescales the canvas before compile")
		fs.StringVar(&opt.HandPath, "hand", DefaultHandFlagDefault, "Hand sprite to fall back to")
		fs.StringVar(&opt.At, "at", "", "explicit times (\"3s,10.5s\") or frame numbers (\"120,360\")")
		fs.StringVar(&opt.Every, "every", "", "interval, e.g. 0.5s")
		fs.StringVar(&opt.Events, "events", "all", "start|mid|end|all")
		fs.IntVar(&opt.Frame, "frame", -1, "single frame number")
		fs.IntVar(&opt.Max, "max", 24, "maximum number of frames to render")
		fs.IntVar(&opt.MaxWidth, "max-w", 0, "downscale PNGs to at most this width")
		fs.StringVar(&opt.OutDir, "outdir", "frames", "output directory")
		fs.StringVar(&opt.Sheet, "sheet", "jpg", "contact sheet format: jpg|png|none")
	}
	return fs, opt
}

// parse builds and parses the flags of one verb, then applies the invariants that
// make the verb runnable at all.
func (a *App) parse(v Verb, argv []string) (*Options, error) {
	conf := a.Config
	// Remember the configured canvas so -fast can halve it exactly once, the way
	// the single-verb CLI always did.
	fs, opt := a.flagSet(v, a.Stderr)
	if err := fs.Parse(argv); err != nil {
		return nil, envelope.Usage("%v", err)
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "hand" {
			opt.HandExplicit = true
		}
	})
	if conf.TTSConcurrency <= 0 {
		conf.TTSConcurrency = constant.TTS_MAX_CONCURRENCY
	}
	if v.NeedsScript && conf.ScriptPath == "" && !opt.ShowDSL {
		return nil, envelope.Usage("-script is required for %s", v.Name)
	}
	if v.Name == "render" && opt.Fast {
		conf.Width, conf.Height = halvedCanvas(conf.Width, conf.Height)
		log.Printf("[Fast Mode] Canvas size scaled down to %dx%d for preview", conf.Width, conf.Height)
	}
	return opt, nil
}

// halvedCanvas is the -fast preview reduction. It is why -fast must never be used
// to judge layout: the halving happens before compile, so every absolute @x,y in
// the script lands somewhere else.
func halvedCanvas(w, h int) (int, int) {
	w, h = w/2, h/2
	if w%2 != 0 {
		w++
	}
	if h%2 != 0 {
		h++
	}
	return w, h
}

// DefaultHandFlagDefault is the shipped hand sprite path. It is always loaded when
// present; the value only decides whether a missing sprite is an error.
const DefaultHandFlagDefault = "./assets/hand.png"

// Usage prints help for the whole CLI, or for one verb when named. An unknown verb is
// an error rather than a silent fallback to the command list.
func (a *App) Usage(argv []string) error {
	if len(argv) > 0 {
		for _, v := range Verbs() {
			if v.Name != argv[0] {
				continue
			}
			fs, _ := a.flagSet(v, a.Stdout)
			fmt.Fprintf(a.Stdout, "zen-board %s - %s\n\n", v.Name, v.Summary)
			fmt.Fprintf(a.Stdout, "Usage: zen-board %s [flags]\n\n", v.Name)
			fs.PrintDefaults()
			if len(argv) > 1 {
				return envelope.Usage("too many arguments to help")
			}
			return nil
		}
		return envelope.Usage("unknown command %q", argv[0])
	}
	fmt.Fprintf(a.Stdout, "zen-board %s - whiteboard video engine\n\nUsage:\n  zen-board <command> [flags]\n  zen-board [render flags]\n\nCommands:\n", Version)
	for _, v := range Verbs() {
		fmt.Fprintf(a.Stdout, "  %-9s %s\n", v.Name, v.Summary)
	}
	fmt.Fprintf(a.Stdout, "\nRun 'zen-board help <command>' for the flags of one command.\n")
	return nil
}
