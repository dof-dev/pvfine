package services

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"pvfine/internal/pvf"
)

// SkillTreeRegistration retains the job token: skill IDs are local to this job.
type SkillTreeRegistration struct {
	Job  string `json:"job"`
	Kind string `json:"kind"`
	Path string `json:"path"`
}
type SkillTreeNode struct {
	SourceStart    int             `json:"sourceStart"`
	SourceClose    int             `json:"sourceClose"`
	PositionTokens []SkillNumber   `json:"positionTokens"`
	NextTokens     []SkillNumber   `json:"nextTokens"`
	ID             int             `json:"id"`
	X              int             `json:"x"`
	Y              int             `json:"y"`
	PositionKind   string          `json:"positionKind"`
	Position       []int           `json:"position"`
	Next           []int           `json:"next"`
	Name           string          `json:"name"`
	Explain        string          `json:"explain"`
	Path           string          `json:"path"`
	FileIndex      int32           `json:"fileIndex"`
	Icon           *ImageReference `json:"icon"`
	RequiredLevel  *int            `json:"requiredLevel"`
}
type SkillTreeBranch struct {
	InsertOffset int             `json:"insertOffset"`
	Job          string          `json:"job"`
	Name         string          `json:"name"`
	Nodes        []SkillTreeNode `json:"nodes"`
}
type SkillTreeDocument struct {
	Text      string            `json:"text"`
	FileIndex int32             `json:"fileIndex"`
	Path      string            `json:"path"`
	Common    []SkillTreeNode   `json:"common"`
	Branches  []SkillTreeBranch `json:"branches"`
	Issues    []string          `json:"issues"`
}

func skillTreeText(a *pvf.Archive, p string) (string, error) {
	i, ok := a.Find(p)
	if !ok {
		return "", fmt.Errorf("找不到文件：%s", p)
	}
	return a.Text(i)
}

func skillTreeRegistrations(a *pvf.Archive) ([]SkillTreeRegistration, error) {
	result := []SkillTreeRegistration{}
	var failures []string
	for _, kind := range []string{"sp", "tp"} {
		p := "clientonly/skillshoptree" + kind + "index.co"
		text, err := skillTreeText(a, p)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		values := []string{}
		for _, e := range pvf.ParseScriptView(text).Elements {
			if e.Kind == pvf.ScriptElementToken && e.Section == "skill tree" {
				values = append(values, e.Value)
			}
		}
		if len(values)%2 != 0 {
			return nil, fmt.Errorf("%s 的注册记录不完整", p)
		}
		for i := 0; i+1 < len(values); i += 2 {
			result = append(result, SkillTreeRegistration{Job: values[i], Kind: kind, Path: path.Join("clientonly", strings.ReplaceAll(values[i+1], "\\", "/"))})
		}
	}
	if len(result) == 0 && len(failures) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(failures, "；"))
	}
	return result, nil
}

func (s *FileGUIService) ListSkillTrees() ([]SkillTreeRegistration, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if s.c.archive == nil {
		return nil, ErrNoArchive
	}
	return skillTreeRegistrations(s.c.archive)
}

// ReadSkillTree resolves only registered trees, using the current archive overlay.
func (s *FileGUIService) ReadSkillTree(job, kind string) (*SkillTreeDocument, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	return s.readSkillTreeLocked(job, kind, nil)
}

// ReadSkillTreeDraft projects the editor draft without changing the archive.
func (s *FileGUIService) ReadSkillTreeDraft(job, kind, text string) (*SkillTreeDocument, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if len(text) > int(s.c.editableByteLimit()) {
		return nil, fmt.Errorf("技能树文本过大")
	}
	return s.readSkillTreeLocked(job, kind, &text)
}

