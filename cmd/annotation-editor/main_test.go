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

	updated := []byte("{\"version\":1,\"description\":\"updated\",\"relations\":{},\"rules\":[]}")
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
	if err != nil || !bytes.Contains(written, []byte("updated")) {
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
