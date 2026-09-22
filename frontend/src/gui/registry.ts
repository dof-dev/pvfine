import { defineAsyncComponent } from "vue";
import type { GUIFile, GUIProvider } from "./types";

const providers: GUIProvider[] = [
  {
    id: "shop",
    label: "商店",
    readOnly: true,
    matches: (file) => file.path.toLowerCase().endsWith(".shp"),
    component: defineAsyncComponent(() => import("../components/gui/ShopViewer.vue")),
  },
];

export function getGUIProvider(file: GUIFile): GUIProvider | null {
  return providers.find((provider) => provider.matches(file)) ?? null;
}
