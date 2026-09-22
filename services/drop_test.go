package services

import (
	"errors"
	"os"
	"testing"

	"pvfine/internal/pvf"
)

func TestDecodeDropRateGroupSupportsLegacySixValuesAndQuantizesToHundredths(t *testing.T) {
	legacy := []int64{679750, 932250, 990000, 1000000, 1000001, 1000002}
	extended := append(append([]int64(nil), legacy...), 123456)

	for name, group := range map[string][]int64{"legacy": legacy, "extended": extended} {
		t.Run(name, func(t *testing.T) {
			rates, err := decodeDropRateGroup(group)
			if err != nil {
				t.Fatal(err)
			}
			want := []int{6798, 2525, 577, 100, 0}
			if got := rates; len(got) != len(want) {
				t.Fatalf("rates = %#v, want %#v", got, want)
			} else {
				for index := range want {
					if got[index] != want[index] {
						t.Fatalf("rates = %#v, want %#v", got, want)
					}
				}
			}
			sum := 0
			for _, rate := range rates {
				sum += rate
			}
			if sum != dropRateTotal {
				t.Fatalf("sum = %d, want %d", sum, dropRateTotal)
			}
		})
	}
}

func TestDecodeDropRateGroupRejectsDecreasingOrInvalidTerminal(t *testing.T) {
	tests := []struct {
		name  string
		group []int64
	}{
		{name: "short", group: []int64{1, 2, 3}},
		{name: "decreasing", group: []int64{100, 200, 150, 1000000, 1000001, 1000002}},
		{name: "terminal too small", group: []int64{100, 200, 300, 400, 999999, 1000000}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := decodeDropRateGroup(test.group); err == nil {
				t.Fatal("expected invalid group error")
			}
		})
	}
}

func TestDropRateServiceReadsAndAppliesSevenValueGroups(t *testing.T) {
	c := NewCore()
	a := makeDropRateArchive(t)
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)

	service := NewDropService(c)
	document, err := service.Read()
	if err != nil {
		t.Fatal(err)
	}
	if document.PVFVersion != "90US" || len(document.Sections) != 4 {
		t.Fatalf("document = %#v", document)
	}
	if got := document.Sections[0].Groups[0].Rates; len(got) != 5 {
		t.Fatalf("hell rates = %#v", got)
	}
	if got := document.Sections[0].Groups[0].Rates[0]; got != 6000 {
		t.Fatalf("hell first rate = %d, want 6000", got)
	}

	monster := document.Sections[2]
	monster.Groups[0].Rates = []int{5000, 2500, 1500, 1000, 0}
	result, err := service.Apply(DropRateApplyRequest{
		Revision: document.Revision,
		Sections: document.Sections,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.FileIndexes) != 1 || result.Revision == document.Revision {
		t.Fatalf("apply result = %#v", result)
	}

	index, ok := a.Find("etc/itemdropinfo_monseter.etc")
	if !ok {
		t.Fatal("monster drop file missing")
	}
	raw, err := a.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := a.ParseScriptDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	section, ok := parsed.Section([]string{dropRateSectionName}, 0)
	if !ok {
		t.Fatal("drop section missing after apply")
	}
	values, err := scriptIntegerValues(section.Values(), "monster")
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := []int64{500000, 750000, 900000, 1000000, 1000001, 1000002, 700}
	for index, want := range wantPrefix {
		if values[index] != want {
			t.Fatalf("value[%d] = %d, want %d", index, values[index], want)
		}
	}
}

func TestDropRateApplyRejectsStaleOrInvalidRequestWithoutPartialMutation(t *testing.T) {
	c := NewCore()
	a := makeDropRateArchive(t)
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)

	service := NewDropService(c)
	document, err := service.Read()
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotDropFile(t, a, "etc/itemdropinfo_monseter.etc")

	document.Sections[2].Groups[0].Rates = []int{5000, 2500, 1500, 1000, 1}
	if _, err := service.Apply(DropRateApplyRequest{Revision: document.Revision, Sections: document.Sections}); err == nil {
		t.Fatal("expected invalid sum error")
	}
	if after := snapshotDropFile(t, a, "etc/itemdropinfo_monseter.etc"); string(after) != string(before) {
		t.Fatal("invalid request changed the archive")
	}

	document, err = service.Read()
	if err != nil {
		t.Fatal(err)
	}
	document.Sections[2].Groups[0].Rates = []int{5000, 2500, 1500, 1000, 0}
	if _, err := service.Apply(DropRateApplyRequest{Revision: document.Revision, Sections: document.Sections}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(DropRateApplyRequest{Revision: document.Revision, Sections: document.Sections}); !errors.Is(err, ErrDropRateStale) {
		t.Fatalf("stale error = %v", err)
	}
}

