import AniPreview from "../components/previews/AniPreview.vue";
import EquipmentPreview from "../components/previews/EquipmentPreview.vue";
import type { PreviewFile, PreviewProvider } from "./types";

const equipmentTypeSection = /^[ \t]*\[equipment type\][ \t]*(?:#.*)?$/i;

const providers: PreviewProvider[] = [
  {
    id: "ani",
    label: "ANI 动画",
    matches: (file) => file.path.toLowerCase().endsWith(".ani"),
    component: AniPreview,
  },
  {
    id: "equ",
    label: "装备预览",
    matches: (file) => file.path.toLowerCase().endsWith(".equ") &&
      file.text.split(/\r\n|\r|\n/).some((line) => equipmentTypeSection.test(line)),
    component: EquipmentPreview,
    chrome: "game-tooltip",
  },
];

export function getPreviewProvider(file: PreviewFile): PreviewProvider | null {
  return providers.find((provider) => provider.matches(file)) ?? null;
}
