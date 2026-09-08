package services

import annotationrules "pvfine/internal/annotations"

type AnnotationReloadResult struct {
	RuleCount     int `json:"ruleCount"`
	RelationCount int `json:"relationCount"`
}

type AnnotationService struct {
	c       *core
	path    string
	initErr error
}

func NewAnnotationService(c *core) *AnnotationService {
	if path, ok := annotationrules.FindSourcePath(); ok {
		return &AnnotationService{c: c, path: path}
	}
	path, err := annotationrules.RuntimePath()
	if err != nil {
		return &AnnotationService{c: c, initErr: err}
	}
	return &AnnotationService{c: c, path: path}
}

func newAnnotationService(c *core, path string) *AnnotationService {
	return &AnnotationService{c: c, path: path}
}

// ReloadRules validates the on-disk rules before atomically replacing the
// active engine. Invalid files leave the current engine untouched.
func (s *AnnotationService) ReloadRules() (AnnotationReloadResult, error) {
	if s.initErr != nil {
		return AnnotationReloadResult{}, s.initErr
	}
	engine, err := annotationrules.LoadFile(s.path)
	if err != nil {
		return AnnotationReloadResult{}, err
	}
	document := engine.Document()

	s.c.mu.Lock()
	s.c.annotationEngine = engine
	s.c.annotationErr = nil
	s.c.annotationRelations = make(map[string]map[string]*relationTarget)
	s.c.editorAnnotation = editorAnnotationCache{}
	if s.c.archive != nil {
		s.c.pathAnnotations = buildPathAnnotations(engine, s.c.dirChildren)
	}
	s.c.mu.Unlock()

	result := AnnotationReloadResult{
		RuleCount:     len(document.Rules),
		RelationCount: len(document.Relations),
	}
	emitEvent("annotations:reloaded", result)
	return result, nil
}
