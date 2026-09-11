import AniPreview from "../components/previews/AniPreview.vue";
import type { PreviewFile, PreviewProvider } from "./types";

const providers: PreviewProvider[] = [
  {
    id: "ani",
    label: "ANI 动画",
    matches: (file) => file.path.toLowerCase().endsWith(".ani"),
    component: AniPreview,
  },
];

export function getPreviewProvider(file: PreviewFile): PreviewProvider | null {
  return providers.find((provider) => provider.matches(file)) ?? null;
}
