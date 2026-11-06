package testutil

import (
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"sync"
)

// NewMockTTSServer returns a test server that responds with a valid minimal WAV file.
func NewMockTTSServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")

		w.Write(mockWAV())
	}))
}

// CountingResponseWriter is not needed here; the mock server counts requests
// itself. Requests is safe for concurrent use.

// CountingMock is a mock TTS server plus the request counter the gate tests need:
// `check` must prove it spent no TTS budget, and the only honest way to assert that
// is to count what the server actually received.
type CountingMock struct {
	Server   *httptest.Server
	requests int64
	mu       sync.Mutex
}

// Requests is the number of synthesis requests the server has served.
func (c *CountingMock) Requests() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return int(c.requests)
}

// URL is the address to point -tts at.
func (c *CountingMock) URL() string { return c.Server.URL }

// Close shuts the server down.
func (c *CountingMock) Close() { c.Server.Close() }

// NewCountingMockTTSServer returns a mock TTS server that records every request.
func NewCountingMockTTSServer() *CountingMock {
	mock := &CountingMock{}
	mock.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mock.mu.Lock()
		mock.requests++
		mock.mu.Unlock()
		w.Header().Set("Content-Type", "audio/wav")
		w.Write(mockWAV())
	}))
	return mock
}

// mockWAV is a minimal 16-bit mono 44.1kHz WAV, two seconds long.
func mockWAV() []byte {
	dataSize := uint32(44100 * 2 * 2)
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], 36+dataSize)
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)     // PCM
	binary.LittleEndian.PutUint16(header[22:24], 1)     // Mono
	binary.LittleEndian.PutUint32(header[24:28], 44100) // Sample Rate
	binary.LittleEndian.PutUint32(header[28:32], 88200) // Byte Rate
	binary.LittleEndian.PutUint16(header[32:34], 2)     // Block Align
	binary.LittleEndian.PutUint16(header[34:36], 16)    // Bits per sample
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], dataSize)
	return append(header, make([]byte, dataSize)...)
}
