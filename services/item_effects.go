package services

import (
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"

	"pvfine/internal/pvf"
)

// SearchEffectResources applies the resource scope before pagination. Particle
// paths are discovered across the archive, including monster/ and equipment/.
func (s *ArchiveService) SearchEffectResources(kind, query string, cursor, limit int) (*SearchResult, error) {
	scopes := map[string]string{"object": "passiveobject", "appendage": "appendage", "monster": "monster"}
	if scope, ok := scopes[kind]; ok {
		if strings.TrimSpace(query) == "" {
			query = "*"
		}
		return s.searchScopedRecords(query, cursor, limit, false, false, "", scope, true)
	}
	if kind == "apc" {
		return s.searchEffectAPCs(query, cursor, limit)
	}
	if kind != "particle" {
		return nil, fmt.Errorf("不支持的效果资源类型")
	}
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	if s.c.archive == nil {
		return nil, ErrNoArchive
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	if cursor < 0 {
		cursor = 0
	}
	result := &SearchResult{Hits: []*SearchHit{}, NextCursor: -1}
	query = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(query, "\\", "/")))
	for i := cursor; i < int(s.c.archive.FileCount()); i++ {
		p := s.c.archive.Path(int32(i))
		if _, ok := s.c.archive.Find(p); !ok || !strings.HasSuffix(strings.ToLower(p), ".ptl") || !strings.Contains(strings.ToLower(p), query) {
			continue
		}
		if len(result.Hits) == limit {
			result.NextCursor = int(i)
			break
		}
		result.Hits = append(result.Hits, &SearchHit{FileIndex: int32(i), Path: p, Name: path.Base(p)})
	}
	return result, nil
}

type EffectParticle struct {
	Life         float64   `json:"life"`
	Frequency    float64   `json:"frequency"`
	Maximum      int       `json:"maximum"`
	MaximumTime  float64   `json:"maximumTime"`
	Gravity      float64   `json:"gravity"`
	Position     []float64 `json:"position"`
	Area         []float64 `json:"area"`
	Alpha        []float64 `json:"alpha"`
	Velocity     []float64 `json:"velocity"`
	Acceleration []float64 `json:"acceleration"`
}
type ItemEffectLayer struct {
	Path      string              `json:"path"`
	Animation *AniPreviewDocument `json:"animation"`
	Particle  *EffectParticle     `json:"particle,omitempty"`
}
type ItemEffectPreview struct {
	ObjectPath string            `json:"objectPath"`
	Layers     []ItemEffectLayer `json:"layers"`
	Issues     []string          `json:"issues"`
}

