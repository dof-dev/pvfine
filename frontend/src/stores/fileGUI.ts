import { defineStore } from "pinia";
import { ref } from "vue";
import { Events } from "@wailsio/runtime";

/** Epoch also changes when the same archive path is reopened. */
export const useFileGUIStore = defineStore("fileGUI", () => {
  const epoch = ref(0);
  const revision = ref(0);
  // Keep the tree selection outside the modal, which is unmounted on close.
  const skillTreeSelection = ref<{ job: string | null; kind: string; branch: string | null }>({ job: null, kind: "sp", branch: null });
  const skillTreeCellSelections = ref<Record<string, number>>({});
  for (const event of ["archive:opened", "archive:closed"]) {
    Events.On(event, () => { epoch.value++; revision.value++; skillTreeCellSelections.value = {}; });
  }
  for (const event of ["archive:changed", "archive:reloaded", "archive:batch-applied", "archive:index-ready", "archive:index-updated", "archive:registrations-changed"]) {
    Events.On(event, () => { revision.value++; });
  }
  return { epoch, revision, skillTreeSelection, skillTreeCellSelections };
});
