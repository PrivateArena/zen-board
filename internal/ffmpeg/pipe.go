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

func buildAudioArgs(audioPath, bgmPath string, bgmVolume float64, videoIdx int, nextIdxStart int) (inputArgs []string, filterComplex []string, mapArgs []string, nextIdx int) {
	nextIdx = nextIdxStart
	audioIdx := -1
	bgmIdx := -1

	if audioPath != "" {
		inputArgs = append(inputArgs, "-i", audioPath)
		audioIdx = nextIdx
		nextIdx++
	}

	if bgmPath != "" {
		inputArgs = append(inputArgs, "-stream_loop", "-1", "-i", bgmPath)
		bgmIdx = nextIdx
		nextIdx++
	}

	if audioIdx != -1 && bgmIdx != -1 {
		filterComplex = []string{
			"-filter_complex",
			fmt.Sprintf("[%d:a]volume=%.4f[bgm];[%d:a][bgm]amix=inputs=2:duration=first:dropout_transition=0[audio_out]", bgmIdx, bgmVolume, audioIdx),
		}
		mapArgs = []string{
			"-map", fmt.Sprintf("%d:v", videoIdx),
			"-map", "[audio_out]",
		}
	} else if audioIdx != -1 {
		mapArgs = []string{
			"-map", fmt.Sprintf("%d:v", videoIdx),
			"-map", fmt.Sprintf("%d:a", audioIdx),
		}
	} else if bgmIdx != -1 {
		filterComplex = []string{
			"-filter_complex",
			fmt.Sprintf("[%d:a]volume=%.4f[audio_out]", bgmIdx, bgmVolume),
		}
		mapArgs = []string{
			"-map", fmt.Sprintf("%d:v", videoIdx),
			"-map", "[audio_out]",
		}
	} else {
		mapArgs = []string{
			"-map", fmt.Sprintf("%d:v", videoIdx),
		}
	}

	return inputArgs, filterComplex, mapArgs, nextIdx
}

func NewPipe(outputPath, audioPath, assPath, bgmPath string, bgmVolume float64, width, height, fps int, duration float64, metadataPath string, fastMode bool) (*Pipe, error) {
	args := []string{
		"-y",
		"-f", "rawvideo",
		"-pixel_format", "rgba",
		"-video_size", fmt.Sprintf("%dx%d", width, height),
		"-framerate", fmt.Sprintf("%d", fps),
		"-i", "pipe:0",
	}

	audioInputs, filterComplex, mapArgs, nextIdx := buildAudioArgs(audioPath, bgmPath, bgmVolume, 0, 1)
	args = append(args, audioInputs...)

	metaIdx := -1
	if metadataPath != "" {
		args = append(args, "-i", metadataPath)
		metaIdx = nextIdx
		nextIdx++
	}

	if assPath != "" {
		args = append(args, "-vf", fmt.Sprintf("ass=%s", assPath))
	}

	if len(filterComplex) > 0 {
		args = append(args, filterComplex...)
	}
	args = append(args, mapArgs...)

	if metadataPath != "" {
		args = append(args, "-map_metadata", fmt.Sprintf("%d", metaIdx))
	}

	preset := "fast"
	if fastMode {
		preset = "ultrafast"
	}

	args = append(args,
		"-loglevel", "error",
		"-c:v", "libx264",
		"-preset", preset,
		"-crf", "18",
		"-pix_fmt", "yuv420p",
	)

	hasAudioMapped := (audioPath != "" || bgmPath != "")
	if hasAudioMapped {
		args = append(args, "-c:a", "aac", "-b:a", "192k")
	}

	args = append(args,
		"-t", fmt.Sprintf("%.6f", duration),
		outputPath,
	)

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

func NewPreviewPipe(width, height, fps int, audioPath, bgmPath string, bgmVolume float64, duration float64, metadataPath string) (*Pipe, error) {
	args := []string{
		"-y",
		"-f", "rawvideo",
		"-pixel_format", "rgba",
		"-video_size", fmt.Sprintf("%dx%d", width, height),
		"-framerate", fmt.Sprintf("%d", fps),
		"-i", "pipe:0",
	}

	audioInputs, filterComplex, mapArgs, nextIdx := buildAudioArgs(audioPath, bgmPath, bgmVolume, 0, 1)
	args = append(args, audioInputs...)

	metaIdx := -1
	if metadataPath != "" {
		args = append(args, "-i", metadataPath)
		metaIdx = nextIdx
		nextIdx++
	}

	if len(filterComplex) > 0 {
		args = append(args, filterComplex...)
	}
	args = append(args, mapArgs...)

	if metadataPath != "" {
		args = append(args, "-map_metadata", fmt.Sprintf("%d", metaIdx))
	}

	args = append(args,
		"-loglevel", "error",
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-tune", "zerolatency",
		"-pix_fmt", "yuv420p",
	)
	hasAudioMapped := (audioPath != "" || bgmPath != "")
	if hasAudioMapped {
		args = append(args, "-c:a", "aac", "-b:a", "192k")
	}
	if duration > 0 {
		args = append(args, "-t", fmt.Sprintf("%.6f", duration))
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
