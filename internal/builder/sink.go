package builder

import (
	"fmt"
	"image"

	"zen-board/internal/ffmpeg"
)

// FrameSink receives finished frames. It replaces the ffmpeg pipe as the one and
// only output of the frame loop, which is what lets the same render drive an
// encode, a PNG dump, or a test.
//
// Ownership - the rule this type exists to make explicit:
//
//   - The frame collector owns the buffer. It comes from the engine's pool and it
//     goes back to the pool, always.
//   - A sink never calls Put on the frame it was handed.
//   - A sink that keeps a frame beyond the call - a PNG writer, a diff buffer -
//     must copy it. Without the camera the same pointer is the pooled buffer
//     itself, so a retained frame would be silently overwritten by a later one.
type FrameSink interface {
	// Wants reports whether this frame is wanted. A false answer means the frame is
	// never rendered at all: skipping is the whole point of asking first.
	Wants(frame int) bool
	// WriteFrame receives one frame, identified by the number that was planned, and
	// which the collector still owns. A sink must not retain it.
	WriteFrame(frameNumber int, frame *image.RGBA) error
	// Close flushes and releases whatever the sink owns (for an encode, the ffmpeg
	// process). It is called exactly once, after the last WriteFrame.
	Close() error
}

// Reporter is the optional half of FrameSink that lets a sink describe its own
// output. The encode path says "Video saved to out.mp4"; a PNG dump says how many
// files it wrote. Nothing in the loop hard-codes either.
type Reporter interface {
	// Name labels the sink in the timing line, e.g. "the FFmpeg pipe".
	Name() string
	// Done is the closing status line.
	Done() string
}

// PipeSink adapts the ffmpeg stdin pipe to FrameSink. It is the historical output
// of the frame loop, unchanged in behaviour: the pipe is fed the raw RGBA bytes and
// closed when the render ends.
type PipeSink struct {
	pipe  *ffmpeg.Pipe
	path  string
	wrote int
}

var _ FrameSink = (*PipeSink)(nil)

// NewPipeSink wraps the encoder pipe.
func NewPipeSink(pipe *ffmpeg.Pipe) *PipeSink {
	return &PipeSink{pipe: pipe, path: pipe.OutputPath()}
}

// Wants every frame: an encode has no frames to skip.
func (s *PipeSink) Wants(int) bool { return true }

// WriteFrame pushes one frame into ffmpeg. The frame number is implicit: an encode
// consumes the stream in order and has no use for it.
func (s *PipeSink) WriteFrame(_ int, frame *image.RGBA) error {
	s.wrote++
	return s.pipe.WriteFrame(frame.Pix)
}

// Close finishes the encode.
func (s *PipeSink) Close() error { return s.pipe.Close() }

// Wrote is the number of frames pushed to the encoder.
func (s *PipeSink) Wrote() int { return s.wrote }

// WantsEverything lets the planner skip a million predicate calls on a full encode.
func (s *PipeSink) WantsEverything() bool { return true }

// Name implements Reporter.
func (s *PipeSink) Name() string { return "FFmpeg pipe" }

// Done implements Reporter.
func (s *PipeSink) Done() string { return fmt.Sprintf("Done! Video saved to %s", s.path) }

// planFrames is the ordered list of frames the loop will produce. The collector is
// driven by this list rather than by a running counter: if it counted, a frame the
// sink declined would never arrive and the collector would block forever waiting for
// it.
func planFrames(total int, sink FrameSink) []int {
	if allFramesSink(sink) {
		frames := make([]int, total)
		for i := range frames {
			frames[i] = i
		}
		return frames
	}
	var frames []int
	for f := 0; f < total; f++ {
		if sink.Wants(f) {
			frames = append(frames, f)
		}
	}
	return frames
}

// allFramesSink recognises the common "everything" case so a full encode does not
// call Wants once per frame.
func allFramesSink(sink FrameSink) bool {
	probe, ok := sink.(interface{ WantsEverything() bool })
	return ok && probe.WantsEverything()
}
