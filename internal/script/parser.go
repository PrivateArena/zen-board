package script

import (
	"regexp"
	"strconv"
	"strings"
	"zen-board/internal/model"
)

var (
	drawRegex = regexp.MustCompile(`\[draw:([^\]]+)\]`)
	waitRegex = regexp.MustCompile(`\[wait:([\d.]+)\]`)
)

func Parse(input string) []model.ScriptLine {
	lines := strings.Split(input, "\n")
	var result []model.ScriptLine

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		cleanText, actions := extractActions(line)
		result = append(result, model.ScriptLine{
			Text:    cleanText,
			Actions: actions,
		})
	}

	return result
}

func extractActions(line string) (string, []model.DrawAction) {
	var actions []model.DrawAction
	
	// We need to keep track of the text as we remove tags to know the word index
	// A simple way is to replace tags with a placeholder, then split by words, 
	// then find the index of the placeholder.
	
	// Find all tags (draw and wait - wait might be handled differently or as a special action)
	// For now, let's focus on draw.
	
	type tagInfo struct {
		start, end int
		tag        string
		isWait     bool
		waitVal    float64
	}
	
	var tags []tagInfo
	cleanBuilder := strings.Builder{}
	lastPos := 0
	
	drawMatches := drawRegex.FindAllStringSubmatchIndex(line, -1)
	for _, m := range drawMatches {
		tags = append(tags, tagInfo{start: m[0], end: m[1], tag: line[m[2]:m[3]]})
	}
	
	waitMatches := waitRegex.FindAllStringSubmatchIndex(line, -1)
	for _, m := range waitMatches {
		val, _ := strconv.ParseFloat(line[m[2]:m[3]], 64)
		tags = append(tags, tagInfo{start: m[0], end: m[1], isWait: true, waitVal: val})
	}
	
	// Sort tags by start position
	for i := 0; i < len(tags); i++ {
		for j := i + 1; j < len(tags); j++ {
			if tags[i].start > tags[j].start {
				tags[i], tags[j] = tags[j], tags[i]
			}
		}
	}

	for _, t := range tags {
		cleanBuilder.WriteString(line[lastPos:t.start])
		
		// Calculate word index in the clean text built so far
		currentClean := cleanBuilder.String()
		wordCount := len(strings.Fields(currentClean))
		
		if t.isWait {
			// Special handling for wait? Maybe add as a special action
			actions = append(actions, model.DrawAction{
				Tag:       "WAIT:" + strconv.FormatFloat(t.waitVal, 'f', -1, 64),
				WordIndex: wordCount,
			})
		} else {
			actions = append(actions, model.DrawAction{
				Tag:       t.tag,
				WordIndex: wordCount,
			})
		}
		
		lastPos = t.end
	}
	cleanBuilder.WriteString(line[lastPos:])
	
	// Normalize spaces
	cleanText := cleanBuilder.String()
	words := strings.Fields(cleanText)
	return strings.Join(words, " "), actions
}