func TestValidateDropRates(t *testing.T) {
	if err := validateDropRates([]int{2000, 2000, 2000, 2000, 2000}); err != nil {
		t.Fatal(err)
	}
	for _, rates := range [][]int{
		{2000, 2000, 2000, 2000},
		{2000, 2000, 2000, 2000, 2001},
		{-1, 2000, 2000, 2000, 4000},
	} {
		if err := validateDropRates(rates); err == nil {
			t.Fatalf("expected invalid rates: %#v", rates)
		}
	}
}

func TestSupportedDropRateVersion(t *testing.T) {
	for _, version := range []string{"90US", "90CN"} {
		if !supportedDropRateVersion(version) {
			t.Fatalf("version %s should be supported", version)
		}
	}
	for _, version := range []string{"110US", "", "recovered"} {
		if supportedDropRateVersion(version) {
			t.Fatalf("version %s should be rejected", version)
		}
	}
}

func TestDropRateServiceReadsRealSupportedArchive(t *testing.T) {
	path := os.Getenv("PVF_TESTFILE")
	if path == "" {
		t.Skip("PVF_TESTFILE 未设置")
	}
	a, err := pvf.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	if !supportedDropRateVersion(a.ClientVersion()) {
		t.Skipf("归档版本 %s 不属于掉率工具支持范围", a.ClientVersion())
	}

	document, err := NewDropService(c).Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Sections) != len(dropRateFileSpecs) {
		t.Fatalf("section count = %d, want %d", len(document.Sections), len(dropRateFileSpecs))
	}
	for index, section := range document.Sections {
		if len(section.Groups) != dropRateFileSpecs[index].groupCount {
			t.Fatalf("section %s groups = %d, want %d", section.Key, len(section.Groups), dropRateFileSpecs[index].groupCount)
		}
		for _, group := range section.Groups {
			if err := validateDropRates(group.Rates); err != nil {
				t.Fatalf("section %s rates %#v: %v", section.Key, group.Rates, err)
			}
		}
	}
}

func makeDropRateArchive(t *testing.T) *pvf.Archive {
	t.Helper()
	a := pvf.New()
	files := map[string]string{
		"etc/itemdropinfo_monster_hell.etc":   "[basis of rarity dicision]\n2 600000 844900 950000 1000000 1000001 1000002 700 100000 400000 500000 800000 1000001 1000002 800\n",
		"etc/itemdropinfo_clearreward.etc":    "[basis of rarity dicision]\n679750 932250 990000 1000000 1000001 1000002 900\n",
		"etc/itemdropinfo_monseter.etc":       "[basis of rarity dicision]\n600000 844900 950000 1000000 1000001 1000002 700 330100 543690 999706 1000000 1000001 1000002 701 500000 944900 999500 1000000 1000001 1000002 702 700000 944900 995000 1000000 1000001 1000002 703\n",
		"etc/itemdropinfo_monseter_extra.etc": "[basis of rarity dicision]\n600000 824900 950000 1000000 1000001 1000002 800 685000 992600 999500 1000000 1000001 1000002 801 500000 944900 999500 1000000 1000001 1000002 802 700000 944900 995000 1000000 1000001 1000002 803\n",
	}
	for path, text := range files {
		if _, err := a.AddFileText(path, text, pvf.TypeScript); err != nil {
			t.Fatal(err)
		}
	}
	return a
}

func snapshotDropFile(t *testing.T, a *pvf.Archive, path string) []byte {
	t.Helper()
	index, ok := a.Find(path)
	if !ok {
		t.Fatalf("file missing: %s", path)
	}
	raw, err := a.RawBytes(index)
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), raw...)
}
