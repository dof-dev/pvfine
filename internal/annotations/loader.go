package annotations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	appconfig "pvfine/config"
)

func LoadDefault() (*Engine, error) {
	document, err := parseDocument(appconfig.AnnotationsJSON)
	if err != nil {
		return nil, err
	}
	lists, err := ParseLists(appconfig.ListsJSON)
	if err != nil {
		return nil, err
	}
	document.Relations = lists.Relations
	if err := Validate(document); err != nil {
		return nil, err
	}
	return Compile(document)
}

func Parse(data []byte) (Document, error) {
	document, err := parseDocument(data)
	if err != nil {
		return Document{}, err
	}
	if err := Validate(document); err != nil {
		return Document{}, err
	}
	return document, nil
}

func ParseRules(data []byte) (Document, error) {
	return parseDocument(data)
}

func parseDocument(data []byte) (Document, error) {
	var document Document
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return Document{}, fmt.Errorf("解析标注规则失败: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return Document{}, fmt.Errorf("标注规则只能包含一个 JSON 文档")
		}
		return Document{}, fmt.Errorf("解析标注规则失败: %w", err)
	}
	return document, nil
}

func ParseLists(data []byte) (ListDocument, error) {
	var lists ListDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&lists); err != nil {
		return ListDocument{}, fmt.Errorf("解析列表配置失败: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return ListDocument{}, fmt.Errorf("列表配置只能包含一个 JSON 文档")
		}
		return ListDocument{}, fmt.Errorf("解析列表配置失败: %w", err)
	}
	document := Document{Version: lists.Version, Relations: lists.Relations}
	if err := Validate(document); err != nil {
		return ListDocument{}, err
	}
	return lists, nil
}

