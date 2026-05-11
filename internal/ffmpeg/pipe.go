package ffmpeg

import (
	"fmt"
	"io"
	"os/exec"
)

type Pipe struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
}

func NewPipe(outputPath, audioPath, assPath string, width, height, fps int) (*Pipe, error) {
	args := []string{
		"-y",
		"-f", "rawvideo",
		"-pixel_format", "rgba",
		"-video_size", fmt.Sprintf("%dx%d", width, height),
		"-framerate", fmt.Sprintf("%d", fps),
		"-i", "pipe:0",
		"-i", audioPath,
		"-vf", fmt.Sprintf("ass=%s", assPath),
		"-c:v", "libx264",
		"-preset", "fast",
		"-crf", "18",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-b:a", "192k",
		"-shortest",
		outputPath,
	}

	cmd := exec.Command("ffmpeg", args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &Pipe{cmd: cmd, stdin: stdin}, nil
}

func (p *Pipe) WriteFrame(data []byte) error {
	_, err := p.stdin.Write(data)
	return err
}

func (p *Pipe) Close() error {
	p.stdin.Close()
	return p.cmd.Wait()
}

func NewPreviewPipe(width, height, fps int) (*Pipe, error) {
	args := []string{
		"-f", "rawvideo",
		"-pixel_format", "rgba",
		"-video_size", fmt.Sprintf("%dx%d", width, height),
		"-framerate", fmt.Sprintf("%d", fps),
		"-i", "pipe:0",
		"-window_title", "Zen-Board Preview",
	}

	cmd := exec.Command("ffplay", args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &Pipe{cmd: cmd, stdin: stdin}, nil
}
