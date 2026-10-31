import { defineAsyncComponent } from "vue";
import type { GUIFile, GUIProvider } from "./types";

function normalizePath(path: string): string {
  return path.replaceAll("\\", "/").replace(/^\/+|\/+$/g, "").toLowerCase();
}

export const guiProviders: readonly GUIProvider[] = [
  {
    id: "shop",
    label: "商店",
    launcher: { fileTypes: ".shp", paths: ["npc/shop/new.shp"], queries: ["*.shp"] },
    readOnly: false,
    matches: (file) => file.path.toLowerCase().endsWith(".shp"),
    component: defineAsyncComponent(() => import("../components/gui/ShopViewer.vue")),
  },
  {
    id: "world-drop",
    label: "全局掉率",
    launcher: { fileTypes: "etc/worlddrop.etc、etc/(r)worlddrop.etc", paths: ["etc/worlddrop.etc", "etc/(r)worlddrop.etc"], queries: ["*worlddrop.etc"] },
    readOnly: false,
    matches: (file) => ["etc/worlddrop.etc", "etc/(r)worlddrop.etc"].includes(normalizePath(file.path)),
    component: defineAsyncComponent(() => import("../components/gui/WorldDropViewer.vue")),
  },
  {
    id: "skill",
    label: "技能",
    launcher: { fileTypes: ".skl", paths: ["skill/new.skl"], queries: ["*.skl"] },
    readOnly: false,
    matches: (file) => file.path.toLowerCase().endsWith(".skl"),
    component: defineAsyncComponent(() => import("../components/gui/SkillViewer.vue")),
  },
  {
    id: "quest",
    label: "任务",
    launcher: { fileTypes: ".qst", paths: ["quest/new.qst"], queries: ["*.qst"] },
    readOnly: false,
    matches: (file) => file.path.toLowerCase().endsWith(".qst"),
    component: defineAsyncComponent(() => import("../components/gui/QuestViewer.vue")),
  },
  {
    id: "upgrade",
    label: "强化/增幅",
    launcher: { fileTypes: "etc/upgrade.etc、etc/amplifyupgrade.etc", paths: ["etc/upgrade.etc", "etc/amplifyupgrade.etc"], queries: ["etc/upgrade.etc", "etc/amplifyupgrade.etc"] },
    readOnly: false,
    matches: (file) => ["etc/upgrade.etc", "etc/amplifyupgrade.etc"].includes(normalizePath(file.path)),
    component: defineAsyncComponent(() => import("../components/gui/UpgradeViewer.vue")),
  },
  {
    id: "forge",
    label: "锻造",
    launcher: { fileTypes: "etc/upgrade_separate.etc", paths: ["etc/upgrade_separate.etc"], queries: ["etc/upgrade_separate.etc"] },
    readOnly: false,
    matches: (file) => normalizePath(file.path) === "etc/upgrade_separate.etc",
    component: defineAsyncComponent(() => import("../components/gui/ForgeViewer.vue")),
  },
  {
    id: "item",
    label: "物品",
    launcher: { fileTypes: ".equ、.stk", paths: ["equipment/new.equ", "stackable/new.stk"], queries: ["*.equ", "*.stk"] },
    readOnly: false,
    matches: (file) => /\.(equ|stk)$/i.test(file.path),
    component: defineAsyncComponent(() => import("../components/gui/ItemViewer.vue")),
  },
  {
    id: "independent-drop",
    label: "独立掉落",
    launcher: { fileTypes: "etc/independent_drop.etc、etc/independentdrop/*.etc", paths: ["etc/independent_drop.etc"], queries: ["etc/independent_drop.etc", "etc/independentdrop/"] },
    readOnly: false,
    matches: (file) => normalizePath(file.path) === "etc/independent_drop.etc" || /^etc\/independentdrop\/.+\.etc$/.test(normalizePath(file.path)),
    component: defineAsyncComponent(() => import("../components/gui/IndependentDropViewer.vue")),
  },
];

export function getGUIProvider(file: GUIFile): GUIProvider | null {
  return guiProviders.find((provider) => provider.matches(file)) ?? null;
}
