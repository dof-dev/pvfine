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
}

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
	view := pvf.ParseScriptView(text)
	results := c.annotationEngine.AnnotateWithContextAndListResolver(
		filePath, view, c.resolveAnnotationReferenceContextLocked, c.resolveListAnnotationReferenceLocked,
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
	c.editorAnnotation = editorAnnotationCache{
		valid:       true,
		fileIndex:   index,
		text:        text,
		annotations: cloneEditorAnnotations(annotations),
	}
	return annotations, nil
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
	for _, relation := range c.annotationEngine.Document().Relations {
		kind := relation.Kind
		if kind == "" {
			kind = "list"
		}
		switch kind {
		case "list":
			if normalizeAnnotationPath(relation.ListPath) == current {
				return true
			}
		case "contextual":
			for _, listPath := range relation.ContextPaths {
				if normalizeAnnotationPath(listPath) == current {
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
	name := firstSectionValue(text, nameSection)
	if name != "" || !sameSearchPath(listPath, itemShopListPath) {
		return name
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
	listIndex, ok := c.archive.Find(listPath)
	if !ok {
		return result
	}
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

func firstSectionValue(text, section string) string {
	for _, element := range pvf.ParseScriptView(text).Elements {
		if element.Kind == pvf.ScriptElementToken && element.Index == 0 && strings.EqualFold(element.Section, section) {
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