// ReadItemEffectPreview resolves live LST references, relative paths and editor
// drafts. It reads only PVF resources; sprite decoding remains in ImageService.
func (s *FileGUIService) ReadItemEffectPreview(fileIndex int32, objectID, particle string, drafts []ShopDraft) (*ItemEffectPreview, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	a := s.c.archive
	if a == nil {
		return nil, ErrNoArchive
	}
	if fileIndex < 0 || fileIndex >= a.FileCount() {
		return nil, fmt.Errorf("物品文件已变化")
	}
	pending := map[string]string{}
	for _, draft := range drafts {
		if draft.FileIndex >= 0 && draft.FileIndex < a.FileCount() && normalizeAnnotationPath(a.Path(draft.FileIndex)) == normalizeAnnotationPath(draft.Path) {
			pending[normalizeAnnotationPath(draft.Path)] = draft.Text
		}
	}
	read := func(p string) (string, bool) {
		if v, ok := pending[normalizeAnnotationPath(p)]; ok {
			return v, true
		}
		i, ok := a.Find(p)
		if !ok {
			return "", false
		}
		v, err := a.Text(i)
		return v, err == nil
	}
	result := &ItemEffectPreview{Layers: []ItemEffectLayer{}, Issues: []string{}}
	resolve := func(base, raw string) string {
		raw = strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
		candidates := []string{path.Join(path.Dir(base), raw), strings.TrimLeft(raw, "/")}
		if strings.HasPrefix(raw, "/") {
			candidates[0], candidates[1] = candidates[1], candidates[0]
		}
		for _, candidate := range candidates {
			if _, ok := a.Find(candidate); ok {
				return candidate
			}
		}
		return candidates[0]
	}
	animation := func(p string, particle *EffectParticle) {
		if len(result.Layers) >= 16 {
			return
		}
		if strings.HasSuffix(strings.ToLower(p), ".img") {
			result.Layers = append(result.Layers, ItemEffectLayer{Path: p, Particle: particle, Animation: &AniPreviewDocument{Valid: true, Loop: true, Frames: []AniPreviewFrame{{Delay: 100, Layers: []AniPreviewLayer{{Image: ImageReference{Path: p, Index: 0}}}}}}})
			return
		}
		text, ok := read(p)
		if !ok {
			result.Issues = append(result.Issues, "无法读取动画："+p)
			return
		}
		doc, err := NewPreviewService().ParseANI(text)
		if err != nil || doc == nil || !doc.Valid {
			result.Issues = append(result.Issues, "动画格式无法预览："+p)
			return
		}
		result.Layers = append(result.Layers, ItemEffectLayer{Path: p, Animation: doc, Particle: particle})
	}
	seenParticles := map[string]bool{}
	addParticle := func(p string) {
		if seenParticles[normalizeAnnotationPath(p)] || len(seenParticles) >= 12 {
			return
		}
		seenParticles[normalizeAnnotationPath(p)] = true
		text, ok := read(p)
		if !ok {
			result.Issues = append(result.Issues, "无法读取粒子："+p)
			return
		}
		view := effectScriptView(text)
		meta := parseEffectParticle(view)
		found := false
		for _, e := range view.Elements {
			if e.Kind != pvf.ScriptElementToken || e.Section != "object" {
				continue
			}
			ext := strings.ToLower(path.Ext(strings.ReplaceAll(e.Value, "\\", "/")))
			if ext == ".ani" || ext == ".img" {
				found = true
				animation(resolve(p, e.Value), meta)
			}
		}
		if !found {
			result.Issues = append(result.Issues, "粒子没有可绘制的 [object]，仅提供运动参数："+p)
		}
	}
	if objectID != "" {
		id, err := strconv.ParseUint(objectID, 10, 31)
		if err != nil {
			return nil, fmt.Errorf("对象 ID 无效")
		}
		objectID = strconv.FormatUint(id, 10)
		index, ok := a.FindList("passiveobject/passiveobject.lst")
		if !ok {
			result.Issues = append(result.Issues, "当前归档没有被动对象列表")
		} else {
			listPath := a.Path(index)
			listText, _ := read(listPath)
			tokens := []pvf.ScriptElement{}
			for _, e := range effectScriptView(listText).Elements {
				if e.Kind == pvf.ScriptElementToken {
					tokens = append(tokens, e)
				}
			}
			for i := 0; i+1 < len(tokens); i += 2 {
				if tokens[i].Value != objectID {
					continue
				}
				p, _, exists := findListTargetInArchive(a, listPath, tokens[i+1].Value)
				if exists {
					result.ObjectPath = p
				}
				break
			}
			if result.ObjectPath == "" {
				result.Issues = append(result.Issues, "对象 ID 未登记或目标文件不存在："+objectID)
			}
		}
		if result.ObjectPath != "" {
			text, ok := read(result.ObjectPath)
			if !ok {
				result.Issues = append(result.Issues, "无法读取对象："+result.ObjectPath)
			}
			view := effectScriptView(text)
			for _, e := range view.Elements {
				if e.Kind != pvf.ScriptElementToken {
					continue
				}
				ext := strings.ToLower(path.Ext(strings.ReplaceAll(e.Value, "\\", "/")))
				switch {
				case e.Section == "basic motion" && ext == ".ani":
					animation(resolve(result.ObjectPath, e.Value), nil)
				case e.Section == "basic action" && (ext == ".act" || ext == ".h"):
					act := resolve(result.ObjectPath, e.Value)
					actText, ok := read(act)
					if !ok {
						result.Issues = append(result.Issues, "无法读取动作："+act)
						continue
					}
					for _, value := range effectScriptView(actText).Elements {
						if value.Kind == pvf.ScriptElementToken && value.Section == "base ani" && strings.HasSuffix(strings.ToLower(value.Value), ".ani") {
							aniPath := resolve(act, value.Value)
							if _, ok := a.Find(aniPath); !ok {
								aniPath = resolve(result.ObjectPath, value.Value)
							}
							animation(aniPath, nil)
						}
					}
				case ext == ".ptl" && (e.Section == "string data" || e.Section == "file"):
					addParticle(resolve(result.ObjectPath, e.Value))
				}
			}
		}
	}
	if particle != "" {
		raw := strings.ReplaceAll(particle, "\\", "/")
		p := resolve(a.Path(fileIndex), raw)
		if !strings.Contains(raw, "/") {
			if _, ok := a.Find("equipment/character/particle/" + raw); ok {
				p = "equipment/character/particle/" + raw
			}
		}
		addParticle(p)
	}
	if len(result.Layers) == 0 && len(result.Issues) == 0 {
		result.Issues = append(result.Issues, "对象未声明可预览的基础动画或粒子")
	}
	return result, nil
}

