package services

import (
	"encoding/json"
	"os"
	"path"
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

// Use a separate opt-in so ordinary real-archive tests do not also scan every
// GUI document. No source archive or user settings are changed.
func TestGUIRealArchiveCompatibility(t *testing.T) {
	filename := os.Getenv("PVF_GUI_TESTFILE")
	if filename == "" {
		t.Skip("PVF_GUI_TESTFILE required")
	}
	a, err := pvf.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	c := newCore()
	// Readers need the archive and relation engine, not a full explorer/search
	// index for the millions of unrelated files in a retail archive.
	c.archive = a
	service := NewFileGUIService(c)
	type questFixture struct {
		Text string `json:"text"`
	}
	var quests []questFixture
	type stats struct {
		Files, Errors, Issues, Empty int
	}
	counts := map[string]*stats{}
	messages := map[string]int{}
	for i := int32(0); i < a.FileCount(); i++ {
		p := strings.ToLower(a.Path(i))
		kind := ""
		switch {
		case strings.HasSuffix(p, ".shp"):
			kind = "shop"
		case strings.HasPrefix(p, "skill/") && strings.HasSuffix(p, ".skl"):
			kind = "skill"
		case strings.HasSuffix(p, ".qst"):
			kind = "quest"
		case p == worldDropPath || p == regionalWorldDropPath:
			kind = "world-drop"
		case isUpgradePath(p):
			kind = "upgrade"
		case p == "etc/upgrade_separate.etc":
			kind = "forge"
		}
		if kind == "" {
			continue
		}
		if counts[kind] == nil {
			counts[kind] = &stats{}
		}
		s := counts[kind]
		s.Files++
		text, readErr := a.Text(i)
		issues, empty := 0, false
		if readErr == nil {
			switch kind {
			case "shop":
				doc, e := service.ReadShop(i, text)
				readErr = e
				if e == nil {
					issues, empty = len(doc.Issues), len(doc.Tabs) == 0
					for _, issue := range doc.Issues {
						messages[kind+": section="+issue.Section]++
					}
				}
			case "skill":
				doc, e := service.ReadSkill(i, text)
				readErr = e
				if e == nil {
					empty = len(doc.Modes) == 0
					for _, mode := range doc.Modes {
						issues += len(mode.Issues)
						for _, issue := range mode.Issues {
							messages[kind+": "+issue]++
						}
					}
				}
			case "world-drop":
				doc, e := service.ReadWorldDrop(i, text)
				readErr = e
				if e == nil {
					empty = len(doc.Levels) == 0
				}
			case "upgrade":
				doc, e := service.ReadUpgrade(i, text)
				readErr = e
				if e == nil {
					empty = len(doc.Rows) == 0
					if doc.SafeIssue != "" {
						issues++
						messages[kind+": "+doc.SafeIssue]++
					}
				}
			case "forge":
				doc, e := service.ReadForge(i, text)
				readErr = e
				if e == nil {
					empty = len(doc.Rows) == 0
				}
			case "quest":
				if os.Getenv("PVF_GUI_QUEST_CORPUS") != "" {
					quests = append(quests, questFixture{Text: text})
				}
			}
		}
		if readErr != nil {
			s.Errors++
			messages[kind+": "+readErr.Error()]++
		}
		if issues > 0 {
			s.Issues++
		}
		if empty {
			s.Empty++
		}
	}
	t.Logf("client=%s format=%s files=%d", a.ClientVersion(), a.Format(), a.FileCount())
	for _, kind := range []string{"shop", "skill", "quest", "world-drop", "upgrade", "forge"} {
		s := counts[kind]
		if s == nil {
			t.Errorf("%s: no matching files", kind)
			continue
		}
		t.Logf("%s: files=%d read-errors=%d with-issues=%d empty=%d", kind, s.Files, s.Errors, s.Issues, s.Empty)
		if s.Errors > 0 {
			t.Errorf("%s: %d GUI read failures", kind, s.Errors)
		}
	}
	for message, count := range messages {
		t.Logf("diagnostic count=%d: %s", count, message)
	}
	for _, spec := range dropRateFileSpecs {
		index, ok := a.Find(spec.path)
		if !ok {
			t.Errorf("drop %s: missing", spec.key)
			continue
		}
		raw, err := a.RawBytes(index)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := a.ParseScriptDocument(raw)
		if err != nil {
			t.Fatal(err)
		}
		section, _ := doc.Section([]string{dropRateSectionName}, 0)
		if section == nil {
			t.Errorf("drop %s: missing rarity section", spec.key)
			continue
		}
		values, err := scriptIntegerValues(section.Values(), path.Base(spec.path))
		if err != nil {
			t.Fatal(err)
		}
		width, err := dropRateGroupWidth(values, spec)
		t.Logf("drop %s: group-width=%d error=%v", spec.key, width, err)
		if err != nil {
			t.Error(err)
		}
	}
	if doc, err := NewDropService(c).Read(); err != nil {
		t.Errorf("drop structure: %v", err)
	} else {
		t.Logf("drop structure: %d sections; version allowed=%t", len(doc.Sections), supportedDropRateVersion(a.ClientVersion()))
		for _, section := range doc.Sections {
			section.Groups[0].Rates = []int{1000, 2000, 3000, 4000, 0}
		}
		replacements, err := prepareDropRateReplacements(a, doc.Sections)
		if err != nil {
			t.Fatal(err)
		}
		if len(replacements) != len(dropRateFileSpecs) {
			t.Fatal("drop edit did not prepare all four files")
		}
		for i, replacement := range replacements {
			original, err := a.RawBytes(replacement.index)
			if err != nil {
				t.Fatal(err)
			}
			before, err := a.ParseScriptDocument(original)
			if err != nil {
				t.Fatal(err)
			}
			after, err := a.ParseScriptDocument(replacement.raw)
			if err != nil {
				t.Fatal(err)
			}
			oldSection, _ := before.Section([]string{dropRateSectionName}, 0)
			newSection, _ := after.Section([]string{dropRateSectionName}, 0)
			oldValues, err := scriptIntegerValues(oldSection.Values(), "")
			if err != nil {
				t.Fatal(err)
			}
			newValues, err := scriptIntegerValues(newSection.Values(), "")
			if err != nil {
				t.Fatal(err)
			}
			spec := dropRateFileSpecs[i]
			if len(newValues) != len(oldValues) || len(original) != len(replacement.raw) {
				t.Fatal("drop edit changed the source layout")
			}
			for j, value := range oldValues {
				if j >= spec.offset && j < spec.offset+4 {
					continue
				}
				if newValues[j] != value {
					t.Fatal("drop edit changed an unrelated threshold")
				}
			}
			width, err := dropRateGroupWidth(newValues, spec)
			if err != nil {
				t.Fatal(err)
			}
			rates, err := decodeDropRateGroup(newValues[spec.offset : spec.offset+width])
			if err != nil || !equalDropRates(rates, []int{1000, 2000, 3000, 4000, 0}) {
				t.Fatal("drop edit did not round trip through the encoder")
			}
		}
		t.Log("drop edits: four replacement payloads round tripped; extra thresholds preserved")
	}
	if output := os.Getenv("PVF_GUI_QUEST_CORPUS"); output != "" {
		data, err := json.Marshal(quests)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if a.ModifiedCount() != 0 {
		t.Fatal("GUI readers modified the archive")
	}
}
