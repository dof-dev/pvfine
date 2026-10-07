package pvf

import (
	"strings"
	"testing"
	"time"
)

func TestProbe110StringTableCost(t *testing.T) {
	a, err := Open("../../testdata/110US.pvf")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()

	fileIndex, ok := a.Find("equipment/character/common/amulet/100300877.equ")
	if !ok {
		t.Fatal("target file not found")
	}
	text, err := a.Text(fileIndex)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[int]int{}
	for offset := 0; offset < len(text); {
		start := strings.IndexByte(text[offset:], '<')
		if start < 0 {
			break
		}
		start += offset
		end := strings.IndexByte(text[start:], '>')
		if end < 0 {
			break
		}
		end += start + 1
		if tableIndex, _, valid := ParsePlaceholder(text[start:end]); valid {
			counts[tableIndex]++
		}
		offset = end
	}
	t.Logf("placeholder table counts: %v", counts)

	for tableIndex := range counts {
		started := time.Now()
		paths := a.StringTablePaths(tableIndex)
		t.Logf("table %d path map: %s (%d paths)", tableIndex, time.Since(started), len(paths))
		for _, path := range paths {
			index, found := a.Find(path)
			if !found {
				continue
			}
			rawStarted := time.Now()
			raw, err := a.RawBytes(index)
			if err != nil {
				t.Fatal(err)
			}
			rawElapsed := time.Since(rawStarted)
			indexStarted := time.Now()
			values, valid := indexStringTableValues(raw)
			t.Logf("table payload: %d bytes, raw=%s index=%s (%d entries, valid=%v)", len(raw), rawElapsed, time.Since(indexStarted), len(values), valid)
			break
		}
	}
}
