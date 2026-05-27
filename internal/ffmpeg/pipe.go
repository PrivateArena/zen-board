package ffmpeg

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

type Pipe struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	extraCmd *exec.Cmd
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
	cmd.Stderr = os.Stderr
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
	err := p.cmd.Wait()
	if p.extraCmd != nil {
		err2 := p.extraCmd.Wait()
		if err == nil {
			err = err2
		}
	}
	return err
}

func NewPreviewPipe(width, height, fps int, audioPath string) (*Pipe, error) {
	args := []string{
		"-y",
		"-f", "rawvideo",
		"-pixel_format", "rgba",
		"-video_size", fmt.Sprintf("%dx%d", width, height),
		"-framerate", fmt.Sprintf("%d", fps),
		"-i", "pipe:0",
	}
	if audioPath != "" {
		args = append(args, "-i", audioPath)
	}

	args = append(args,
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-tune", "zerolatency",
		"-pix_fmt", "yuv420p",
	)
	if audioPath != "" {
		args = append(args, "-c:a", "aac", "-b:a", "192k")
	}
	args = append(args, "-f", "nut", "pipe:1")

	ffmpegCmd := exec.Command("ffmpeg", args...)
	ffmpegCmd.Stderr = os.Stderr

	stdin, err := ffmpegCmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := ffmpegCmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	ffplayCmd := exec.Command("ffplay", "-window_title", "Zen-Board Preview", "-")
	ffplayCmd.Stdin = stdout
	ffplayCmd.Stderr = os.Stderr

	if err := ffplayCmd.Start(); err != nil {
		return nil, err
	}
	if err := ffmpegCmd.Start(); err != nil {
		return nil, err
	}

	return &Pipe{cmd: ffmpegCmd, stdin: stdin, extraCmd: ffplayCmd}, nil
}
