package services

import (
	"fmt"
	"path"
	"strings"
	"time"

	modpkg "pvfine/internal/mod"
	"pvfine/internal/pvf"
)

func (s *exportSource) findList(configured string) (string, bool) {
	if s.snapshot == nil {
		i, ok := s.archive.FindList(configured)
		if ok {
			return s.archive.Path(i), true
		}
		return "", false
	}
	candidates := []string{configured, "list/" + path.Base(configured)}
	if strings.EqualFold(path.Dir(configured), "list") {
		candidates = append(candidates, strings.TrimSuffix(path.Base(configured), ".lst")+"/"+path.Base(configured))
	}
	for _, p := range candidates {
		if actual, ok := s.lookup(p); ok {
			return actual, true
		}
	}
	return "", false
}

func (s *exportSource) listTarget(listPath, relative string) (string, bool) {
	candidates, ok := listPathCandidates(listPath, relative)
	if !ok {
		return "", false
	}
	for _, candidate := range candidates {
		if p, ok := s.lookup(candidate); ok {
			return p, true
		}
	}
	return "", false
}

type exportTableMapping struct {
	listPath string
	pair     modpkg.Pair
}

func (s *ExportService) addExportDependenciesLocked(source *exportSource, pkg *modpkg.Package, writer modpkg.Writer) (int, error) {
	explicit := make(map[string]int, len(pkg.Entries))
	scripts := []modpkg.Entry{}
	targets := map[string]bool{}
	for i, entry := range pkg.Entries {
		explicit[strings.ToLower(entry.Path)] = i
		if entry.Operation == modpkg.ReplaceFile {
			targets[strings.ToLower(entry.Path)] = true
			if entry.DataType == pvf.TypeScript {
				scripts = append(scripts, entry)
			}
		}
	}
	if len(targets) == 0 {
		return 0, nil
	}
	type scriptReference struct{ path, value string }
	references := []scriptReference{}
	seenRefs := map[string]bool{}
	for _, script := range scripts {
		if !strings.Contains(string(script.Data), "<") {
			continue
		}
		for _, element := range pvf.ParseScriptView(string(script.Data)).Elements {
			if element.TokenType != 8 && element.TokenType != 10 {
				continue
			}
			for _, reference := range exportPlaceholders(element.Value) {
				if !seenRefs[reference] {
					references = append(references, scriptReference{script.Path, reference})
					seenRefs[reference] = true
				}
			}
		}
	}
	count := 0
	// Explicit contents win; dependency additions only fill absent keys.
	add := func(p string, operation modpkg.Operation, pair modpkg.Pair) {
		key := strings.ToLower(p)
		if i, ok := explicit[key]; ok {
			entry := &pkg.Entries[i]
			if entry.Operation == modpkg.ReplaceFile {
				return
			}
			for _, existing := range entry.Pairs {
				if existing.Key == pair.Key {
					return
				}
			}
		}
		entry := modpkg.Entry{Path: p, Operation: operation, Encoding: modpkg.Text, Pairs: []modpkg.Pair{pair}}
		if err := writer.Validate(modpkg.Package{Metadata: pkg.Metadata, Entries: []modpkg.Entry{entry}}); err != nil {
			source.diagnostics.skip("无法导出依赖条目 %s / %s: %v", p, pair.Key, err)
			return
		}
		if i, ok := explicit[key]; ok {
			pkg.Entries[i].Pairs = append(pkg.Entries[i].Pairs, pair)
		} else {
			explicit[key] = len(pkg.Entries)
			pkg.Entries = append(pkg.Entries, entry)
		}
		count++
	}
	cache := map[string][]modpkg.Pair{}
	stringValues := map[string]map[string]string{}
	readPairs := func(p string, operation modpkg.Operation) []modpkg.Pair {
		key := strings.ToLower(p)
		if pairs, ok := cache[key]; ok {
			return pairs
		}
		started := time.Now()
		content, err := source.read(p)
		if err != nil {
			source.diagnostics.skip("无法读取依赖表 %s: %v", p, err)
			cache[key] = nil
			return nil
		}
		readElapsed := time.Since(started)
		pairs, err := parseExportPairsReported(*content, operation, &source.diagnostics)
		if err != nil {
			source.diagnostics.skip("无法解析依赖表 %s: %v", p, err)
			cache[key] = nil
			return nil
		}
		developmentLog("[pvfine:export] dependency table=%s rows=%d read=%s parse=%s",
			p, len(pairs), readElapsed.Round(time.Millisecond), (time.Since(started) - readElapsed).Round(time.Millisecond))
		cache[key] = pairs
		return pairs
	}
	mappings := map[string][]exportTableMapping{}
	for _, list := range []string{
		"list/n_string.lst", "n_string.lst",
		"list/n_string_translate.lst", "n_string_translate.lst",
		"list/n_string_kor.lst", "n_string_kor.lst",
	} {
		if len(references) == 0 {
			break
		}
		actual, ok := source.lookup(list)
		if !ok {
			continue
		}
		pairs := readPairs(actual, modpkg.MergeList)
		for _, pair := range pairs {
			mappings[pair.Key] = append(mappings[pair.Key], exportTableMapping{actual, pair})
		}
	}
	for _, ref := range references {
		reference := ref.value
		table, key, _ := pvf.ParsePlaceholder(reference)
		candidates := mappings[fmt.Sprint(table)]
		found, emptyFound := false, false
		var emptyMapping exportTableMapping
		var emptyPath string
		for _, mapping := range candidates {
			strPath, exists := source.listTarget(mapping.listPath, mapping.pair.Value)
			if !exists {
				continue
			}
			pairs := readPairs(strPath, modpkg.MergeStrings)
			values, cached := stringValues[strPath]
			if !cached {
				values = pairMap(pairs)
				stringValues[strPath] = values
			}
			value, exists := values[key]
			if !exists {
				continue
			}
			if value == "" {
				if !emptyFound {
					emptyMapping, emptyPath, emptyFound = mapping, strPath, true
				}
				continue
			}
			add(strPath, modpkg.MergeStrings, modpkg.Pair{Key: key, Value: value})
			add(mapping.listPath, modpkg.MergeList, mapping.pair)
			found = true
			break
		}
		if !found && emptyFound {
			add(emptyPath, modpkg.MergeStrings, modpkg.Pair{Key: key, Value: ""})
			add(emptyMapping.listPath, modpkg.MergeList, emptyMapping.pair)
			found = true
		}
		if !found {
			source.diagnostics.skip("文字表依赖缺失或无法解析: %s 引用 %s", ref.path, reference)
		}
	}
	seenLists, registered := map[string]bool{}, map[string]bool{}
	for _, spec := range s.c.searchableListSpecsLocked() {
		list, ok := source.findList(spec.listPath)
		if !ok || seenLists[strings.ToLower(list)] {
			continue
		}
		seenLists[strings.ToLower(list)] = true
		pairs := readPairs(list, modpkg.MergeList)
		for _, pair := range pairs {
			// Most registration rows do not target any exported file. Avoid
			// archive lookups for those rows, but preserve candidate precedence
			// when a row could match an exported path.
			candidates, valid := listPathCandidates(list, pair.Value)
			if !valid {
				continue
			}
			matches := false
			for _, candidate := range candidates {
				if targets[strings.ToLower(candidate)] {
					matches = true
					break
				}
			}
			if !matches {
				continue
			}
			target, ok := source.listTarget(list, pair.Value)
			if ok && targets[strings.ToLower(target)] {
				add(list, modpkg.MergeList, pair)
				registered[strings.ToLower(target)] = true
			}
		}
	}
	for target := range targets {
		ext := strings.ToLower(path.Ext(target))
		if (ext == ".equ" || ext == ".stk") && !registered[target] {
			source.diagnostics.skip("物品列表依赖缺失: %s 未在配置的列表中登记", target)
		}
	}
	return count, nil
}

func exportPlaceholders(value string) []string {
	result := []string{}
	for {
		start := strings.IndexByte(value, '<')
		if start < 0 {
			break
		}
		value = value[start:]
		end := strings.IndexByte(value, '>')
		if end < 0 {
			break
		}
		candidate := value[:end+1]
		if _, _, ok := pvf.ParsePlaceholder(candidate); ok {
			result = append(result, candidate)
		}
		value = value[end+1:]
	}
	return result
}