func (s *FileGUIService) readSkillTreeLocked(job, kind string, draft *string) (*SkillTreeDocument, error) {
	a := s.c.archive
	if a == nil {
		return nil, ErrNoArchive
	}
	registrations, err := skillTreeRegistrations(a)
	if err != nil {
		return nil, err
	}
	var p string
	for _, r := range registrations {
		if r.Job == job && r.Kind == kind {
			p = r.Path
			break
		}
	}
	if p == "" {
		return nil, fmt.Errorf("该职业未注册 %s 技能树", kind)
	}
	text, err := skillTreeText(a, p)
	if err != nil {
		return nil, err
	}
	if draft != nil {
		text = *draft
	}
	doc := parseSkillTree(text)
	// Use the archive identity, not the registration spelling (often mixed case).
	treeIndex, _ := a.Find(p) // skillTreeText above already validated this lookup.
	doc.FileIndex = treeIndex
	doc.Path = a.Path(treeIndex)
	paths, err := skillTreeSkillPaths(a, job)
	if err != nil {
		return nil, err
	}
	cache := map[int]SkillTreeNode{}
	resolve := func(n *SkillTreeNode) {
		meta, exists := cache[n.ID]
		if !exists {
			meta = SkillTreeNode{ID: n.ID, Name: fmt.Sprintf("技能 #%d", n.ID), FileIndex: -1}
			meta.Path = paths[n.ID]
			if idx, found := a.Find(meta.Path); found && meta.Path != "" {
				meta.FileIndex = idx
				if body, err := a.Text(idx); err == nil {
					meta.Explain = normalizeExplain(resolvePreviewDescription(a, firstSectionValue(body, "explain")))
					if level, err := strconv.Atoi(firstSectionValue(body, "required level")); err == nil {
						meta.RequiredLevel = &level
					}
				} else {
					doc.Issues = append(doc.Issues, fmt.Sprintf("技能 #%d 描述和学习等级读取失败：%s", n.ID, err))
				}
				if m, e := a.ScriptMetadata(idx); e == nil {
					if m.HasName {
						meta.Name = resolvePreviewText(a, m.Name)
					}
					meta.Icon = imageReferenceFromPVF(m.Icon)
				} else {
					doc.Issues = append(doc.Issues, fmt.Sprintf("技能 #%d 信息读取失败：%s", n.ID, e))
				}
			} else {
				doc.Issues = append(doc.Issues, fmt.Sprintf("职业 %s 的技能 #%d 未找到定义", job, n.ID))
			}
			cache[n.ID] = meta
		}
		n.Name, n.Path, n.FileIndex, n.Icon, n.RequiredLevel, n.Explain = meta.Name, meta.Path, meta.FileIndex, meta.Icon, meta.RequiredLevel, meta.Explain
	}
	commonText, e := skillTreeText(a, "clientonly/commonskilllist.co")
	if e != nil {
		doc.Issues = append(doc.Issues, e.Error())
	} else {
		for _, id := range parseCommonSkillIDs(commonText, job) {
			n := SkillTreeNode{ID: id, SourceStart: -1, Next: []int{}, Position: []int{}, PositionTokens: []SkillNumber{}}
			resolve(&n)
			doc.Common = append(doc.Common, n)
		}
	}
	for bi := range doc.Branches {
		b := &doc.Branches[bi]
		if b.Job != job {
			continue
		}
		ids := map[int]bool{}
		for ni := range b.Nodes {
			n := &b.Nodes[ni]
			resolve(n)
			ids[n.ID] = true
		}
		for _, n := range b.Nodes {
			for _, id := range n.Next {
				if !ids[id] {
					doc.Issues = append(doc.Issues, fmt.Sprintf("%s：技能 #%d 的后继 #%d 不在当前分支", b.Name, n.ID, id))
				}
			}
		}
	}
	filtered := []SkillTreeBranch{}
	for _, b := range doc.Branches {
		if b.Job == job {
			filtered = append(filtered, b)
		}
	}
	doc.Branches = filtered
	return doc, nil
}

func parseCommonSkillIDs(text, job string) []int {
	ids := []int{}
	excluded := map[int]bool{}
	matching := false
	for _, e := range pvf.ParseScriptView(text).Elements {
		if e.Kind != pvf.ScriptElementToken {
			continue
		}
		if e.Section == "common skill" {
			if id, err := strconv.Atoi(e.Value); err == nil {
				ids = append(ids, id)
			}
		}
		if e.Section == "info" {
			if e.Index == 0 {
				matching = e.Value == job
			} else if matching {
				if id, err := strconv.Atoi(e.Value); err == nil {
					excluded[id] = true
				}
			}
		}
	}
	result := []int{}
	for _, id := range ids {
		if !excluded[id] {
			result = append(result, id)
		}
	}
	return result
}

