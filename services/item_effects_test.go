package services

import (
	"fmt"
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

const effectTestANI = "[FRAME MAX]\n1\n[FRAME000]\n[IMAGE]\n`effect/test.img` 3\n[IMAGE POS]\n-16 -32\n[DELAY]\n80"

func effectFixture(t *testing.T, modern bool) (*core, int32, map[string]int32) {
	t.Helper()
	a := pvf.New()
	indexes := map[string]int32{}
	listPath, entry := "passiveobject/passiveobject.lst", "common/test.obj"
	if modern {
		listPath, entry = "list/passiveobject.lst", "passiveobject/common/test.obj"
	}
	files := map[string]string{
		"equipment/test.equ":                          "[name]\n`测试装备`",
		listPath:                                      "12 `" + entry + "`",
		"passiveobject/common/test.obj":               "[basic motion]\n`animation/base.ani`\n[basic action]\n`action/base.act`\n[string data]\n`particle/own.ptl`\n[/string data]\n[add particles]\n[particle]\n[file]\n`particle/added.ptl`\n[/particle]\n[/add particles]",
		"passiveobject/common/animation/base.ani":     effectTestANI,
		"passiveobject/common/action/base.act":        "[BASE ANI]\n`../animation/action.ani`",
		"passiveobject/common/animation/action.ani":   effectTestANI,
		"passiveobject/common/particle/own.ptl":       "[life time]\n1000\n[creation frequency]\n25.0\n[maximum create number]\n5\n[gravity]\n10\n[transparent value]\n100 0\n[x axis]\n[move variable 1]\n20 5\n[/x axis]\n[object]\n`../animation/particle.ani` 1\n[/object]",
		"passiveobject/common/particle/added.ptl":     "[life time]\n500\n[object]\n`../animation/particle.ani`\n[/object]",
		"passiveobject/common/animation/particle.ani": effectTestANI,
		"equipment/character/particle/straight.ptl":   "[life time]\n500\n[object]\n`../../../passiveobject/common/animation/particle.ani`\n[/object]",
	}
	for p, text := range files {
		i, err := a.AddFileText(p, text, pvf.TypeScript)
		if err != nil {
			t.Fatal(err)
		}
		indexes[p] = i
	}
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	return c, indexes["equipment/test.equ"], indexes
}
func TestItemEffectPreviewCurrentArchiveAndRelativePaths(t *testing.T) {
	for _, modern := range []bool{false, true} {
		t.Run(fmt.Sprintf("modern=%v", modern), func(t *testing.T) {
			c, index, _ := effectFixture(t, modern)
			doc, err := NewFileGUIService(c).ReadItemEffectPreview(index, "12", "straight.ptl", nil)
			if err != nil || doc == nil {
				t.Fatal(err)
			}
			if len(doc.Issues) != 0 || doc.ObjectPath != "passiveobject/common/test.obj" || len(doc.Layers) != 5 {
				t.Fatalf("preview=%+v", doc)
			}
			particles := 0
			for _, layer := range doc.Layers {
				if layer.Animation == nil || !layer.Animation.Valid || len(layer.Animation.Frames) != 1 || layer.Animation.Frames[0].Layers[0].Image.Index != 3 {
					t.Fatalf("layer=%+v", layer)
				}
				if layer.Particle != nil {
					particles++
					if layer.Particle.Maximum > 64 {
						t.Fatal("unbounded particles")
					}
				}
			}
			if particles != 3 {
				t.Fatalf("particles=%d", particles)
			}
		})
	}
}
func TestItemEffectPreviewDraftsAndMissingResources(t *testing.T) {
	c, index, indexes := effectFixture(t, false)
	s := NewFileGUIService(c)
	p := "passiveobject/common/animation/base.ani"
	doc, err := s.ReadItemEffectPreview(index, "12", "missing.ptl", []ShopDraft{{FileIndex: indexes[p], Path: p, Text: strings.Replace(effectTestANI, "80", "240", 1)}})
	if err != nil || doc.Layers[0].Animation.Frames[0].Delay != 240 || len(doc.Issues) != 1 || !strings.Contains(doc.Issues[0], "无法读取粒子") {
		t.Fatalf("draft=%+v %v", doc, err)
	}
	original, _ := c.archive.Text(indexes[p])
	if strings.Contains(original, "240") {
		t.Fatal("preview mutated archive")
	}
	doc, err = s.ReadItemEffectPreview(index, "12", "", []ShopDraft{{FileIndex: indexes[p], Path: "wrong/path.ani", Text: "invalid"}})
	if err != nil || doc.Layers[0].Animation.Frames[0].Delay != 80 {
		t.Fatal("mismatched draft applied")
	}
	for _, id := range []string{"-1", "nan", "2147483648"} {
		if _, err := s.ReadItemEffectPreview(index, id, "", nil); err == nil {
			t.Fatal("invalid object accepted")
		}
	}
	if _, err := s.ReadItemEffectPreview(-1, "12", "", nil); err == nil {
		t.Fatal("invalid index accepted")
	}
	doc, err = s.ReadItemEffectPreview(index, "99", "", nil)
	if err != nil || len(doc.Layers) != 0 || len(doc.Issues) == 0 {
		t.Fatal("missing object not reported")
	}
}
func TestItemEffectParticleMetadataAndPlaceholders(t *testing.T) {
	meta := parseEffectParticle(pvf.ParseScriptView("[life time]\n4000\n[creation frequency]\n25.0\n[transparent value]\n75\n[x axis]\n[move variable 1]\n20 -2\n[/x axis]\n[y axis]\n[move variable 1]\n30 4\n[/y axis]"))
	if meta.Life != 4000 || meta.Maximum != 64 || meta.Alpha[1] != 75 || meta.Velocity[0] != 20 || meta.Acceleration[1] != 4 {
		t.Fatalf("meta=%+v", meta)
	}
	c, index, _ := effectFixture(t, false)
	_, err := c.archive.AddFileText("equipment/empty.ptl", "[life time]\n1000", pvf.TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := NewFileGUIService(c).ReadItemEffectPreview(index, "", "empty.ptl", nil)
	if err != nil || len(doc.Layers) != 0 || len(doc.Issues) != 1 || !strings.Contains(doc.Issues[0], "没有可绘制") {
		t.Fatalf("placeholder=%+v %v", doc, err)
	}
}
func TestEffectResourceSearchScopesBeforePagination(t *testing.T) {
	for _, disk := range []bool{false, true} {
		t.Run(fmt.Sprintf("disk=%v", disk), func(t *testing.T) {
			c, _, _ := effectFixture(t, false)
			if _, err := c.archive.AddFileText("passiveobject/unregistered.obj", "[name]\n`未登记对象`", pvf.TypeScript); err != nil {
				t.Fatal(err)
			}
			if err := c.setArchive(c.archive); err != nil {
				t.Fatal(err)
			}
			if disk {
				index, _, err := openSQLiteArchiveIndex(c.archive)
				if err != nil {
					t.Fatal(err)
				}
				c.mu.Lock()
				c.diskIndex = index
				c.installDiskArchiveIndexesLocked(c.archive)
				c.mu.Unlock()
			}
			c.startSearchIndex()
			waitForSearchIndex(t, c)
			s := NewArchiveService(c)
			result, err := s.SearchEffectResources("object", "12", 0, 1)
			if err != nil || len(result.Hits) != 1 || result.Hits[0].ID != "12" {
				t.Fatalf("object=%+v %v", result, err)
			}
			cursor, count := 0, 0
			seen := map[string]bool{}
			for page := 0; page < 10; page++ {
				result, err = s.SearchEffectResources("particle", ".PTL", cursor, 1)
				if err != nil {
					t.Fatal(err)
				}
				for _, hit := range result.Hits {
					if seen[hit.Path] || !strings.HasSuffix(hit.Path, ".ptl") {
						t.Fatalf("bad particle=%+v", hit)
					}
					seen[hit.Path] = true
					count++
				}
				if result.NextCursor < 0 {
					break
				}
				if result.NextCursor <= cursor {
					t.Fatal("cursor did not advance")
				}
				cursor = result.NextCursor
			}
			if count != 3 {
				t.Fatalf("particles=%d", count)
			}
			if _, err := s.SearchEffectResources("invalid", "", 0, 30); err == nil {
				t.Fatal("invalid resource kind accepted")
			}
		})
	}
}

func TestEffectResourceAPCAndEmptyQuery(t *testing.T) {
	c, _, _ := effectFixture(t, true)
	for p, text := range map[string]string{
		"list/aicharacter.lst":  "101 `aicharacter/sub/a.chr` 102 `aicharacter/sub/b.chr`",
		"aicharacter/sub/a.chr": "[name]\n`测试人偶甲`",
		"aicharacter/sub/b.chr": "[name]\n`测试人偶乙`",
	} {
		if _, err := c.archive.AddFileText(p, text, pvf.TypeScript); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.setArchive(c.archive); err != nil {
		t.Fatal(err)
	}
	c.startSearchIndex()
	waitForSearchIndex(t, c)
	s := NewArchiveService(c)
	first, err := s.SearchEffectResources("apc", "测试人偶", 0, 1)
	if err != nil || len(first.Hits) != 1 || first.Hits[0].ID != "101" || first.NextCursor < 0 {
		t.Fatalf("APC=%+v %v", first, err)
	}
	second, err := s.SearchEffectResources("apc", "测试人偶", first.NextCursor, 1)
	if err != nil || len(second.Hits) != 1 || second.Hits[0].ID != "102" {
		t.Fatalf("APC page2=%+v %v", second, err)
	}
	all, err := s.SearchEffectResources("object", "", 0, 30)
	if err != nil || len(all.Hits) != 1 || all.Hits[0].ID != "12" {
		t.Fatalf("empty query=%+v %v", all, err)
	}
	empty, err := s.SearchEffectResources("particle", "", 1<<40, 30)
	if err != nil || len(empty.Hits) != 0 {
		t.Fatalf("oversized cursor=%+v %v", empty, err)
	}
}
