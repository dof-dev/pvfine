package preview

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// IssueSeverity describes whether a preview issue prevents the document from
// being used as a new playback document.
type IssueSeverity string

const (
	IssueWarning IssueSeverity = "warning"
	IssueError   IssueSeverity = "error"
)

// Issue is a recoverable ANI parsing problem. Line is one-based and points to
// the source line that introduced the problem when possible.
type Issue struct {
	Severity IssueSeverity
	Line     int
	Section  string
	Message  string
}

// Layer is one image layer in an ANI frame.
type Layer struct {
	Path  string
	Index int32
	X     int32
	Y     int32
}

// Frame is one decoded ANI frame.
type Frame struct {
	Index  int32
	Delay  int32
	Layers []Layer
}

// Document is the structured preview model produced from ANI text.
type Document struct {
	Valid    bool
	Loop     bool
	Shadow   bool
	FrameMax int32
	Frames   []Frame
	Issues   []Issue
}

type token struct {
	value string
	line  int
}

type section struct {
	name   string
	line   int
	values []token
}

type frameBlock struct {
	section section
	parts   []section
}

// ParseANI parses the decompiled TypeScript text used by an .ani file. The
// parser intentionally works on text rather than archive tokens so preview
// results can follow unsaved editor content.
func ParseANI(text string) Document {
	sections := parseSections(text)
	document := Document{FrameMax: -1, Frames: make([]Frame, 0)}
	issues := make([]Issue, 0)

	global := make(map[string][]section)
	blocks := make([]frameBlock, 0)
	var current *frameBlock
	for _, item := range sections {
		if _, ok := parseFrameName(item.name); ok {
			block := frameBlock{section: item, parts: make([]section, 0)}
			block.section.name = item.name
			blocks = append(blocks, block)
			current = &blocks[len(blocks)-1]
			continue
		}
		if strings.HasPrefix(item.name, "frame") && item.name != "frame max" {
			issues = append(issues, Issue{Severity: IssueError, Line: item.line, Section: item.name, Message: "帧 section 名称必须是 FRAME 加数字"})
			current = nil
			continue
		}
		if current != nil {
			if isFramePart(item.name) {
				current.parts = append(current.parts, item)
				continue
			}
			// A new top-level section ends the current frame. Keep known
			// global sections in the global map for files that place them
			// after the frame list.
			if isGlobalSection(item.name) {
				global[item.name] = append(global[item.name], item)
				current = nil
				continue
			}
			current.parts = append(current.parts, item)
			continue
		}
		if isGlobalSection(item.name) {
			global[item.name] = append(global[item.name], item)
		}
	}

	parseBooleanField(&document.Loop, global["loop"], "LOOP", &issues)
	parseBooleanField(&document.Shadow, global["shadow"], "SHADOW", &issues)
	if values := global["frame max"]; len(values) == 0 {
		issues = append(issues, Issue{Severity: IssueError, Line: 1, Section: "FRAME MAX", Message: "缺少 [FRAME MAX]"})
	} else {
		document.FrameMax = parseNonNegativeInt(values[len(values)-1], "FRAME MAX", &issues, -1)
		if len(values) > 1 {
			issues = append(issues, Issue{Severity: IssueWarning, Line: values[len(values)-1].line, Section: "FRAME MAX", Message: "重复的 [FRAME MAX]，使用最后一个值"})
		}
	}

	frames := make([]Frame, 0, len(blocks))
	seenFrames := make(map[int32]struct{}, len(blocks))
	for _, block := range blocks {
		frameIndex, _ := parseFrameName(block.section.name)
		if _, exists := seenFrames[frameIndex]; exists {
			issues = append(issues, Issue{Severity: IssueError, Line: block.section.line, Section: block.section.name, Message: fmt.Sprintf("重复的帧编号 %d", frameIndex)})
			continue
		}
		seenFrames[frameIndex] = struct{}{}
		frame := Frame{Index: frameIndex, Delay: 100, Layers: make([]Layer, 0)}
		frameLine := block.section.line
		var pendingLayer *Layer
		posSet := false
		delaySeen := false

		flushLayerPosition := func(line int) {
			if pendingLayer != nil && !posSet {
				issues = append(issues, Issue{Severity: IssueWarning, Line: line, Section: block.section.name, Message: "图片缺少 [IMAGE POS]，使用坐标 0,0"})
			}
		}
		for _, part := range block.parts {
			switch part.name {
			case "image":
				flushLayerPosition(part.line)
				layer, ok := parseImage(part, &issues, block.section.name)
				if !ok {
					pendingLayer = nil
					posSet = false
					continue
				}
				frame.Layers = append(frame.Layers, layer)
				pendingLayer = &frame.Layers[len(frame.Layers)-1]
				posSet = false
			case "image pos":
				if pendingLayer == nil {
					issues = append(issues, Issue{Severity: IssueWarning, Line: part.line, Section: block.section.name, Message: "[IMAGE POS] 没有对应的 [IMAGE]，已忽略"})
					continue
				}
				if len(part.values) < 2 {
					issues = append(issues, Issue{Severity: IssueError, Line: part.line, Section: "IMAGE POS", Message: "[IMAGE POS] 需要两个整数"})
					continue
				}
				x, xOK := parseInt(part.values[0])
				y, yOK := parseInt(part.values[1])
				if !xOK || !yOK {
					issues = append(issues, Issue{Severity: IssueError, Line: part.line, Section: "IMAGE POS", Message: "[IMAGE POS] 坐标必须是整数"})
					continue
				}
				pendingLayer.X, pendingLayer.Y = x, y
				posSet = true
			case "delay":
				if delaySeen {
					issues = append(issues, Issue{Severity: IssueWarning, Line: part.line, Section: block.section.name, Message: "重复的 [DELAY]，使用最后一个值"})
				}
				delaySeen = true
				if len(part.values) == 0 {
					issues = append(issues, Issue{Severity: IssueWarning, Line: part.line, Section: "DELAY", Message: "[DELAY] 缺少数值，使用 100ms"})
					frame.Delay = 100
					continue
				}
				delay, ok := parseInt(part.values[0])
				if !ok || delay <= 0 {
					issues = append(issues, Issue{Severity: IssueWarning, Line: part.line, Section: "DELAY", Message: "[DELAY] 必须是正整数，使用 100ms"})
					frame.Delay = 100
					continue
				}
				frame.Delay = delay
			}
		}
		flushLayerPosition(frameLine)
		if !delaySeen {
			issues = append(issues, Issue{Severity: IssueWarning, Line: frameLine, Section: block.section.name, Message: "帧缺少 [DELAY]，使用 100ms"})
		}
		if len(frame.Layers) == 0 {
			issues = append(issues, Issue{Severity: IssueError, Line: frameLine, Section: block.section.name, Message: "帧没有有效的 [IMAGE]"})
		}
		frames = append(frames, frame)
	}

	sort.SliceStable(frames, func(i, j int) bool { return frames[i].Index < frames[j].Index })
	document.Frames = frames
	if document.FrameMax < 0 {
		// Keep the value useful for an invalid document without allowing a
		// consumer to mistake it for a valid declared count.
		document.FrameMax = int32(len(frames))
	}
	if document.FrameMax == 0 && len(frames) == 0 {
		issues = append(issues, Issue{Severity: IssueError, Line: 1, Section: "FRAME MAX", Message: "动画没有帧"})
	}
	if document.FrameMax != int32(len(frames)) {
		issues = append(issues, Issue{Severity: IssueError, Line: frameMaxLine(global["frame max"]), Section: "FRAME MAX", Message: fmt.Sprintf("声明帧数为 %d，实际解析到 %d 帧", document.FrameMax, len(frames))})
	}
	for index, frame := range frames {
		if frame.Index != int32(index) {
			issues = append(issues, Issue{Severity: IssueError, Line: frameLine(blocks, frame.Index), Section: fmt.Sprintf("FRAME%03d", frame.Index), Message: fmt.Sprintf("帧编号不连续，期望 %d，实际 %d", index, frame.Index)})
		}
	}

	document.Issues = issues
	document.Valid = !hasErrors(issues) && len(frames) > 0
	return document
}

