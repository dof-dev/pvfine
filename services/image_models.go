package services

// ImageReference is the PVF path plus zero-based frame index stored in an
// [icon] or [field image] section.
type ImageReference struct {
	Path  string `json:"path"`
	Index int32  `json:"index"`
}

// ImageData is always a PNG data URL. The source NPK path is intentionally
// never exposed to the frontend.
type ImageData struct {
	DataURL string `json:"dataUrl"`
	Width   int32  `json:"width"`
	Height  int32  `json:"height"`
}

const (
	ImageIndexStateIdle     = "idle"
	ImageIndexStateBuilding = "building"
	ImageIndexStateReady    = "ready"
	ImageIndexStateError    = "error"
)

// ImageIndexStatus is emitted while the NPK directory is being scanned.
type ImageIndexStatus struct {
	State           string  `json:"state"`
	Stage           string  `json:"stage"`
	Directory       string  `json:"directory"`
	Done            int     `json:"done"`
	Total           int     `json:"total"`
	NPKFiles        int     `json:"npkFiles"`
	IMGFiles        int     `json:"imgFiles"`
	ImageCount      int     `json:"imageCount"`
	Skipped         int     `json:"skipped"`
	Duplicates      int     `json:"duplicates"`
	Error           string  `json:"error"`
	Generation      uint64  `json:"generation"`
	BuildDurationMs float64 `json:"buildDurationMs"`
}