func parseSkillTree(text string) *SkillTreeDocument {
	doc := &SkillTreeDocument{Text: text, Common: []SkillTreeNode{}, Branches: []SkillTreeBranch{}, Issues: []string{}}
	closingOffsets := pvf.ScriptSectionClosingOffsets(text, "character job")
	skillClosingOffsets := pvf.ScriptSectionClosingOffsets(text, "skill info")
	var branch *SkillTreeBranch
	var node *SkillTreeNode
	for _, e := range pvf.ParseScriptView(text).Elements {
		if e.Kind == pvf.ScriptElementSection {
			if e.Section == "character job" {
				offset, closed := closingOffsets[e.Start]
				if !closed {
					offset = -1
				}
				doc.Branches = append(doc.Branches, SkillTreeBranch{InsertOffset: offset, Nodes: []SkillTreeNode{}})
				branch = &doc.Branches[len(doc.Branches)-1]
				node = nil
			}
			if e.Section == "skill info" && branch != nil {
				closeOffset, closed := skillClosingOffsets[e.Start]
				if !closed {
					closeOffset = -1
				}
				branch.Nodes = append(branch.Nodes, SkillTreeNode{SourceStart: e.Start, SourceClose: closeOffset, ID: -1, FileIndex: -1, Position: []int{}, PositionTokens: []SkillNumber{}, NextTokens: []SkillNumber{}, Next: []int{}})
				node = &branch.Nodes[len(branch.Nodes)-1]
			}
			continue
		}
		if branch == nil {
			continue
		}
		if e.Section == "character job" {
			if e.Index == 0 {
				branch.Job = e.Value
			}
			if e.Index == 1 {
				branch.Name = e.Value
			}
			continue
		}
		if node == nil {
			continue
		}
		v, err := strconv.Atoi(e.Value)
		if err != nil {
			continue
		}
		switch e.Section {
		case "index":
			node.ID = v
		case "next skill":
			node.Next = append(node.Next, v)
			node.NextTokens = append(node.NextTokens, SkillNumber{Value: float64(v), Start: e.Start, End: e.End, TokenType: e.TokenType})
		case "cell pos", "icon pos":
			if e.Index == 0 {
				node.PositionKind = e.Section
				node.Position = []int{v}
				node.PositionTokens = []SkillNumber{}
			} else if e.Index == 1 {
				node.Position = append(node.Position, v)
			}
			if e.Index <= 1 {
				node.PositionTokens = append(node.PositionTokens, SkillNumber{Value: float64(v), Start: e.Start, End: e.End, TokenType: e.TokenType})
			}
		}
	}
	for bi := range doc.Branches {
		for ni := range doc.Branches[bi].Nodes {
			n := &doc.Branches[bi].Nodes[ni]
			if n.ID < 0 || len(n.Position) != 2 || n.Position[0] < 0 || n.Position[1] < 0 {
				doc.Issues = append(doc.Issues, fmt.Sprintf("%s：技能 #%d 的编号或坐标无效，未渲染", doc.Branches[bi].Name, n.ID))
				continue
			}
			n.X, n.Y = n.Position[0], n.Position[1]
			// 110US mixes both formats within one branch, with half-cell icon offsets.
			if n.PositionKind == "cell pos" {
				n.X *= 47
				n.Y *= 67
			}
		}
		valid := []SkillTreeNode{}
		for _, n := range doc.Branches[bi].Nodes {
			if n.ID >= 0 && len(n.Position) == 2 && n.X >= 0 && n.Y >= 0 && n.Position[0] >= 0 && n.Position[1] >= 0 {
				valid = append(valid, n)
			}
		}
		doc.Branches[bi].Nodes = valid
	}
	return doc
}

func skillTreeSkillPaths(a *pvf.Archive, job string) (map[int]string, error) {
	// Discover the job list through skilllist.lst, including newer jobs.
	listIndex, ok := a.Find("skill/skilllist.lst")
	if !ok {
		return nil, fmt.Errorf("找不到职业技能列表 skill/skilllist.lst")
	}
	lists, err := a.ListPairs(listIndex)
	if err != nil {
		return nil, err
	}
	jobKey := strings.ReplaceAll(strings.Trim(job, "[]"), " ", "")
	if jobKey == "demonicswordman" {
		jobKey = "dsswordman"
	}
	var listPath string
	for _, pair := range lists {
		key := strings.ToLower(strings.TrimSuffix(path.Base(strings.ReplaceAll(pair.Path, "\\", "/")), path.Ext(pair.Path)))
		key = strings.TrimSuffix(key, "skill")
		if key == jobKey {
			listPath = path.Join("skill", strings.ReplaceAll(pair.Path, "\\", "/"))
			break
		}
	}
	if listPath == "" {
		return nil, fmt.Errorf("未找到职业 %s 对应的技能列表", job)
	}
	li, ok := a.Find(listPath)
	if !ok {
		return nil, fmt.Errorf("找不到职业技能列表：%s", listPath)
	}
	pairs, err := a.ListPairs(li)
	if err != nil {
		return nil, err
	}
	paths := map[int]string{}
	for _, pair := range pairs {
		if id, err := strconv.Atoi(pair.ID); err == nil {
			paths[id] = path.Join("skill", strings.ReplaceAll(pair.Path, "\\", "/"))
		}
	}
	return paths, nil
}

type SkillTreeSkill struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	FileIndex int32  `json:"fileIndex"`
}

// ListSkillTreeSkills lists registered IDs in the current job, including common skills.
func (s *FileGUIService) ListSkillTreeSkills(job string) ([]SkillTreeSkill, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	a := s.c.archive
	if a == nil {
		return nil, ErrNoArchive
	}
	paths, err := skillTreeSkillPaths(a, job)
	if err != nil {
		return nil, err
	}
	result := []SkillTreeSkill{}
	for id, p := range paths {
		index, found := a.Find(p)
		if !found {
			continue
		}
		name := path.Base(p)
		if meta, err := a.ScriptMetadata(index); err == nil && meta.HasName {
			name = resolvePreviewText(a, meta.Name)
		}
		result = append(result, SkillTreeSkill{ID: id, Name: name, Path: a.Path(index), FileIndex: index})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}
