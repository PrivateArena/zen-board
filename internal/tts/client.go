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
	reqBody, _ := json.Marshal(ttsRequest{Text: text, Speed: speed})
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

	var totalDataSize uint32
	var header []byte

	// Extract header from first chunk
	if len(chunks[0]) < 44 {
		return nil, fmt.Errorf("WAV chunk too small")
	}
	header = make([]byte, 44)
	copy(header, chunks[0][:44])

	var result bytes.Buffer
	result.Write(header)

	for i, chunk := range chunks {
		if len(chunk) < 44 {
			continue
		}
		data := chunk[44:]
		result.Write(data)
		totalDataSize += uint32(len(data))
		_ = i
	}

	final := result.Bytes()
	// Update RIFF chunk size (total file size - 8)
	binary.LittleEndian.PutUint32(final[4:8], 36+totalDataSize)
	// Update data chunk size
	binary.LittleEndian.PutUint32(final[40:44], totalDataSize)

	return final, nil
}

func SaveWAV(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}
