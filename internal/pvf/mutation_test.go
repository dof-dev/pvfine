package pvf

import "testing"

func TestMutationJournalTracksOnlyEffectiveChanges(t *testing.T) {
	a := New()
	index, err := a.AddFileText("equipment/item.equ", "[name]\n`old`", TypeScript)
	if err != nil {
		t.Fatal(err)
	}
	a.ClearMutations()

	checkpoint := a.MutationCheckpoint()
	if err := a.SetText(index, "[name]\n`old`"); err != nil {
		t.Fatal(err)
	}
	if summary := a.MutationsSince(checkpoint); len(summary.Files) != 0 {
		t.Fatalf("identical edit recorded = %#v", summary.Files)
	}

	checkpoint = a.MutationCheckpoint()
	if err := a.SetText(index, "[name]\n`new`"); err != nil {
		t.Fatal(err)
	}
	summary := a.MutationsSince(checkpoint)
	if len(summary.Files) != 1 || summary.Files[0].Kind != MutationModified || summary.Files[0].Path != "equipment/item.equ" {
		t.Fatalf("modified summary = %#v", summary)
	}
	a.ClearMutations()

	checkpoint = a.MutationCheckpoint()
	added := a.AddFile("misc/new.txt", []byte("new"), TypeScript)
	summary = a.MutationsSince(checkpoint)
	if len(summary.Files) != 1 || summary.Files[0].Kind != MutationAdded || summary.Files[0].Index != added || !summary.Structural {
		t.Fatalf("added summary = %#v", summary)
	}

	checkpoint = a.MutationCheckpoint()
	if _, err := a.RemoveFiles([]int32{added}); err != nil {
		t.Fatal(err)
	}
	summary = a.MutationsSince(checkpoint)
	if len(summary.Files) != 1 || summary.Files[0].Kind != MutationRemoved || summary.Files[0].Path != "misc/new.txt" || !summary.Structural {
		t.Fatalf("removed summary = %#v", summary)
	}
}