func parseSections(text string) []section {
	lines := strings.Split(text, "\n")
	sections := make([]section, 0)
	var current *section
	for lineNumber, raw := range lines {
		line := stripComment(strings.TrimSuffix(raw, "\r"))
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			end := strings.IndexByte(trimmed, ']')
			if end >= 0 {
				name := normalizeSection(trimmed[1:end])
				if strings.HasPrefix(name, "/") {
					current = nil
					continue
				}
				sections = append(sections, section{name: name, line: lineNumber + 1, values: make([]token, 0)})
				current = &sections[len(sections)-1]
				rest := strings.TrimSpace(trimmed[end+1:])
				if rest != "" {
					current.values = append(current.values, tokenize(rest, lineNumber+1)...)
				}
				continue
			}
		}
		if current != nil {
			current.values = append(current.values, tokenize(trimmed, lineNumber+1)...)
		}
	}
	return sections
}

func tokenize(value string, line int) []token {
	result := make([]token, 0, 2)
	for index := 0; index < len(value); {
		for index < len(value) && isSpace(value[index]) {
			index++
		}
		if index >= len(value) {
			break
		}
		if value[index] == '`' {
			index++
			var builder strings.Builder
			for index < len(value) {
				if value[index] != '`' {
					builder.WriteByte(value[index])
					index++
					continue
				}
				if index+1 < len(value) && value[index+1] == '`' {
					builder.WriteByte('`')
					index += 2
					continue
				}
				index++
				break
			}
			result = append(result, token{value: builder.String(), line: line})
			continue
		}
		start := index
		for index < len(value) && !isSpace(value[index]) {
			index++
		}
		result = append(result, token{value: value[start:index], line: line})
	}
	return result
}

