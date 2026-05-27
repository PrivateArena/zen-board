package tts

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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
}

func (c *TTSClient) Synthesize(text string, speed float64) ([]byte, error) {
	reqBody, err := json.Marshal(ttsRequest{Text: text, Speed: speed})
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
