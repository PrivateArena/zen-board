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

type WAVParams struct {
	Channels      uint16
	SampleRate    uint32
	BitsPerSample uint16
}

func ParseWAVParams(data []byte) (WAVParams, error) {
	var params WAVParams
	if len(data) < 12 {
		return params, fmt.Errorf("WAV too small")
	}

	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return params, fmt.Errorf("not a valid WAVE file")
	}

	pos := 12
	foundFmt := false

	for pos+8 <= len(data) {
		chunkID := string(data[pos : pos+4])
		chunkSize := binary.LittleEndian.Uint32(data[pos+4 : pos+8])
		nextPos := pos + 8 + int(chunkSize)

		if chunkID == "fmt " && chunkSize >= 16 {
			if pos+24 > len(data) {
				return params, fmt.Errorf("fmt chunk out of bounds")
			}
			params.Channels = binary.LittleEndian.Uint16(data[pos+10 : pos+12])
			params.SampleRate = binary.LittleEndian.Uint32(data[pos+12 : pos+16])
			params.BitsPerSample = binary.LittleEndian.Uint16(data[pos+22 : pos+24])
			foundFmt = true
			break
		}

		pos = nextPos
		if chunkSize%2 != 0 {
			pos++
		}
	}

	if !foundFmt {
		return params, fmt.Errorf("missing fmt chunk")
	}
	return params, nil
}

func CreateWAVHeader(params WAVParams, dataSize uint32) []byte {
	header := make([]byte, 44)

	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], 36+dataSize)
	copy(header[8:12], "WAVE")

	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16) // Subchunk1Size
	binary.LittleEndian.PutUint16(header[20:22], 1)  // AudioFormat = PCM
	binary.LittleEndian.PutUint16(header[22:24], params.Channels)
	binary.LittleEndian.PutUint32(header[24:28], params.SampleRate)

	bytesPerSample := uint32(params.BitsPerSample) / 8
	byteRate := params.SampleRate * uint32(params.Channels) * bytesPerSample
	binary.LittleEndian.PutUint32(header[28:32], byteRate)

	blockAlign := params.Channels * uint16(bytesPerSample)
	binary.LittleEndian.PutUint16(header[32:34], blockAlign)
	binary.LittleEndian.PutUint16(header[34:36], params.BitsPerSample)

	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], dataSize)

	return header
}

func CreateSilentWAV(params WAVParams, duration float64) []byte {
	bytesPerSample := uint32(params.BitsPerSample) / 8
	dataSize := uint32(duration * float64(params.SampleRate) * float64(params.Channels) * float64(bytesPerSample))
	// Data size must be even
	if dataSize%2 != 0 {
		dataSize++
	}
	pcm := make([]byte, dataSize) // all zeros is silence in standard PCM format
	header := CreateWAVHeader(params, dataSize)
	
	result := make([]byte, len(header)+len(pcm))
	copy(result[0:], header)
	copy(result[len(header):], pcm)
	return result
}
