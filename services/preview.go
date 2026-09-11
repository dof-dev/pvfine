package services

import previewmodel "pvfine/internal/preview"

// PreviewService parses structured preview formats from the editor's current
// text. It intentionally does not read the archive so unsaved edits can be
// previewed without changing the archive overlay first.
type PreviewService struct {
	c *core
}

// PreviewIssue is a recoverable problem reported by a preview parser.
type PreviewIssue struct {
	Severity string `json:"severity"`
	Line     int32  `json:"line"`
	Section  string `json:"section,omitempty"`
	Message  string `json:"message"`
}

// AniPreviewLayer is one image layer in an ANI frame.
type AniPreviewLayer struct {
	Image ImageReference `json:"image"`
	X     int32          `json:"x"`
	Y     int32          `json:"y"`
}

// AniPreviewFrame is one decoded ANI frame.
type AniPreviewFrame struct {
	Index  int32             `json:"index"`
	Delay  int32             `json:"delayMs"`
	Layers []AniPreviewLayer `json:"layers"`
}

// AniPreviewDocument is the structured preview model returned for .ani text.
type AniPreviewDocument struct {
	Valid    bool              `json:"valid"`
	Loop     bool              `json:"loop"`
	Shadow   bool              `json:"shadow"`
	FrameMax int32             `json:"frameMax"`
	Frames   []AniPreviewFrame `json:"frames"`
	Issues   []PreviewIssue    `json:"issues"`
}

// NewPreviewService creates the Wails-facing preview service.
func NewPreviewService(cores ...*core) *PreviewService {
	service := &PreviewService{}
	if len(cores) > 0 {
		service.c = cores[0]
	}
	return service
}

// ParseANI parses the current editor text. Semantic and structural problems
// are returned in Issues so the caller can retain the last valid preview;
// the RPC error is reserved for service-level failures.
func (s *PreviewService) ParseANI(text string) (*AniPreviewDocument, error) {
	parsed := previewmodel.ParseANI(text)
	result := &AniPreviewDocument{
		Valid:    parsed.Valid,
		Loop:     parsed.Loop,
		Shadow:   parsed.Shadow,
		FrameMax: parsed.FrameMax,
		Frames:   make([]AniPreviewFrame, 0, len(parsed.Frames)),
		Issues:   make([]PreviewIssue, 0, len(parsed.Issues)),
	}
	for _, frame := range parsed.Frames {
		converted := AniPreviewFrame{
			Index:  frame.Index,
			Delay:  frame.Delay,
			Layers: make([]AniPreviewLayer, 0, len(frame.Layers)),
		}
		for _, layer := range frame.Layers {
			converted.Layers = append(converted.Layers, AniPreviewLayer{
				Image: ImageReference{Path: layer.Path, Index: layer.Index},
				X:     layer.X,
				Y:     layer.Y,
			})
		}
		result.Frames = append(result.Frames, converted)
	}
	for _, issue := range parsed.Issues {
		result.Issues = append(result.Issues, PreviewIssue{
			Severity: string(issue.Severity),
			Line:     int32(issue.Line),
			Section:  issue.Section,
			Message:  issue.Message,
		})
	}
	return result, nil
}
