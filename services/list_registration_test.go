package services

import (
	"strings"
	"testing"

	"pvfine/internal/pvf"
)

func TestArchiveServiceRegisterFileToList(t *testing.T) {
	a := pvf.New()
	fileIndex := mustAddText(t, a, "equipment/character/new.equ", "[name]\n`新装备`", pvf.TypeScript)
	mustAddText(t, a, "equipment/character/old.equ", "[name]\n`旧装备`", pvf.TypeScript)
	mustAddText(t, a, "equipment/equipment.lst", "1008 `character/old.equ`", pvf.TypeScript)

	c := NewCore()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	service := NewArchiveService(c)

	options, err := service.ListRegistrationOptions(fileIndex)
	if err != nil {
		t.Fatal(err)
	}
	if len(options.Targets) == 0 {
		t.Fatal("no list registration targets")
	}
	var target *ListRegistrationTarget
	for _, candidate := range options.Targets {
		if candidate.ListPath == "equipment/equipment.lst" {
			target = candidate
			break
		}
	}
	if target == nil {
		t.Fatalf("equipment target missing: %#v", options.Targets)
	}
	if target.EntryPath != "character/new.equ" || target.SuggestedID != "1009" {
		t.Fatalf("target = %#v", target)
	}

	registration, err := service.RegisterFileToList(fileIndex, target.ListPath, target.SuggestedID)
	if err != nil {
		t.Fatal(err)
	}
	if registration.ListPath != target.ListPath || registration.EntryPath != target.EntryPath {
		t.Fatalf("registration = %#v", registration)
	}
	listIndex, ok := a.Find("equipment/equipment.lst")
	if !ok {
		t.Fatal("equipment list disappeared")
	}
	pairs, err := a.ListPairs(listIndex)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, pair := range pairs {
		if pair.ID == "1009" && strings.EqualFold(pair.Path, "character/new.equ") {
			found = true
		}
	}
	if !found {
		t.Fatalf("registered pair missing: %#v", pairs)
	}
	if _, err := service.RegisterFileToList(fileIndex, target.ListPath, "1010"); err == nil {
		t.Fatal("duplicate file registration succeeded")
	}
}

func TestArchiveServiceIndexHashTargetsRequirePaged110(t *testing.T) {
	c := NewCore()
	a := pvf.New()
	if err := c.setArchive(a); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.closeArchive)
	if _, err := NewArchiveService(c).IndexHashTargets(); err == nil {
		t.Fatal("non-Paged110 archive exposed indexhash targets")
	}
}
