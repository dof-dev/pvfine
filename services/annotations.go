package services

import (
	"fmt"
	"path"
	"strings"

	annotationrules "pvfine/internal/annotations"
	"pvfine/internal/pvf"
)

type EditorAnnotation struct {
	Start           int32           `json:"start"`
	End             int32           `json:"end"`
	Title           string          `json:"title"`
	Content         string          `json:"content"`
	Type            string          `json:"type"`
	TargetFileIndex int32           `json:"targetFileIndex"`
	RuleIDs         []string        `json:"ruleIds,omitempty"`
	Image           *ImageReference `json:"image,omitempty"`
	InlineImage     bool            `json:"inlineImage,omitempty"`
	Placeholder     *PlaceholderRef `json:"placeholder,omitempty"`
	Rarity          int32           `json:"rarity"`
}

// PlaceholderRef identifies the string-table entry a placeholder annotation
// resolves through, so the editor can offer to rewrite — or create — that text.
type PlaceholderRef struct {
	TableIndex int32  `json:"tableIndex"`
	Key        string `json:"key"`
	Fallback   bool   `json:"fallback,omitempty"`
	// Missing reports a placeholder no table answers yet: the editor offers to
	// create the entry, which is how a new file gets its display text.
	Missing bool `json:"missing,omitempty"`
}

// missingPlaceholderLabel is the tag shown for a `<table::key>` placeholder no
// string table answers yet; clicking it creates the entry.
const missingPlaceholderLabel = "未定义"

// TreeAnnotation is one annotation attached to a path in the explorer.
type TreeAnnotation struct {
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Type    string   `json:"type"`
	RuleIDs []string `json:"ruleIds,omitempty"`
}

type relationTarget struct {
	reference   annotationrules.Reference
	nameSection string
	listPath    string
	nameLoaded  bool
}

func buildPathAnnotations(engine *annotationrules.Engine, children map[string][]*TreeNode) map[string][]TreeAnnotation {
	// Disk-backed archives resolve annotations lazily. Preserve the nil cache
	// sentinel on reload instead of installing an empty, authoritative cache.
	if children == nil {
		return nil
	}
	result := make(map[string][]TreeAnnotation)
	if engine == nil {
		return result
	}
	for _, nodes := range children {
		for _, node := range nodes {
			matches := engine.AnnotatePath(node.Path, node.IsDir)
			if len(matches) == 0 {
				continue
			}
			annotations := make([]TreeAnnotation, 0, len(matches))
			for _, match := range matches {
				annotations = append(annotations, TreeAnnotation{
					Title: match.Title, Content: match.Content, Type: match.Type,
					RuleIDs: append([]string(nil), match.RuleIDs...),
				})
			}
			result[node.Path] = annotations
		}
	}
	return result
}

// annotationsForPathLocked keeps large archives from materializing one
// annotation slice for every path. The caller must hold c.mu.
func (c *core) annotationsForPathLocked(path string, directory ...bool) []TreeAnnotation {
	if c.pathAnnotations != nil {
		return cloneTreeAnnotations(c.pathAnnotations[path])
	}
	if c.annotationEngine == nil {
		return nil
	}
	isDir := len(directory) > 0 && directory[0]
	matches := c.annotationEngine.AnnotatePath(path, isDir)
	if len(matches) == 0 {
		return nil
	}
	result := make([]TreeAnnotation, 0, len(matches))
	for _, match := range matches {
		result = append(result, TreeAnnotation{Title: match.Title, Content: match.Content, Type: match.Type, RuleIDs: append([]string(nil), match.RuleIDs...)})
	}
	return result
}

