import type { Component } from "vue";

export interface PreviewFile {
  index: number;
  path: string;
  text: string;
  editable: boolean;
}

export interface PreviewComponentProps {
  file: PreviewFile;
  active: boolean;
}

export interface PreviewProvider {
  id: string;
  label: string;
  matches: (file: PreviewFile) => boolean;
  component: Component;
  chrome?: "default" | "game-tooltip";
}
