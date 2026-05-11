package testutil

import (
	"encoding/binary"
	"net/http"
	"net/http/httptest"
)

// NewMockTTSServer returns a test server that responds with a valid minimal WAV file.
func NewMockTTSServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		
		// Minimal 16-bit mono 44.1kHz WAV
		dataSize := uint32(44100 * 2 * 2) // 2 seconds
		header := make([]byte, 44)
		copy(header[0:4], "RIFF")
		binary.LittleEndian.PutUint32(header[4:8], 36+dataSize)
		copy(header[8:12], "WAVE")
		copy(header[12:16], "fmt ")
		binary.LittleEndian.PutUint32(header[16:20], 16)
		binary.LittleEndian.PutUint16(header[20:22], 1)      // PCM
		binary.LittleEndian.PutUint16(header[22:24], 1)      // Mono
		binary.LittleEndian.PutUint32(header[24:28], 44100)  // Sample Rate
		binary.LittleEndian.PutUint32(header[28:32], 88200)  // Byte Rate
		binary.LittleEndian.PutUint16(header[32:34], 2)      // Block Align
		binary.LittleEndian.PutUint16(header[34:36], 16)     // Bits per sample
		copy(header[36:40], "data")
		binary.LittleEndian.PutUint32(header[40:44], dataSize)
		
		w.Write(header)
		w.Write(make([]byte, dataSize))
	}))
}