func (c *core) annotationChainLocked(filePath string) map[string][]TreeAnnotation {
	result := make(map[string][]TreeAnnotation)
	current := filePath
	for current != "" {
		if annotations := c.annotationsForPathLocked(current); len(annotations) > 0 {
			result[current] = annotations
		}
		current, _ = splitParent(current)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func (c *core) editorAnnotationsLocked(index int32, text string) ([]EditorAnnotation, error) {
	if c.annotationErr != nil {
		return nil, c.annotationErr
	}
	if c.annotationEngine == nil || c.archive == nil {
		return nil, nil
	}
	if c.editorAnnotation.valid && c.editorAnnotation.fileIndex == index && c.editorAnnotation.text == text {
		return cloneEditorAnnotations(c.editorAnnotation.annotations), nil
	}
	filePath := c.archive.Path(index)
	view := pvf.ParseScriptViewWithNestedSections(text, func(parent, child string) bool {
		if c.renderingEngine == nil {
			return false
		}
		for _, nested := range c.renderingEngine.SectionFormat(filePath, parent).NestedSections {
			if strings.EqualFold(nested, child) {
				return true
			}
		}
		return false
	})
	results := c.annotationEngine.AnnotateWithResolvers(
		filePath, view, c.resolveAnnotationReferenceContextLocked, c.resolveListAnnotationReferenceLocked,
		func(root, value string) (int32, bool) {
			value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
			if value == "" {
				return -1, false
			}
			root = strings.Trim(strings.TrimSpace(strings.ReplaceAll(root, "\\", "/")), "/")
			return c.archive.Find(path.Clean(path.Join(root, strings.TrimLeft(value, "/"))))
		},
	)
	annotations := make([]EditorAnnotation, 0, len(results))
	for _, result := range results {
		annotations = append(annotations, EditorAnnotation{
			Start: int32(result.Start), End: int32(result.End),
			Title: result.Title, Content: result.Content, Type: result.Type,
			TargetFileIndex: result.TargetFileIndex,
			RuleIDs:         append([]string(nil), result.RuleIDs...),
			Image:           imageReferenceFromAnnotation(result.Image),
			InlineImage:     result.InlineImage,
		})
	}
	annotations = c.appendUnindexedListLinksLocked(filePath, view, annotations)
	annotations = c.appendPlaceholderAnnotationsLocked(view, annotations)
	for i := range annotations {
		annotation := &annotations[i]
		annotation.Rarity = pvf.RarityUnknown
		if annotation.TargetFileIndex < 0 {
			continue
		}
		switch strings.ToLower(path.Ext(c.archive.Path(annotation.TargetFileIndex))) {
		case ".equ", ".stk":
			annotation.Rarity = c.fileVisualsLocked(annotation.TargetFileIndex).rarity
		}
	}
	c.editorAnnotation = editorAnnotationCache{
		valid:       true,
		fileIndex:   index,
		text:        text,
		annotations: cloneEditorAnnotations(annotations),
	}
	return annotations, nil
}

// appendPlaceholderAnnotationsLocked surfaces the text behind the newer
// clients' `<table::key>` placeholders. The editor keeps showing (and writing)
// the placeholder itself — rewriting it would change the stored data — and the
// resolved text is attached as a display-only tag next to it. The tag carries
// the table index and key so it can be edited in place (SetPlaceholderText).
//
// A placeholder no table answers yet is annotated too, with Missing set: that
// is how a brand-new file gets its text, because the editor can then create the
// entry instead of the user having to open the (possibly 49 MB) table.
func (c *core) appendPlaceholderAnnotationsLocked(view pvf.ScriptView, annotations []EditorAnnotation) []EditorAnnotation {
	if c.archive == nil {
		return annotations
	}
	for _, element := range view.Elements {
		if element.Kind != pvf.ScriptElementToken {
			continue
		}
		index, key, ok := pvf.ParsePlaceholder(element.Value)
		if !ok {
			continue
		}
		resolution, found := c.archive.ResolveStringTable(index, key)
		if !found {
			annotations = append(annotations, EditorAnnotation{
				Start:           int32(element.Start),
				End:             int32(element.End),
				Title:           missingPlaceholderLabel,
				Content:         element.Value + "\n该字符串表里还没有这个键，单击可创建并填写译文",
				Type:            "placeholder-missing",
				TargetFileIndex: -1,
				Placeholder: &PlaceholderRef{
					TableIndex: int32(index),
					Key:        key,
					Missing:    true,
				},
			})
			continue
		}
		text := resolution.Text
		if resolution.Fallback {
			text += untranslatedMark
		}
		annotation := EditorAnnotation{
			Start:   int32(element.Start),
			End:     int32(element.End),
			Title:   text,
			Content: element.Value + "\n" + resolution.Source,
			Type:    "placeholder",
			Placeholder: &PlaceholderRef{
				TableIndex: int32(index),
				Key:        key,
				Fallback:   resolution.Fallback,
			},
		}
		// Link to the string table itself when the editor can open it.
		if sourceIndex, ok := c.archive.Find(resolution.Source); ok && c.archive.File(sourceIndex).DataSize <= maxEditableBytes {
			annotation.TargetFileIndex = sourceIndex
			annotation.Content += "\n\nCmd/Ctrl+单击打开字符串表；单击标签可修改译文"
		} else {
			annotation.TargetFileIndex = -1
			annotation.Content += "\n\n单击标签可修改译文（表过大，不会整文件打开）"
		}
		annotations = append(annotations, annotation)
	}
	return annotations
}

// appendUnindexedListLinksLocked makes the path token in an otherwise
// unconfigured .lst file navigable. These are link-only editor annotations:
// they carry no title/content, so the frontend renders no name tag.
func (c *core) appendUnindexedListLinksLocked(filePath string, view pvf.ScriptView, annotations []EditorAnnotation) []EditorAnnotation {
	if c.archive == nil || c.annotationEngine == nil || !strings.EqualFold(path.Ext(filePath), ".lst") {
		return annotations
	}
	if c.hasListRelationPathLocked(filePath) {
		return annotations
	}

	linked := make(map[string]struct{}, len(annotations))
	for _, annotation := range annotations {
		if annotation.TargetFileIndex >= 0 {
			linked[editorAnnotationRangeKey(annotation.Start, annotation.End)] = struct{}{}
		}
	}
	tokens := make([]pvf.ScriptElement, 0, len(view.Elements))
	for _, element := range view.Elements {
		if element.Kind == pvf.ScriptElementToken {
			tokens = append(tokens, element)
		}
	}
	for offset := 0; offset+1 < len(tokens); offset += 2 {
		pathToken := tokens[offset+1]
		_, targetIndex, ok := findListTargetInArchive(c.archive, filePath, pathToken.Value)
		if !ok {
			continue
		}
		key := editorAnnotationRangeKey(int32(pathToken.Start), int32(pathToken.End))
		if _, exists := linked[key]; exists {
			continue
		}
		annotations = append(annotations, EditorAnnotation{
			Start: int32(pathToken.Start), End: int32(pathToken.End),
			Type: "link", TargetFileIndex: targetIndex,
		})
		linked[key] = struct{}{}
	}
	return annotations
}

func (c *core) hasListRelationPathLocked(filePath string) bool {
	current := normalizeAnnotationPath(filePath)
	matches := func(configured string) bool {
		if normalizeAnnotationPath(configured) == current {
			return true
		}
		if c.archive == nil {
			return false
		}
		index, ok := c.archive.FindList(configured)
		return ok && normalizeAnnotationPath(c.archive.Path(index)) == current
	}
	for _, relation := range c.annotationEngine.Document().Relations {
		kind := relation.Kind
		if kind == "" {
			kind = "list"
		}
		switch kind {
		case "list":
			if matches(relation.ListPath) {
				return true
			}
		case "contextual":
			for _, listPath := range relation.ContextPaths {
				if matches(listPath) {
					return true
				}
			}
		}
	}
	return false
}

func normalizeAnnotationPath(value string) string {
	return strings.ToLower(strings.Trim(strings.ReplaceAll(strings.TrimSpace(value), "\\", "/"), "/"))
}

func editorAnnotationRangeKey(start, end int32) string {
	return fmt.Sprintf("%d:%d", start, end)
}

// resolveListAnnotationReferenceLocked resolves a concrete row by its path.
// The regular relation cache is intentionally ID-based for field references;
// list rows need path-based resolution so duplicate IDs remain independent.
func (c *core) resolveListAnnotationReferenceLocked(relationName, id, context, listPath, relativePath string) (annotationrules.Reference, bool) {
	if c.archive == nil || c.annotationEngine == nil {
		return annotationrules.Reference{}, false
	}
	_, fileIndex, ok := findListTargetInArchive(c.archive, listPath, relativePath)
	if !ok {
		return annotationrules.Reference{}, false
	}
	relation, ok := c.annotationEngine.Relation(relationName)
	if !ok {
		return annotationrules.Reference{}, false
	}
	reference := annotationrules.Reference{
		ID: id, Path: c.archive.Path(fileIndex), FileIndex: fileIndex,
	}
	text, err := c.archive.Text(fileIndex)
	if err != nil {
		return reference, true
	}
	reference.Name = c.readRelationTargetNameFromTextLocked(listPath, relation.NameSection, text)
	return reference, true
}

func (c *core) readRelationTargetNameLocked(fileIndex int32, listPath, nameSection string) string {
	text, err := c.archive.Text(fileIndex)
	if err != nil {
		return ""
	}
	return c.readRelationTargetNameFromTextLocked(listPath, nameSection, text)
}

func (c *core) readRelationTargetNameFromTextLocked(listPath, nameSection, text string) string {
	if sameSearchPath(listPath, "character/character.lst") {
		if name := firstSectionValue(text, "growtype name"); strings.TrimSpace(name) != "" {
			return resolvePreviewText(c.archive, name)
		}
	}
	name := firstSectionValue(text, nameSection)
	if name != "" || !sameSearchPath(listPath, itemShopListPath) {
		return resolvePreviewText(c.archive, name)
	}
	npcID := firstSectionValue(text, "npc")
	if npcID == "" {
		return ""
	}
	for relationName, relation := range c.annotationEngine.Document().Relations {
		kind := relation.Kind
		if kind == "" {
			kind = "list"
		}
		if kind != "list" || !sameSearchPath(relation.ListPath, npcListPath) {
			continue
		}
		reference, ok := c.resolveAnnotationReferenceContextLocked(relationName, npcID, "")
		if ok {
			return reference.Name
		}
	}
	return ""
}

func (c *core) resolveAnnotationReferenceLocked(relationName, id string) (annotationrules.Reference, bool) {
	return c.resolveAnnotationReferenceContextLocked(relationName, id, "")
}

func (c *core) resolveAnnotationReferenceContextLocked(relationName, id, context string) (annotationrules.Reference, bool) {
	if c.annotationRelations == nil {
		c.annotationRelations = make(map[string]map[string]*relationTarget)
	}
	relation, relationOK := c.annotationEngine.Relation(relationName)
	if relationOK && relation.Kind == "union" {
		for _, member := range relation.Relations {
			if reference, ok := c.resolveAnnotationReferenceContextLocked(member, id, context); ok {
				return reference, true
			}
		}
		return annotationrules.Reference{}, false
	}
	cacheKey := relationName
	if relationOK && relation.Kind == "contextual" {
		cacheKey += "\x00" + normalizeAnnotationContext(context)
	}
	targets, ok := c.annotationRelations[cacheKey]
	if !ok {
		if relationOK && relation.Kind == "contextual" {
			targets = c.buildContextualAnnotationRelationLocked(relation, context)
		} else {
			targets = c.buildAnnotationRelationLocked(relationName)
		}
		c.annotationRelations[cacheKey] = targets
	}
	target, ok := targets[id]
	if !ok {
		return annotationrules.Reference{}, false
	}
	if !target.nameLoaded {
		target.nameLoaded = true
		target.reference.Name = c.readRelationTargetNameLocked(
			target.reference.FileIndex, target.listPath, target.nameSection,
		)
	}
	return target.reference, true
}

func (c *core) buildAnnotationRelationLocked(name string) map[string]*relationTarget {
	result := make(map[string]*relationTarget)
	if c.annotationEngine == nil || c.archive == nil {
		return result
	}
	relation, ok := c.annotationEngine.Relation(name)
	if !ok {
		return result
	}
	return c.buildAnnotationRelationFromListLocked(relation, relation.ListPath)
}

func (c *core) buildContextualAnnotationRelationLocked(relation annotationrules.RelationSpec, context string) map[string]*relationTarget {
	listPath, ok := annotationContextPath(relation.ContextPaths, context)
	if !ok {
		return map[string]*relationTarget{}
	}
	return c.buildAnnotationRelationFromListLocked(relation, listPath)
}

func (c *core) buildAnnotationRelationFromListLocked(relation annotationrules.RelationSpec, listPath string) map[string]*relationTarget {
	result := make(map[string]*relationTarget)
	if c.archive == nil {
		return result
	}
	listIndex, ok := c.archive.FindList(listPath)
	if !ok {
		return result
	}
	listPath = c.archive.Path(listIndex)
	text, err := c.archive.Text(listIndex)
	if err != nil {
		return result
	}
	view := pvf.ParseScriptView(text)
	tokens := make([]pvf.ScriptElement, 0, len(view.Elements))
	for _, element := range view.Elements {
		if element.Kind == pvf.ScriptElementToken {
			tokens = append(tokens, element)
		}
	}
	for offset := 0; offset+relation.RecordTokens <= len(tokens); offset += relation.RecordTokens {
		id := tokens[offset+relation.IDToken].Value
		if id == "" {
			continue
		}
		_, fileIndex, ok := findListTargetInArchive(c.archive, listPath, tokens[offset+relation.PathToken].Value)
		if !ok {
			continue
		}
		if _, duplicate := result[id]; duplicate {
			continue
		}
		result[id] = &relationTarget{
			reference: annotationrules.Reference{
				ID: id, Path: c.archive.Path(fileIndex), FileIndex: fileIndex,
			},
			nameSection: relation.NameSection,
			listPath:    listPath,
		}
	}
	return result
}

func annotationContextPath(paths map[string]string, context string) (string, bool) {
	context = normalizeAnnotationContext(context)
	for key, value := range paths {
		if normalizeAnnotationContext(key) == context {
			return value, true
		}
	}
	return "", false
}

func normalizeAnnotationContext(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// firstSectionValue returns the first direct value of a top-level section.
// Nested sections are skipped: they describe sub-records, so their values must
// not be mistaken for the file's own name or reference id.
func firstSectionValue(text, section string) string {
	for _, element := range pvf.ParseScriptView(text).Elements {
		if element.Kind != pvf.ScriptElementToken || element.Index != 0 || len(element.SectionPath) != 1 {
			continue
		}
		if strings.EqualFold(element.Section, section) {
			return element.Value
		}
	}
	return ""
}

func cloneTreeAnnotations(values []TreeAnnotation) []TreeAnnotation {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]TreeAnnotation, len(values))
	for i, value := range values {
		cloned[i] = value
		cloned[i].RuleIDs = append([]string(nil), value.RuleIDs...)
	}
	return cloned
}

func cloneEditorAnnotations(values []EditorAnnotation) []EditorAnnotation {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]EditorAnnotation, len(values))
	for i, value := range values {
		cloned[i] = value
		cloned[i].RuleIDs = append([]string(nil), value.RuleIDs...)
		cloned[i].Image = cloneImageReference(value.Image)
	}
	return cloned
}

func imageReferenceFromAnnotation(reference *annotationrules.ImageReference) *ImageReference {
	if reference == nil || strings.TrimSpace(reference.Path) == "" || reference.Index < 0 {
		return nil
	}
	return &ImageReference{Path: reference.Path, Index: reference.Index}
}

func validateAnnotationIndex(a *pvf.Archive, index int32) error {
	if index < 0 || index >= a.FileCount() {
		return fmt.Errorf("文件索引越界: %d", index)
	}
	return nil
}
