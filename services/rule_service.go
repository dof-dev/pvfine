package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	annotationrules "pvfine/internal/annotations"
	"pvfine/internal/buildmode"
	"pvfine/internal/ruleconfig"
)

// RuleService exposes only rule identities and contents, never arbitrary paths.
type RuleService struct{ c *core }

func NewRuleService(c *core) *RuleService { return &RuleService{c: c} }

type RuleTarget struct {
	Category  string `json:"category"`
	Path      string `json:"path"`
	Section   string `json:"section"`
	Directory bool   `json:"directory"`
}
type RuleQueryResult struct {
	Revision    string             `json:"revision"`
	Development bool               `json:"development"`
	Entries     []ruleconfig.Entry `json:"entries"`
	Relations   []string           `json:"relations"`
}

func (s *RuleService) StartupWarnings() []string { return append([]string{}, s.c.ruleWarnings...) }
func (s *RuleService) Query(target RuleTarget) (RuleQueryResult, error) {
	if s.c.ruleStore == nil {
		return RuleQueryResult{}, fmt.Errorf("规则存储不可用")
	}
	snap, err := s.c.ruleStore.Load(target.Category)
	if err != nil {
		return RuleQueryResult{}, err
	}
	s.c.mu.RLock()
	version := s.c.archive.ClientVersion()
	archiveOpen := s.c.archive != nil
	s.c.mu.RUnlock()
	result := RuleQueryResult{Revision: snap.Revision, Development: snap.Development, Entries: []ruleconfig.Entry{}, Relations: []string{}}
	ann, renderer, err := snap.Engines(target.Category)
	if err != nil {
		return result, err
	}
	applicable := map[string]bool{}
	activeFields := map[string]bool{}
	if ann != nil {
		if archiveOpen {
			ann = ann.ForVersion(version)
		}
		applicable = ann.ApplicableRuleIDs(target.Path, target.Section, target.Directory)
		for _, field := range ann.Document().Fields {
			if field.Preview != nil {
				activeFields[field.ID] = true
			}
		}
	}
	fields := map[string]json.RawMessage{}
	for _, entry := range snap.Entries {
		if entry.Kind == "field" {
			fields[entry.ID] = json.RawMessage(entry.JSON)
		}
	}
	for _, entry := range snap.Entries {
		var r struct {
			Field       string                     `json:"field"`
			PVFVersions []string                   `json:"pvfVersions"`
			Match       annotationrules.MatchSpec  `json:"match"`
			Target      annotationrules.TargetSpec `json:"target"`
		}
		if err := json.Unmarshal([]byte(entry.JSON), &r); err != nil {
			return result, err
		}
		if r.Field != "" {
			var f struct {
				Match  annotationrules.MatchSpec  `json:"match"`
				Target annotationrules.TargetSpec `json:"target"`
			}
			_ = json.Unmarshal(fields[r.Field], &f)
			if r.Target.Kind == "" {
				r.Target = f.Target
			}
			if r.Match.Glob == "" && len(r.Match.Extensions) == 0 {
				r.Match = f.Match
			}
		}
		// A directory rendering entry describes descendant files; use the rule's
		// glob plus a representative child for extension-only rules.
		matches := ruleconfig.Matches(r.Match.Glob, target.Path, r.Match.Extensions)
		if target.Directory && target.Category == "rendering" {
			matches = false
			candidates := []string{target.Path + "/example"}
			for _, extension := range r.Match.Extensions {
				candidates = append(candidates, target.Path+"/example"+extension)
			}
			for _, candidate := range candidates {
				matches = matches || ruleconfig.Matches(r.Match.Glob, candidate, r.Match.Extensions)
			}
			prefix := strings.TrimSuffix(target.Path, "/") + "/"
			matches = matches || strings.HasPrefix(strings.ToLower(r.Match.Glob), strings.ToLower(prefix))
		}
		if !matches {
			continue
		}
		if target.Section != "" {
			if !strings.EqualFold(r.Target.Section, target.Section) {
				continue
			}
		} else if target.Category == "annotations" && r.Target.Kind != "path" {
			continue
		}
		if target.Directory && target.Category == "annotations" && len(r.Match.Extensions) > 0 {
			continue
		}
		if len(r.PVFVersions) > 0 && version != "" {
			ok := false
			for _, v := range r.PVFVersions {
				ok = ok || v == version
			}
			entry.Effective = entry.Effective && ok
		}
		if ann != nil {
			id := entry.ID
			if entry.Kind == "field" {
				id = "field:" + id
			}
			entry.Effective = entry.Effective && (applicable[id] || (entry.Kind == "field" && activeFields[entry.ID]))
		}
		if renderer != nil && !target.Directory {
			chosen := renderer.EffectiveRuleID(target.Path, r.Target.Section, r.Target.Kind)
			entry.Effective = entry.Effective && chosen == entry.ID
		}
		result.Entries = append(result.Entries, entry)
	}
	if ann != nil {
		for name := range ann.Document().Relations {
			result.Relations = append(result.Relations, name)
		}
	}
	sort.Strings(result.Relations)
	return result, nil
}

