package services

import renderingrules "pvfine/internal/rendering"

// RenderingReloadResult describes the renderer that was activated.
type RenderingReloadResult struct {
	RuleCount int `json:"ruleCount"`
}

// RenderingService manages the user-facing script rendering configuration.
type RenderingService struct {
	c       *core
	path    string
	initErr error
}

// NewRenderingService creates the Wails-facing rendering configuration
// service. Development builds prefer the repository config; packaged builds
// use the per-user runtime mirror.
func NewRenderingService(c *core) *RenderingService {
	if path, ok := renderingrules.FindSourcePath(); ok {
		return &RenderingService{c: c, path: path}
	}
	path, err := renderingrules.RuntimePath()
	if err != nil {
		return &RenderingService{c: c, initErr: err}
	}
	return &RenderingService{c: c, path: path}
}

func newRenderingService(c *core, path string) *RenderingService {
	return &RenderingService{c: c, path: path}
}

// ReloadRules validates the on-disk rendering rules before replacing the
// active renderer. Invalid files leave the current renderer untouched.
func (s *RenderingService) ReloadRules() (RenderingReloadResult, error) {
	if s.initErr != nil {
		return RenderingReloadResult{}, s.initErr
	}
	engine, err := renderingrules.LoadFile(s.path)
	if err != nil {
		return RenderingReloadResult{}, err
	}

	s.c.mu.Lock()
	s.c.renderingEngine = engine
	s.c.renderingErr = nil
	if s.c.archive != nil {
		s.c.bindRenderingEngineLocked(s.c.archive)
	}
	if s.c.versionBaseArchive != nil {
		s.c.versionBaseArchive.SetScriptRenderer(engine)
	}
	s.c.editorAnnotation = editorAnnotationCache{}
	s.c.batchRevision++
	s.c.batchPlan = nil
	s.c.invalidateScriptLocked()
	s.c.mu.Unlock()

	result := RenderingReloadResult{RuleCount: len(engine.Document().Rules)}
	emitEvent("rendering:reloaded", result)
	return result, nil
}