func stripComment(value string) string {
	inBacktick := false
	for index := 0; index < len(value); index++ {
		switch value[index] {
		case '`':
			if inBacktick && index+1 < len(value) && value[index+1] == '`' {
				index++
				continue
			}
			inBacktick = !inBacktick
		case '#':
			if !inBacktick {
				return value[:index]
			}
		}
	}
	return value
}

func normalizeSection(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func isGlobalSection(name string) bool {
	switch name {
	case "loop", "shadow", "frame max":
		return true
	default:
		return false
	}
}

func isFramePart(name string) bool {
	switch name {
	case "image", "image pos", "delay":
		return true
	default:
		return false
	}
}

func parseFrameName(name string) (int32, bool) {
	if !strings.HasPrefix(name, "frame") || name == "frame max" {
		return 0, false
	}
	number := strings.TrimPrefix(name, "frame")
	if number == "" {
		return 0, false
	}
	value, err := strconv.ParseInt(number, 10, 32)
	return int32(value), err == nil && value >= 0
}

func parseBooleanField(target *bool, values []section, name string, issues *[]Issue) {
	if len(values) == 0 {
		return
	}
	field := values[len(values)-1]
	if len(field.values) == 0 {
		*issues = append(*issues, Issue{Severity: IssueWarning, Line: field.line, Section: name, Message: fmt.Sprintf("[%s] 缺少数值，使用 0", name)})
		return
	}
	value, ok := parseInt(field.values[0])
	if !ok {
		*issues = append(*issues, Issue{Severity: IssueWarning, Line: field.values[0].line, Section: name, Message: fmt.Sprintf("[%s] 必须是整数，使用 0", name)})
		return
	}
	*target = value != 0
	if value != 0 && value != 1 {
		*issues = append(*issues, Issue{Severity: IssueWarning, Line: field.values[0].line, Section: name, Message: fmt.Sprintf("[%s] 只建议使用 0 或 1", name)})
	}
	if len(values) > 1 {
		*issues = append(*issues, Issue{Severity: IssueWarning, Line: field.line, Section: name, Message: fmt.Sprintf("重复的 [%s]，使用最后一个值", name)})
	}
}

func parseNonNegativeInt(value section, name string, issues *[]Issue, fallback int32) int32 {
	if len(value.values) == 0 {
		*issues = append(*issues, Issue{Severity: IssueError, Line: value.line, Section: name, Message: fmt.Sprintf("[%s] 缺少数值", name)})
		return fallback
	}
	parsed, ok := parseInt(value.values[0])
	if !ok || parsed < 0 {
		*issues = append(*issues, Issue{Severity: IssueError, Line: value.values[0].line, Section: name, Message: fmt.Sprintf("[%s] 必须是非负整数", name)})
		return fallback
	}
	return parsed
}

func parseImage(value section, issues *[]Issue, frameName string) (Layer, bool) {
	if len(value.values) == 0 || strings.TrimSpace(value.values[0].value) == "" {
		*issues = append(*issues, Issue{Severity: IssueWarning, Line: value.line, Section: frameName, Message: "[IMAGE] 图片引用为空，此帧按空图片处理"})
		return Layer{Index: -1}, true
	}
	if len(value.values) < 2 {
		*issues = append(*issues, Issue{Severity: IssueError, Line: value.line, Section: frameName, Message: "[IMAGE] 需要图片路径和索引"})
		return Layer{}, false
	}
	index, ok := parseInt(value.values[1])
	if !ok || index < 0 {
		*issues = append(*issues, Issue{Severity: IssueError, Line: value.values[1].line, Section: "IMAGE", Message: "图片索引必须是非负整数"})
		return Layer{}, false
	}
	return Layer{Path: value.values[0].value, Index: index}, true
}

func parseInt(value token) (int32, bool) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value.value), 10, 32)
	return int32(parsed), err == nil
}

func frameMaxLine(values []section) int {
	if len(values) == 0 {
		return 1
	}
	return values[len(values)-1].line
}

func frameLine(blocks []frameBlock, index int32) int {
	for _, block := range blocks {
		frameIndex, ok := parseFrameName(block.section.name)
		if ok && frameIndex == index {
			return block.section.line
		}
	}
	return 1
}

func hasErrors(issues []Issue) bool {
	for _, issue := range issues {
		if issue.Severity == IssueError {
			return true
		}
	}
	return false
}

func isSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n' || value == '\v' || value == '\f'
}
