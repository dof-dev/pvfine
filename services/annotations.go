package services

import (
	"fmt"
	"strings"

	annotationrules "pvfine/internal/annotations"
	"pvfine/internal/pvf"
)

type EditorAnnotation struct {
	Start           int32    `json:"start"`
	End             int32    `json:"end"`
	Title           string   `json:"title"`
	Content         string   `json:"content"`
	Type            string   `json:"type"`
	TargetFileIndex int32    `json:"targetFileIndex"`
	RuleIDs         []string `json:"ruleIds,omitempty"`
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
	results := c.annotationEngine.Annotate(filePath, view, c.resolveAnnotationReferenceLocked)
	annotations := make([]EditorAnnotation, 0, len(results))
	for _, result := range results {
		annotations = append(annotations, EditorAnnotation{
			Start: int32(result.Start), End: int32(result.End),
			Title: result.Title, Content: result.Content, Type: result.Type,
			TargetFileIndex: result.TargetFileIndex,
			RuleIDs:         append([]string(nil), result.RuleIDs...),
		})
	}
	c.editorAnnotation = editorAnnotationCache{
		valid:       true,
		fileIndex:   index,
		text:        text,
		annotations: cloneEditorAnnotations(annotations),
	}
	return annotations, nil
}

func (c *core) resolveAnnotationReferenceLocked(relationName, id string) (annotationrules.Reference, bool) {
	if c.annotationRelations == nil {
		c.annotationRelations = make(map[string]map[string]*relationTarget)
	}
	targets, ok := c.annotationRelations[relationName]
	if !ok {
		targets = c.buildAnnotationRelationLocked(relationName)
		c.annotationRelations[relationName] = targets
	}
	target, ok := targets[id]
	if !ok {
		return annotationrules.Reference{}, false
	}
	if !target.nameLoaded {
		target.nameLoaded = true
		text, err := c.archive.Text(target.reference.FileIndex)
		if err == nil {
			target.reference.Name = firstSectionValue(text, target.nameSection)
		}
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
	listIndex, ok := c.archive.Find(relation.ListPath)
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
		targetPath, ok := resolveListPath(relation.ListPath, tokens[offset+relation.PathToken].Value)
		if !ok {
			continue
		}
		fileIndex, ok := c.archive.Find(targetPath)
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
		}
	}
	return result
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
	}
	return cloned
}

func validateAnnotationIndex(a *pvf.Archive, index int32) error {
	if index < 0 || index >= a.FileCount() {
		return fmt.Errorf("文件索引越界: %d", index)
	}
	return nil
}
