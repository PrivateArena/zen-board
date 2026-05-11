package tts

import (
	"encoding/binary"
	"fmt"
)

func GetWAVDuration(data []byte) (float64, error) {
	if len(data) < 44 {
		return 0, fmt.Errorf("WAV too small")
	}

	// Format chunk starts at byte 12
	// "fmt " at 12
	// size at 16
	// type at 20
	// channels at 22
	// sample rate at 24
	// byte rate at 28
	// block align at 32
	// bits per sample at 34

	channels := binary.LittleEndian.Uint16(data[22:24])
	sampleRate := binary.LittleEndian.Uint32(data[24:28])
	bitsPerSample := binary.LittleEndian.Uint16(data[34:36])

	// Data chunk usually starts at 36 ("data")
	// size at 40
	dataSize := binary.LittleEndian.Uint32(data[40:44])

	if channels == 0 || sampleRate == 0 || bitsPerSample == 0 {
		return 0, fmt.Errorf("invalid WAV parameters")
	}

	bytesPerSample := float64(bitsPerSample) / 8.0
	duration := float64(dataSize) / (float64(sampleRate) * float64(channels) * bytesPerSample)

	return duration, nil
}
