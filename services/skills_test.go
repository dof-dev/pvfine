package services

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func createSkillTestSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		// Windows requires Developer Mode or an elevated symlink privilege.
		if runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
			t.Skipf("symlink privilege unavailable: %v", err)
		}
		t.Fatal(err)
	}
}

func TestBuiltinSkillsInstall(t *testing.T) {
	s := NewSkillService()
	project := t.TempDir()
	ids := []string{}
	for _, skill := range s.List() {
		ids = append(ids, skill.ID)
	}
	results, err := s.Install(ids, "project", project, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 5 {
		t.Fatalf("results: %d", len(results))
	}
	for _, result := range results {
		if result.Status != "installed" {
			t.Fatalf("%+v", result)
		}
		expected := filepath.Join(project, ".agents", "skills", result.ID)
		if result.Path != expected {
			t.Fatalf("path: %s", result.Path)
		}
		content, err := os.ReadFile(filepath.Join(expected, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), "name: "+result.ID) {
			t.Fatal("missing frontmatter")
		}
		if _, err := os.Stat(filepath.Join(expected, "references", "mcp-workflow.md")); err != nil {
			t.Fatal(err)
		}
	}
	// Reinstallation must preserve an edited entrypoint and all custom files.
	entrypoint := filepath.Join(results[0].Path, "SKILL.md")
	if err := os.WriteFile(entrypoint, []byte("user edits"), 0644); err != nil {
		t.Fatal(err)
	}
	results, err = s.Install(ids, "project", project, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.Status != "skipped" {
			t.Fatalf("%+v", result)
		}
	}
	content, err := os.ReadFile(entrypoint)
	if err != nil || string(content) != "user edits" {
		t.Fatalf("user edits replaced: %v", err)
	}
}

func TestBuiltinSkillsRejectInvalidRequest(t *testing.T) {
	s := NewSkillService()
	root := filepath.Join(t.TempDir(), "new")
	for _, ids := range [][]string{nil, {"../../escape"}, {"pvfine-quests", "unknown"}, {"pvfine-quests", "pvfine-quests"}} {
		if _, err := s.Install(ids, "custom", root, false); err == nil {
			t.Fatalf("accepted %v", ids)
		}
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("invalid request created directories")
	}
	for _, target := range []string{"project", "custom", "unknown"} {
		if _, err := s.ResolveDirectory(target, "relative"); err == nil {
			t.Fatalf("accepted %s", target)
		}
	}
}

func TestBuiltinSkillsFileAndSymlinkConflict(t *testing.T) {
	for _, conflict := range []string{"file", "symlink"} {
		t.Run(conflict, func(t *testing.T) {
			s := NewSkillService()
			root, external := t.TempDir(), t.TempDir()
			destination := filepath.Join(root, "pvfine-quests")
			if conflict == "file" {
				if err := os.WriteFile(destination, []byte("keep"), 0644); err != nil {
					t.Fatal(err)
				}
			} else {
				createSkillTestSymlink(t, external, destination)
			}
			results, err := s.Install([]string{"pvfine-quests", "pvfine-upgrade"}, "custom", root, false)
			if err != nil {
				t.Fatal(err)
			}
			if results[0].Status != "skipped" || results[1].Status != "installed" {
				t.Fatalf("%+v", results)
			}
			if conflict == "file" {
				content, err := os.ReadFile(destination)
				if err != nil || string(content) != "keep" {
					t.Fatalf("conflicting file changed: %v", err)
				}
			}
			entries, err := os.ReadDir(external)
			if err != nil || len(entries) != 0 {
				t.Fatalf("external directory changed: %v", err)
			}
		})
	}
}

func TestBuiltinSkillDefaultDirectories(t *testing.T) {
	s := NewSkillService()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	for target, tool := range map[string]string{"global": ".agents", "claude": ".claude"} {
		got, err := s.ResolveDirectory(target, "")
		if err != nil || got != filepath.Join(home, tool, "skills") {
			t.Fatalf("%s: %s %v", target, got, err)
		}
	}
}

func TestBuiltinSkillsOverwritePreservesExtraFiles(t *testing.T) {
	s := NewSkillService()
	root := t.TempDir()
	id := "pvfine-quests"
	if _, err := s.Install([]string{id}, "custom", root, false); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, id)
	for _, relative := range []string{"SKILL.md", "references/mcp-workflow.md", "references/custom.md"} {
		if err := os.WriteFile(filepath.Join(destination, filepath.FromSlash(relative)), []byte("local content"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	results, err := s.Install([]string{id, "pvfine-upgrade"}, "custom", root, true)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Status != "updated" || results[1].Status != "installed" {
		t.Fatalf("%+v", results)
	}
	for _, relative := range []string{"SKILL.md", "references/mcp-workflow.md"} {
		expected, err := builtinSkills.ReadFile("builtin_skills/" + id + "/" + relative)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(destination, filepath.FromSlash(relative)))
		if err != nil || string(got) != string(expected) {
			t.Fatalf("not updated: %s %v", relative, err)
		}
	}
	extra, err := os.ReadFile(filepath.Join(destination, "references", "custom.md"))
	if err != nil || string(extra) != "local content" {
		t.Fatalf("extra file changed: %v", err)
	}
}

func TestBuiltinSkillsOverwriteRejectsConflictsBeforeWriting(t *testing.T) {
	for _, conflict := range []string{"root-symlink", "root-file", "reference-symlink", "reference-directory", "reference-parent-symlink"} {
		t.Run(conflict, func(t *testing.T) {
			s := NewSkillService()
			root, external := t.TempDir(), t.TempDir()
			id := "pvfine-quests"
			destination := filepath.Join(root, id)
			if err := os.WriteFile(filepath.Join(external, "marker"), []byte("keep"), 0644); err != nil {
				t.Fatal(err)
			}
			if conflict == "root-symlink" {
				createSkillTestSymlink(t, external, destination)
			} else if conflict == "root-file" {
				if err := os.WriteFile(destination, []byte("keep"), 0644); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := s.Install([]string{id}, "custom", root, false); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(destination, "SKILL.md"), []byte("local entrypoint"), 0644); err != nil {
					t.Fatal(err)
				}
				reference := filepath.Join(destination, "references", "mcp-workflow.md")
				if err := os.Remove(reference); err != nil {
					t.Fatal(err)
				}
				switch conflict {
				case "reference-symlink":
					createSkillTestSymlink(t, filepath.Join(external, "marker"), reference)
				case "reference-directory":
					if err := os.Mkdir(reference, 0755); err != nil {
						t.Fatal(err)
					}
				case "reference-parent-symlink":
					parent := filepath.Dir(reference)
					if err := os.Remove(parent); err != nil {
						t.Fatal(err)
					}
					createSkillTestSymlink(t, external, parent)
				}
			}
			results, err := s.Install([]string{id}, "custom", root, true)
			if err != nil {
				t.Fatal(err)
			}
			if results[0].Status != "failed" {
				t.Fatalf("%+v", results)
			}
			if conflict != "root-symlink" && conflict != "root-file" {
				entry, err := os.ReadFile(filepath.Join(destination, "SKILL.md"))
				if err != nil || string(entry) != "local entrypoint" {
					t.Fatalf("entrypoint changed on failed preflight: %v", err)
				}
			}
			marker, err := os.ReadFile(filepath.Join(external, "marker"))
			if err != nil || string(marker) != "keep" {
				t.Fatalf("external file changed: %v", err)
			}
			entries, err := os.ReadDir(external)
			if err != nil || len(entries) != 1 {
				t.Fatalf("external directory changed: %v", err)
			}
		})
	}
}
