package script

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"

	"pvfine/internal/pvf"
)

func (t *Transaction) referencesStringTable(raw []byte, dependency string) bool {
	for pos := 0; pos+5 <= len(raw); pos += 5 {
		if raw[pos] != 8 && raw[pos] != 10 {
			continue
		}
		offset := int32(binary.LittleEndian.Uint32(raw[pos+1:]))
		table, _, ok := pvf.ParsePlaceholder(t.stage.ResolveString(offset))
		if !ok {
			continue
		}
		for _, candidate := range t.stage.StringTablePaths(table) {
			if pvf.NormalizePath(candidate) == dependency {
				return true
			}
		}
	}
	return false
}

// RegisterStringReference uses content-keyed entries so changing one script
// never overwrites the text behind a reference shared by other scripts.
func (t *Transaction) RegisterStringReference(owner, text string, existing *pvf.ScriptValue) (pvf.ScriptValue, error) {
	if strings.ContainsAny(text, "\r\n\x00") {
		return pvf.ScriptValue{}, fmt.Errorf("字符串表文本不能包含换行或 NUL，请使用 setStrValue")
	}
	key := fmt.Sprintf("pvfine_%x", sha256.Sum256([]byte(text)))
	tables := make([]int, 0)
	if existing != nil {
		if reference, ok := existing.Value.(string); ok {
			if table, _, ok := pvf.ParsePlaceholder(reference); ok {
				tables = append(tables, table)
			}
		}
	}
	for _, mapPath := range []string{"list/n_string.lst", "n_string.lst"} {
		index, ok := t.stage.Find(mapPath)
		if !ok {
			continue
		}
		pairs, err := t.stage.ListPairs(index)
		if err != nil {
			return pvf.ScriptValue{}, err
		}
		for _, pair := range pairs {
			if table, err := strconv.Atoi(pair.ID); err == nil && table >= 0 {
				tables = append(tables, table)
			}
		}
	}
	for _, table := range tables {
		index, ok := t.stage.StringTableEntryIndex(table, key)
		if !ok {
			continue
		}
		if value, found := t.stage.LookupStringTableText(table, key); found && value != text {
			return pvf.ScriptValue{}, fmt.Errorf("自动字符串键已被占用: %s", key)
		}
		tablePath := t.stage.Path(index)
		if err := t.Mark(index); err != nil {
			return pvf.ScriptValue{}, err
		}
		if err := t.stage.SetStringTableEntryAt(index, key, text); err != nil {
			return pvf.ScriptValue{}, err
		}
		if t.stringDependencies == nil {
			t.stringDependencies = make(map[string]map[string]struct{})
		}
		owner = pvf.NormalizePath(owner)
		if t.stringDependencies[owner] == nil {
			t.stringDependencies[owner] = make(map[string]struct{})
		}
		t.stringDependencies[owner][pvf.NormalizePath(tablePath)] = struct{}{}
		return pvf.NewScriptValue(pvf.ScriptTokenBlock8, fmt.Sprintf("<%d::%s>", table, key), pvf.ScriptPoolUTF16)
	}
	return pvf.ScriptValue{}, fmt.Errorf("未找到可写的字符串表，请使用 setStrValue")
}