func parseEffectParticle(view pvf.ScriptView) *EffectParticle {
	values := map[string][]float64{}
	axis := ""
	for _, e := range view.Elements {
		if e.Kind == pvf.ScriptElementSection && (strings.EqualFold(e.Section, "x axis") || strings.EqualFold(e.Section, "y axis") || strings.EqualFold(e.Section, "z axis")) {
			axis = strings.ToLower(e.Section)
		}
		if e.Kind != pvf.ScriptElementToken {
			continue
		}
		value, err := strconv.ParseFloat(e.Value, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		key := strings.ToLower(e.Section)
		if key == "move variable 1" {
			key = axis + ":" + key
		}
		values[key] = append(values[key], value)
	}
	first := func(key string, fallback float64) float64 {
		if v := values[key]; len(v) > 0 {
			return v[0]
		}
		return fallback
	}
	vector := func(key string, size int, fallback float64) []float64 {
		out := make([]float64, size)
		for i := range out {
			out[i] = fallback
			if i < len(values[key]) {
				out[i] = values[key][i]
			}
		}
		return out
	}
	life := math.Max(50, math.Min(first("life time", 1000), 60000))
	frequency := first("creation frequency", 100)
	maximum := first("maximum create number", 0)
	if maximum <= 0 {
		maximum = 8
		if frequency > 0 {
			maximum = life/frequency + 1
		}
	}
	alpha := vector("transparent value", 2, 100)
	if len(values["transparent value"]) == 1 {
		alpha[1] = alpha[0]
	}
	meta := &EffectParticle{Life: life, Frequency: frequency, Maximum: int(math.Max(1, math.Min(maximum, 64))), MaximumTime: first("maximum create time", 0), Gravity: first("gravity", 0), Position: vector("creation pos", 3, 0), Area: vector("create area size", 3, 0), Alpha: alpha, Velocity: make([]float64, 3), Acceleration: make([]float64, 3)}
	for i, axis := range []string{"x axis", "y axis", "z axis"} {
		v := values[axis+":move variable 1"]
		if len(v) > 0 {
			meta.Velocity[i] = v[0]
		}
		if len(v) > 1 {
			meta.Acceleration[i] = v[1]
		}
	}
	return meta
}

func effectScriptView(text string) pvf.ScriptView {
	view := pvf.ParseScriptView(text)
	for i := range view.Elements {
		view.Elements[i].Section = strings.ToLower(view.Elements[i].Section)
	}
	return view
}

// APC lists are not part of the general semantic index on all clients.
func (s *ArchiveService) searchEffectAPCs(query string, cursor, limit int) (*SearchResult, error) {
	s.c.mu.RLock()
	defer s.c.mu.RUnlock()
	a := s.c.archive
	if a == nil {
		return nil, ErrNoArchive
	}
	result := &SearchResult{Hits: []*SearchHit{}, NextCursor: -1}
	index, ok := a.FindList("aicharacter/aicharacter.lst")
	if !ok {
		return result, nil
	}
	pairs, err := a.ListPairs(index)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	if cursor < 0 {
		cursor = 0
	}
	q := strings.ToLower(strings.TrimSpace(query))
	matcher := newSearchMatcher(q, false)
	for i := cursor; i < len(pairs); i++ {
		pair := pairs[i]
		p, fileIndex, ok := findListTargetInArchive(a, a.Path(index), pair.Path)
		if !ok {
			continue
		}
		metadata, err := a.ScriptMetadata(fileIndex)
		name := ""
		if err == nil {
			name = resolvePreviewText(a, metadata.Name)
		}
		if q != "" && !matcher.match(strings.ToLower(pair.ID)) && !matcher.match(strings.ToLower(p)) && !matcher.match(strings.ToLower(name)) {
			continue
		}
		if len(result.Hits) == limit {
			result.NextCursor = i
			break
		}
		result.Hits = append(result.Hits, &SearchHit{ID: pair.ID, Path: p, FileIndex: fileIndex, Name: name})
	}
	return result, nil
}
