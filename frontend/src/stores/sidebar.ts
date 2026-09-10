import { defineStore } from "pinia";
import { ref } from "vue";

export type SidebarPanel = "filesets" | "bookmarks";

/** 右侧工作区侧栏状态。 */
export const useSidebarStore = defineStore("sidebar", () => {
  const visible = ref(true);
  const activePanel = ref<SidebarPanel>("filesets");

  function show(panel?: SidebarPanel): void {
    if (panel) activePanel.value = panel;
    visible.value = true;
  }

  /** 仅切换当前面板，不改变侧栏的展开状态。 */
  function setPanel(panel: SidebarPanel): void {
    activePanel.value = panel;
  }

  function toggle(): void {
    visible.value = !visible.value;
  }

  function close(): void {
    visible.value = false;
  }

  return { visible, activePanel, show, setPanel, toggle, close };
});