// Apply validates, saves and activates a rule operation. A stale revision is rejected.
func (s *RuleService) Apply(m ruleconfig.Mutation) error {
	if s.c.ruleStore == nil {
		return fmt.Errorf("规则存储不可用")
	}
	// Serialize disk mutation and engine activation, including concurrent reloads.
	s.c.ruleMu.Lock()
	defer s.c.ruleMu.Unlock()
	snap, err := s.c.ruleStore.Mutate(m)
	if err != nil {
		return err
	}
	return s.activate(m.Category, snap)
}
func (s *RuleService) reload(category string) error {
	s.c.ruleMu.Lock()
	defer s.c.ruleMu.Unlock()
	snap, err := s.c.ruleStore.Load(category)
	if err != nil {
		return err
	}
	return s.activate(category, snap)
}
func (s *RuleService) activate(category string, snap *ruleconfig.Snapshot) error {
	ann, renderer, err := snap.Engines(category)
	if err != nil {
		return err
	}
	c := s.c
	c.mu.Lock()
	archiveOpen := c.archive != nil
	specChanged := false
	if ann != nil {
		old := searchIndexSpecFingerprint(c.searchableListSpecsLocked())
		c.annotationSnapshot = snap
		c.annotationEngine = ann.ForVersion(c.archive.ClientVersion())
		c.annotationErr = nil
		c.annotationRelations = make(map[string]map[string]*relationTarget)
		c.pathAnnotations = nil
		if archiveOpen {
			c.pathAnnotations = buildPathAnnotations(c.annotationEngine, c.dirChildren)
		}
		specChanged = old != searchIndexSpecFingerprint(c.searchableListSpecsLocked())
		if archiveOpen && specChanged {
			c.markAdvancedSearchStaleLocked(c.archive)
		}
		if archiveOpen && specChanged && c.diskIndex != nil {
			c.diskIndex.dirty = true
			c.diskIndex.semanticEpoch++
		}
	}
	if renderer != nil {
		c.renderingEngine = renderer
		c.renderingErr = nil
		if archiveOpen {
			c.bindRenderingEngineLocked(c.archive)
		}
		if c.versionBaseArchive != nil {
			c.versionBaseArchive.SetScriptRenderer(renderer)
		}
		c.batchRevision++
		c.batchPlan = nil
		c.invalidateScriptLocked()
	}
	c.editorAnnotation = editorAnnotationCache{}
	c.mu.Unlock()
	if archiveOpen && specChanged {
		c.startSearchIndex()
	}
	if ann != nil {
		doc := ann.Document()
		emitEvent("annotations:reloaded", AnnotationReloadResult{RuleCount: len(doc.Rules), RelationCount: len(doc.Relations)})
	}
	if renderer != nil {
		emitEvent("rendering:reloaded", RenderingReloadResult{RuleCount: len(renderer.Document().Rules)})
	}
	return nil
}
func (c *core) initializeRuleStore() {
	configDir, err := os.UserConfigDir()
	if err != nil {
		c.ruleWarnings = append(c.ruleWarnings, err.Error())
		return
	}
	sourceDir := ""
	if buildmode.Development {
		if source, ok := annotationrules.FindSourcePath(); ok {
			sourceDir = filepath.Dir(source)
		}
	}
	c.loadInitialRules(ruleconfig.New(buildmode.Development, sourceDir, filepath.Join(configDir, "pvfine")))
}

func (c *core) loadInitialRules(store *ruleconfig.Store) {
	c.ruleStore = store
	for _, category := range []string{"annotations", "rendering"} {
		if err := c.ruleStore.Migrate(category); err != nil {
			c.ruleWarnings = append(c.ruleWarnings, err.Error())
			continue
		}
		snap, err := c.ruleStore.Load(category)
		if err != nil {
			c.ruleWarnings = append(c.ruleWarnings, err.Error())
			continue
		}
		ann, renderer, err := snap.Engines(category)
		if err != nil {
			c.ruleWarnings = append(c.ruleWarnings, err.Error())
			continue
		}
		if ann != nil {
			c.annotationSnapshot = snap
			c.annotationEngine = ann
			c.annotationErr = nil
		}
		if renderer != nil {
			c.renderingEngine = renderer
			c.renderingErr = nil
		}
	}
}
