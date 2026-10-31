import type { Component } from "vue";

export interface GUIFile {
  index: number;
  path: string;
  text: string;
  editable: boolean;
}

export interface GUIProvider {
  id: string;
  label: string;
  readOnly: boolean;
  launcher: {
    fileTypes: string;
    paths: string[];
    queries: string[];
  };
  matches: (file: GUIFile) => boolean;
  component: Component;
}
