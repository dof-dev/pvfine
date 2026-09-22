package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRulesAPIWritesBackupAndRejectsInvalidRules(t *testing.T) {
	dir := t.TempDir()
	rulesPath := filepath.Join(dir, "default.json")
	runtimePath := filepath.Join(dir, "config", "annotations.json")
	original := []byte("{\"version\":1,\"relations\":{},\"rules\":[]}\n")
	if err := os.WriteFile(rulesPath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	server := &editorServer{rulesPath: rulesPath, runtimePath: runtimePath, token: "test-token"}

	updated := []byte(`{"version":1,"description":"updated","relations":{},"fields":[{"id":"equ.rarity","match":{"extensions":[".equ"]},"target":{"kind":"token","section":"rarity","index":0},"annotation":{"title":"稀有度","type":"enum","values":{"4":"史诗"}},"preview":{"provider":"equ","role":"rarity","group":"summary","order":10,"format":"enum"}}],"rules":[]}`)
	request := httptest.NewRequest(http.MethodPut, "/api/rules", bytes.NewReader(updated))
	request.Header.Set("X-Annotation-Editor-Token", "test-token")
	response := httptest.NewRecorder()
	server.routes().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	backup, err := os.ReadFile(rulesPath + ".bak")
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatalf("backup = %q, err = %v", backup, err)
	}
	written, err := os.ReadFile(rulesPath)
	if err != nil || !bytes.Contains(written, []byte("updated")) || !bytes.Contains(written, []byte("equ.rarity")) {
		t.Fatalf("written = %q, err = %v", written, err)
	}
	runtimeRules, err := os.ReadFile(runtimePath)
	if err != nil || !bytes.Equal(runtimeRules, written) {
		t.Fatalf("runtime rules = %q, err = %v", runtimeRules, err)
	}

	invalid := httptest.NewRequest(http.MethodPut, "/api/rules", bytes.NewReader([]byte("{}")))
	invalid.Header.Set("X-Annotation-Editor-Token", "test-token")
	invalidResponse := httptest.NewRecorder()
	server.routes().ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d", invalidResponse.Code)
	}
	afterInvalid, err := os.ReadFile(rulesPath)
	if err != nil || !bytes.Equal(afterInvalid, written) {
		t.Fatalf("invalid request changed rules: %q, err = %v", afterInvalid, err)
	}
}

func TestPreviewAPIUsesSubmittedRulesAndIncludesPathAnnotations(t *testing.T) {
	dir := t.TempDir()
	rulesPath := filepath.Join(dir, "default.json")
	if err := os.WriteFile(rulesPath, []byte("{\"version\":1,\"relations\":{},\"rules\":[]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := &editorServer{rulesPath: rulesPath, token: "test-token"}
	body := []byte(`{
		"path":"equipment/example.equ",
		"text":"[rarity]\n4",
		"document":{
			"version":1,
			"relations":{},
			"rules":[
				{"id":"rarity","match":{"extensions":[".equ"]},"target":{"kind":"token","section":"rarity","index":0},"annotation":{"title":"装备品级","type":"enum","values":{"4":"史诗"}}},
				{"id":"path","match":{"glob":"equipment/**"},"target":{"kind":"path"},"annotation":{"title":"装备配置","type":"text"}}
			]
		}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/api/preview", bytes.NewReader(body))
	request.Header.Set("X-Annotation-Editor-Token", "test-token")
	response := httptest.NewRecorder()
	server.routes().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var result struct {
		Annotations     []map[string]any `json:"annotations"`
		PathAnnotations []map[string]any `json:"pathAnnotations"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Annotations) != 1 || result.Annotations[0]["title"] != "史诗" {
		t.Fatalf("annotations = %#v", result.Annotations)
	}
	if len(result.PathAnnotations) != 1 || result.PathAnnotations[0]["title"] != "装备配置" {
		t.Fatalf("path annotations = %#v", result.PathAnnotations)
	}
}

func TestVersionedRulesSaveAndPreview(t *testing.T) {
	dir := t.TempDir()
	rulesPath := filepath.Join(dir, "annotations.json")
	if err := os.WriteFile(rulesPath, []byte(`{"version":1,"rules":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	server := &editorServer{rulesPath: rulesPath, runtimePath: filepath.Join(dir, "runtime.json"), token: "test-token"}
	document := json.RawMessage(`{"version":1,"fields":[{"id":"name","pvfVersions":["90US","90CN"],"target":{"kind":"section","section":"name"},"annotation":{"title":"name","type":"text"}}],"rules":[{"id":"path","pvfVersions":["90US","110US"],"target":{"kind":"path"},"annotation":{"title":"path","type":"text"}}]}`)
	call := func(method, path string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, bytes.NewReader(body))
		request.Header.Set("X-Annotation-Editor-Token", "test-token")
		response := httptest.NewRecorder()
		server.routes().ServeHTTP(response, request)
		return response
	}
	if response := call(http.MethodPut, "/api/rules", document); response.Code != http.StatusOK {
		t.Fatalf("save: %s", response.Body.String())
	}
	response := call(http.MethodGet, "/api/rules", nil)
	var saved struct {
		Fields []struct {
			PVFVersions []string `json:"pvfVersions"`
		} `json:"fields"`
		Rules []struct {
			PVFVersions []string `json:"pvfVersions"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Fields) != 1 || len(saved.Fields[0].PVFVersions) != 2 || len(saved.Rules) != 1 || len(saved.Rules[0].PVFVersions) != 2 {
		t.Fatalf("versions lost: %s", response.Body.String())
	}
	for _, submitted := range []bool{false, true} {
		for _, tc := range []struct {
			version            string
			annotations, paths int
		}{{"90US", 1, 1}, {"90CN", 1, 0}, {"110US", 0, 1}, {"", 0, 0}} {
			payload := map[string]any{"path": "a.equ", "text": "[name]\n`example`"}
			if tc.version != "" {
				payload["pvfVersion"] = tc.version
			}
			if submitted {
				payload["document"] = document
			}
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			response := call(http.MethodPost, "/api/preview", body)
			if response.Code != http.StatusOK {
				t.Fatalf("preview: %s", response.Body.String())
			}
			var result struct {
				Annotations     []any `json:"annotations"`
				PathAnnotations []any `json:"pathAnnotations"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Annotations) != tc.annotations || len(result.PathAnnotations) != tc.paths {
				t.Fatalf("%s submitted=%v: %s", tc.version, submitted, response.Body.String())
			}
		}
	}
	if response := call(http.MethodPost, "/api/preview", []byte(`{"pvfVersion":"invalid"}`)); response.Code != http.StatusBadRequest {
		t.Fatal("invalid preview version accepted")
	}
	invalid := bytes.ReplaceAll(document, []byte("90US"), []byte("invalid"))
	if response := call(http.MethodPut, "/api/rules", invalid); response.Code != http.StatusBadRequest {
		t.Fatal("invalid config version accepted")
	}
}
