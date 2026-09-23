package annotations

import (
	"reflect"
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

func TestPVFVersionRulesAndSourceRoundTrip(t *testing.T) {
	document := Document{Version: 1}
	for i, versions := range [][]string{nil, {}, {"90US"}, {"90CN", "110US"}} {
		document.Rules = append(document.Rules, Rule{ID: string(rune('a' + i)), PVFVersions: versions,
			Target: TargetSpec{Kind: "path"}, Annotation: AnnotationSpec{Title: string(rune('a' + i)), Type: "text"}})
	}
	engine, err := Compile(document)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		version string
		want    int
	}{{"90US", 3}, {"90CN", 3}, {"110US", 3}, {"", 2}, {"future", 2}} {
		t.Run(tc.version, func(t *testing.T) {
			runtime := engine.ForVersion(tc.version)
			if got := len(runtime.AnnotatePath("a.equ", false)[0].RuleIDs); got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
			if !reflect.DeepEqual(runtime.Document(), engine.Document()) {
				t.Fatal("runtime lost original document")
			}
			data, err := Marshal(runtime.Document())
			if err != nil {
				t.Fatal(err)
			}
			restored, err := Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(restored.Rules[3].PVFVersions, []string{"90CN", "110US"}) {
				t.Fatal("serialization lost versions")
			}
			if got := len(runtime.ForVersion("90US").AnnotatePath("a.equ", false)[0].RuleIDs); got != 3 {
				t.Fatal("switching versions lost rules")
			}
		})
	}
}

func TestPVFVersionFieldIntersectionAndImplicitFallback(t *testing.T) {
	for _, tc := range []struct {
		name        string
		field, rule []string
		version     string
		want        string
		previews    int
	}{
		{"intersection", []string{"90US", "90CN"}, []string{"90CN", "110US"}, "90CN", "explicit", 1},
		{"field excludes rule", []string{"90US", "90CN"}, []string{"90CN", "110US"}, "110US", "", 0},
		{"rule excludes field falls back", []string{"90US", "90CN"}, []string{"90CN", "110US"}, "90US", "field:shared", 1},
		{"empty intersection", []string{"90US"}, []string{"110US"}, "110US", "", 0},
		{"unrestricted rule restricted field", []string{"90CN"}, nil, "90US", "", 0},
		{"restricted rule unrestricted field", nil, []string{"110US"}, "90CN", "field:shared", 1},
		{"unknown restricted field", []string{"90US"}, nil, "", "", 0},
		{"unknown unrestricted field", nil, []string{"90US"}, "", "field:shared", 1},
		{"all unrestricted", nil, nil, "", "explicit", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, err := Compile(Document{Version: 1,
				Fields: []FieldDefinition{{ID: "shared", PVFVersions: tc.field, Target: TargetSpec{Kind: "token", Section: "name", Index: intPtr(0)}, Annotation: AnnotationSpec{Title: "name", Type: "text"}, Preview: &PreviewSpec{Provider: "equ", Role: "name", Group: "header", Format: "text"}}},
				Rules:  []Rule{{ID: "explicit", Field: "shared", PVFVersions: tc.rule}},
			})
			if err != nil {
				t.Fatal(err)
			}
			runtime := engine.ForVersion(tc.version)
			view := pvf.ParseScriptView("[name]\n`example`")
			results := runtime.Annotate("a.equ", view, nil)
			if tc.want == "" {
				if len(results) != 0 {
					t.Fatalf("unexpected results: %#v", results)
				}
			} else if len(results) != 1 || !reflect.DeepEqual(results[0].RuleIDs, []string{tc.want}) {
				t.Fatalf("results = %#v", results)
			}
			if got := len(runtime.ExtractPreviewFields("a.equ", view, "equ")); got != tc.previews {
				t.Fatalf("preview fields = %d", got)
			}
		})
	}
}

func TestPVFVersionNonReferenceRuleDoesNotSuppressField(t *testing.T) {
	field := FieldDefinition{ID: "shared", Target: TargetSpec{Kind: "path"}, Annotation: AnnotationSpec{Title: "shared", Type: "text"}}
	engine, err := Compile(Document{Version: 1, Fields: []FieldDefinition{field}, Rules: []Rule{{ID: "explicit", PVFVersions: []string{"110US"}, Target: field.Target, Annotation: field.Annotation}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"90US", "110US"} {
		results := engine.ForVersion(version).AnnotatePath("a.equ", false)
		want := "explicit"
		if version == "90US" {
			want = "field:shared"
		}
		if len(results) != 1 || !reflect.DeepEqual(results[0].RuleIDs, []string{want}) {
			t.Fatalf("%s: %#v", version, results)
		}
	}
}

func TestPVFVersionValidation(t *testing.T) {
	for _, value := range []string{"90us", "unknown", "", "120US"} {
		for _, field := range []bool{false, true} {
			document := Document{Version: 1}
			if field {
				document.Fields = []FieldDefinition{{ID: "f", PVFVersions: []string{value}, Target: TargetSpec{Kind: "path"}, Annotation: AnnotationSpec{Title: "f", Type: "text"}}}
			} else {
				document.Rules = []Rule{{ID: "r", PVFVersions: []string{value}, Target: TargetSpec{Kind: "path"}, Annotation: AnnotationSpec{Title: "r", Type: "text"}}}
			}
			if _, err := Compile(document); err == nil || !strings.Contains(err.Error(), "pvfVersions") {
				t.Fatalf("invalid version accepted: %q, field=%v, err=%v", value, field, err)
			}
			if _, err := Marshal(document); err == nil {
				t.Fatal("invalid version serialized")
			}
		}
	}
}