func Marshal(document Document) ([]byte, error) {
	if err := Validate(document); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func MarshalRules(document Document) ([]byte, error) {
	if err := Validate(document); err != nil {
		return nil, err
	}
	document.Relations = nil
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func MarshalLists(lists ListDocument) ([]byte, error) {
	document := Document{Version: lists.Version, Relations: lists.Relations}
	if err := Validate(document); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(lists, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func Validate(document Document) error {
	problems := make([]string, 0)
	if document.Version != 1 {
		problems = append(problems, fmt.Sprintf("version 必须为 1，当前为 %d", document.Version))
	}

	relationNames := make([]string, 0, len(document.Relations))
	for name := range document.Relations {
		relationNames = append(relationNames, name)
	}
	sort.Strings(relationNames)
	for _, name := range relationNames {
		relation := document.Relations[name]
		prefix := fmt.Sprintf("relations.%s", name)
		if strings.TrimSpace(name) == "" {
			problems = append(problems, "relation 名称不能为空")
		}
		kind := relation.Kind
		if kind == "" {
			kind = "list"
		}
		if kind != "list" && kind != "contextual" && kind != "union" {
			problems = append(problems, prefix+".kind 必须是 list、contextual 或 union")
		}
		if kind == "union" {
			if len(relation.Relations) == 0 {
				problems = append(problems, prefix+".relations 不能为空")
			}
			for _, member := range relation.Relations {
				if strings.TrimSpace(member) == "" || member == name {
					problems = append(problems, prefix+".relations 包含无效关联类型")
					break
				}
			}
			continue
		}
		if kind == "list" && strings.TrimSpace(relation.ListPath) == "" {
			problems = append(problems, prefix+".listPath 不能为空")
		}
		if relation.IDToken < 0 || relation.PathToken < 0 || relation.ContextToken < 0 {
			problems = append(problems, prefix+" 的 token 下标不能为负数")
		}
		if kind == "contextual" {
			if len(relation.ContextPaths) == 0 {
				problems = append(problems, prefix+".contextPaths 不能为空")
			}
			for context, listPath := range relation.ContextPaths {
				if strings.TrimSpace(context) == "" || strings.TrimSpace(listPath) == "" {
					problems = append(problems, prefix+".contextPaths 的键和值不能为空")
					break
				}
			}
			if relation.RecordTokens <= relation.IDToken || relation.RecordTokens <= relation.PathToken {
				problems = append(problems, prefix+".recordTokens 必须覆盖 idToken 和 pathToken")
			}
		}
		recordTokens := relation.RecordTokens
		if recordTokens == 0 {
			recordTokens = max(relation.IDToken, relation.PathToken) + 1
		}
		if recordTokens <= relation.IDToken || recordTokens <= relation.PathToken {
			problems = append(problems, prefix+".recordTokens 必须覆盖 idToken 和 pathToken")
		}
		if strings.TrimSpace(relation.NameSection) == "" {
			problems = append(problems, prefix+".nameSection 不能为空")
		}
	}
	unionState := make(map[string]uint8)
	var visitUnion func(string)
	visitUnion = func(name string) {
		if unionState[name] == 2 {
			return
		}
		if unionState[name] == 1 {
			problems = append(problems, "relations."+name+" 存在循环引用")
			return
		}
		relation, ok := document.Relations[name]
		if !ok || relation.Kind != "union" {
			unionState[name] = 2
			return
		}
		unionState[name] = 1
		for _, member := range relation.Relations {
			if _, ok := document.Relations[member]; !ok {
				problems = append(problems, fmt.Sprintf("relations.%s 引用了不存在的 relation: %s", name, member))
				continue
			}
			visitUnion(member)
		}
		unionState[name] = 2
	}
	for _, name := range relationNames {
		visitUnion(name)
	}

	seenIDs := make(map[string]bool)
	for i, rule := range document.Rules {
		prefix := fmt.Sprintf("rules[%d]", i)
		if strings.TrimSpace(rule.ID) == "" {
			problems = append(problems, prefix+".id 不能为空")
		} else if seenIDs[rule.ID] {
			problems = append(problems, prefix+".id 重复: "+rule.ID)
		}
		seenIDs[rule.ID] = true

		for _, extension := range rule.Match.Extensions {
			if extension == "" || !strings.HasPrefix(extension, ".") || strings.ContainsAny(extension, "/\\") {
				problems = append(problems, prefix+".match.extensions 必须是以点开头的文件后缀")
				break
			}
		}
		if rule.Match.Glob != "" {
			if err := validateGlob(rule.Match.Glob); err != nil {
				problems = append(problems, prefix+".match.glob 无效: "+err.Error())
			}
		}

		switch rule.Target.Kind {
		case "path":
			if rule.Annotation.Type != "text" {
				problems = append(problems, prefix+" 的 path 标注只支持 text 类型")
			}
		case "section":
			if strings.TrimSpace(rule.Target.Section) == "" {
				problems = append(problems, prefix+".target.section 不能为空")
			}
			if rule.Annotation.Type != "text" {
				problems = append(problems, prefix+" 的 section 标注只支持 text 类型")
			}
		case "token":
			if strings.TrimSpace(rule.Target.Section) == "" {
				problems = append(problems, prefix+".target.section 不能为空")
			}
			if (rule.Target.Index == nil) == (rule.Target.Range == nil) {
				problems = append(problems, prefix+" 的 token 目标必须且只能配置 index 或 range")
			}
			if rule.Target.Index != nil && *rule.Target.Index < 0 {
				problems = append(problems, prefix+".target.index 不能为负数")
			}
			if rule.Target.RecordTokens < 0 {
				problems = append(problems, prefix+".target.recordTokens 不能为负数")
			}
			if rule.Target.RecordTokens == 0 && rule.Target.ContextIndex != nil {
				problems = append(problems, prefix+".target.contextIndex 需要同时配置 recordTokens")
			}
			if rule.Target.RecordTokens > 0 {
				if rule.Target.Index == nil {
					problems = append(problems, prefix+".target.recordTokens 需要配合单个 index 使用")
				} else if *rule.Target.Index >= rule.Target.RecordTokens {
					problems = append(problems, prefix+".target.index 必须小于 recordTokens")
				}
				if rule.Target.ContextIndex != nil && (*rule.Target.ContextIndex < 0 || *rule.Target.ContextIndex >= rule.Target.RecordTokens) {
					problems = append(problems, prefix+".target.contextIndex 必须位于 recordTokens 范围内")
				}
			}
			if rule.Target.Range != nil {
				if rule.Target.Range.Start < 0 || rule.Target.Range.EndExclusive <= rule.Target.Range.Start {
					problems = append(problems, prefix+".target.range 必须满足 0 <= start < endExclusive")
				}
				if rule.Annotation.Type != "text" {
					problems = append(problems, prefix+" 的 token 范围只支持 text 类型")
				}
			}
		default:
			problems = append(problems, prefix+".target.kind 必须是 path、section 或 token")
		}

		if strings.TrimSpace(rule.Annotation.Title) == "" {
			problems = append(problems, prefix+".annotation.title 不能为空")
		}
		switch rule.Annotation.Type {
		case "text":
		case "enum":
			if len(rule.Annotation.Values) == 0 {
				problems = append(problems, prefix+".annotation.values 不能为空")
			}
		case "reference":
			relation, ok := document.Relations[rule.Annotation.Relation]
			if !ok {
				problems = append(problems, prefix+" 引用了不存在的 relation: "+rule.Annotation.Relation)
			} else if relation.Kind == "contextual" {
				if rule.Target.RecordTokens == 0 {
					problems = append(problems, prefix+" 的 contextual relation 需要配置 target.recordTokens")
				}
				contextIndex := relation.ContextToken
				if rule.Target.ContextIndex != nil {
					contextIndex = *rule.Target.ContextIndex
				}
				if rule.Target.RecordTokens > 0 && (contextIndex < 0 || contextIndex >= rule.Target.RecordTokens) {
					problems = append(problems, prefix+" 的 contextual relation 上下文 token 下标必须位于 target.recordTokens 范围内")
				}
			}
		default:
			problems = append(problems, prefix+".annotation.type 必须是 text、enum 或 reference")
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("标注规则校验失败:\n- %s", strings.Join(problems, "\n- "))
	}
	return nil
}

func validateGlob(pattern string) error {
	for _, segment := range strings.Split(normalizePath(pattern), "/") {
		if segment == "**" {
			continue
		}
		if _, err := path.Match(segment, segment); err != nil {
			return err
		}
	}
	return nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
