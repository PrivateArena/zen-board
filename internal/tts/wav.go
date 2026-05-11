package tts

import (
	"encoding/binary"
	"fmt"
)

func GetWAVDuration(data []byte) (float64, error) {
	if len(data) < 12 {
		return 0, fmt.Errorf("WAV too small")
	}

	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return 0, fmt.Errorf("not a valid WAVE file")
	}

	var channels uint16
	var sampleRate uint32
	var bitsPerSample uint16
	var dataSize uint32
	foundFmt := false
	foundData := false

	pos := 12
	for pos+8 <= len(data) {
		chunkID := string(data[pos : pos+4])
		chunkSize := binary.LittleEndian.Uint32(data[pos+4 : pos+8])
		nextPos := pos + 8 + int(chunkSize)

		if chunkID == "fmt " && chunkSize >= 16 {
			channels = binary.LittleEndian.Uint16(data[pos+10 : pos+12])
			sampleRate = binary.LittleEndian.Uint32(data[pos+12 : pos+16])
			bitsPerSample = binary.LittleEndian.Uint16(data[pos+22 : pos+24])
			foundFmt = true
		} else if chunkID == "data" {
			dataSize = chunkSize
			foundData = true
		}

		pos = nextPos
		if chunkSize%2 != 0 {
			pos++
		}
	}

	if !foundFmt || !foundData {
		return 0, fmt.Errorf("missing fmt or data chunk")
	}

	if channels == 0 || sampleRate == 0 || bitsPerSample == 0 {
		return 0, fmt.Errorf("invalid WAV parameters")
	}

	bytesPerSample := float64(bitsPerSample) / 8.0
	duration := float64(dataSize) / (float64(sampleRate) * float64(channels) * bytesPerSample)

	return duration, nil
}
