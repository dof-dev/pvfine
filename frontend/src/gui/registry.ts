import { defineAsyncComponent } from "vue";
import type { GUIFile, GUIProvider } from "./types";

function normalizePath(path: string): string {
  return path.replaceAll("\\", "/").replace(/^\/+|\/+$/g, "").toLowerCase();
}

const providers: GUIProvider[] = [
  {
    id: "shop",
    label: "商店",
    readOnly: false,
    matches: (file) => file.path.toLowerCase().endsWith(".shp"),
    component: defineAsyncComponent(() => import("../components/gui/ShopViewer.vue")),
  },
  {
    id: "world-drop",
    label: "全局掉率",
    readOnly: false,
    matches: (file) => normalizePath(file.path) === "etc/worlddrop.etc",
    component: defineAsyncComponent(() => import("../components/gui/WorldDropViewer.vue")),
  },
];

export function getGUIProvider(file: GUIFile): GUIProvider | null {
  return providers.find((provider) => provider.matches(file)) ?? null;
}
