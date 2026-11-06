package script

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sort"
	"strings"
)

// Source is the raw script plus a line index, so every diagnostic can be stated
// as file:line:col and every tag can point back at the byte range it came from.
//
// Offsets are byte offsets into Raw. Column numbers count runes, not bytes, so a
// script with em-dashes or accented narration reports the column an editor shows.
type Source struct {
	Path  string
	Raw   []byte
	Lines []Line
}

// Line is one physical line of the script.
type Line struct {
	Number int    // 1-based
	Offset int    // byte offset of the first byte of Text, within Source.Raw
	Length int    // byte length of Text, excluding the line terminator
	Text   string // the line as written, without its terminator
}

// Load reads a script from disk.
func Load(path string) (*Source, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return NewSource(path, data), nil
}

// NewSource indexes an already-read script.
func NewSource(path string, raw []byte) *Source {
	s := &Source{Path: path, Raw: raw}
	offset := 0
	num := 1
	for {
		nl := indexByteFrom(raw, '\n', offset)
		if nl < 0 {
			s.appendLine(num, offset, offset, len(raw))
			break
		}
		s.appendLine(num, offset, offset, nl)
		offset = nl + 1
		num++
		if offset > len(raw) {
			break
		}
	}
	return s
}

func (s *Source) appendLine(number, lineStart, textStart, textEnd int) {
	if textEnd < textStart {
		textEnd = textStart
	}
	s.Lines = append(s.Lines, Line{
		Number: number,
		Offset: textStart,
		Length: textEnd - textStart,
		Text:   string(s.Raw[textStart:textEnd]),
	})
	_ = lineStart
}

func indexByteFrom(b []byte, c byte, from int) int {
	if from >= len(b) {
		return -1
	}
	i := strings.IndexByte(string(b[from:]), c)
	if i < 0 {
		return -1
	}
	return from + i
}

// LineOf returns the Line containing a byte offset (the last line if past the end).
func (s *Source) LineOf(byteOffset int) Line {
	if len(s.Lines) == 0 {
		return Line{Number: 1}
	}
	i := sort.Search(len(s.Lines), func(i int) bool {
		return s.Lines[i].Offset+s.Lines[i].Length >= byteOffset
	})
	if i >= len(s.Lines) {
		return s.Lines[len(s.Lines)-1]
	}
	return s.Lines[i]
}

// Locate maps a byte offset to the 1-based line and rune column an editor shows.
func (s *Source) Locate(byteOffset int) (line, col int) {
	l := s.LineOf(byteOffset)
	if byteOffset < l.Offset {
		return l.Number, 1
	}
	prefix := l.Text
	if off := byteOffset - l.Offset; off < len(prefix) {
		prefix = prefix[:off]
	}
	return l.Number, len([]rune(prefix)) + 1
}

// Snippet returns the source line a byte offset falls on, trimmed to one line.
func (s *Source) Snippet(byteOffset int) string {
	l := s.LineOf(byteOffset)
	text := strings.TrimSpace(l.Text)
	const max = 96
	if len(text) > max {
		if byteOffset-l.Offset <= max {
			text = text[:minInt(max, len(text))] + "..."
		} else {
			start := byteOffset - l.Offset - max/2
			if start < 0 {
				start = 0
			}
			end := start + max
			if end > len(text) {
				end = len(text)
			}
			text = "..." + text[start:end] + "..."
		}
	}
	return text
}

// Hash is the content identity of the script: everything an artifact (a PNG, a
// snapshot directory) was derived from can be proved to match it.
func (s *Source) Hash() string {
	sum := sha256.Sum256(s.Raw)
	return hex.EncodeToString(sum[:])
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
