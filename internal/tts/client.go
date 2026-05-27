package tts

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"zen-board/internal/model"
)

type TTSClient struct {
	Addr string
}

func NewClient(addr string) *TTSClient {
	return &TTSClient{Addr: addr}
}

type ttsRequest struct {
	Text  string  `json:"text"`
	Speed float64 `json:"speed"`
	Voice string  `json:"voice,omitempty"`
}

func (c *TTSClient) Synthesize(text string, speed float64, voice string) ([]byte, error) {
	reqBody, err := json.Marshal(ttsRequest{Text: text, Speed: speed, Voice: voice})
	if err != nil {
		return nil, fmt.Errorf("marshal error: %w", err)
	}
	resp, err := http.Post(c.Addr+"/tts", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("TTS server error (%d): %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}

// TTSResult carries synthesized audio plus optional ground-truth word timings.
// Timings is nil when the server is an older version that does not support ?timestamps=1.
type TTSResult struct {
	Audio    []byte           // complete WAV bytes
	Timings  []model.WordTiming // nil if server did not return timings
	Duration float64          // exact audio duration derived from WAV samples
}

// SynthesizeWithTimings calls /tts?timestamps=1 and decodes the JSON envelope.
// Falls back transparently to raw-WAV Synthesize() if the server returns audio/wav.
func (c *TTSClient) SynthesizeWithTimings(text string, speed float64, voice string) (*TTSResult, error) {
	reqBody, err := json.Marshal(ttsRequest{Text: text, Speed: speed, Voice: voice})
	if err != nil {
		return nil, fmt.Errorf("marshal error: %w", err)
	}
	resp, err := http.Post(c.Addr+"/tts?timestamps=1", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("TTS server error (%d): %s", resp.StatusCode, string(body))
	}

	// Check content-type to detect old server returning raw WAV
	ct := resp.Header.Get("Content-Type")
	if ct == "audio/wav" {
		// Fallback path: old server — no timings available
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		dur, _ := GetWAVDuration(data)
		return &TTSResult{Audio: data, Duration: dur}, nil
	}

	// New path: JSON envelope
	var envelope struct {
		Audio    string             `json:"audio"`
		Timings  []model.WordTiming `json:"timings"`
		Duration float64            `json:"duration"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decoding timestamps response: %w", err)
	}

	wavBytes, err := base64.StdEncoding.DecodeString(envelope.Audio)
	if err != nil {
		return nil, fmt.Errorf("decoding base64 audio: %w", err)
	}

	// Prefer exact duration from WAV over server-reported value
	dur := envelope.Duration
	if wavDur, werr := GetWAVDuration(wavBytes); werr == nil {
		dur = wavDur
	}

	return &TTSResult{
		Audio:    wavBytes,
		Timings:  envelope.Timings,
		Duration: dur,
	}, nil
}

// ConcatenateWAVs stitches multiple WAV files into one.
// It assumes all WAVs have the same sample rate/channels/bit depth.
func ConcatenateWAVs(chunks [][]byte) ([]byte, error) {
	if len(chunks) == 0 {
		return nil, nil
	}
	if len(chunks) == 1 {
		return chunks[0], nil
	}

	params, err := ParseWAVParams(chunks[0])
	if err != nil {
		return nil, fmt.Errorf("parsing WAV params of first chunk: %w", err)
	}

	var pcmData bytes.Buffer
	var totalDataSize uint32

	for _, chunk := range chunks {
		// Find data chunk
		pos := 12
		found := false
		for pos+8 <= len(chunk) {
			chunkID := string(chunk[pos : pos+4])
			chunkSize := binary.LittleEndian.Uint32(chunk[pos+4 : pos+8])
			if chunkID == "data" {
				pcmData.Write(chunk[pos+8 : pos+8+int(chunkSize)])
				totalDataSize += chunkSize
				found = true
				break
			}
			pos += 8 + int(chunkSize)
			if chunkSize%2 != 0 {
				pos++
			}
		}
		if !found {
			return nil, fmt.Errorf("missing data chunk in WAV chunk")
		}
	}

	header := CreateWAVHeader(params, totalDataSize)

	var result bytes.Buffer
	result.Write(header)
	result.Write(pcmData.Bytes())

	return result.Bytes(), nil
}

func SaveWAV(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}
