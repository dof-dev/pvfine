import type { TreeOption } from "naive-ui";
import type { ExportPreviewFile } from "../../bindings/pvfine/services/models";

export function exportFileKey(path: string): string {
  return `file:${path}`;
}

export function buildExportTree(files: ExportPreviewFile[]): TreeOption[] {
  const roots: TreeOption[] = [];
  const directories = new Map<string, TreeOption>();
  for (const file of files) {
    const parts = file.path.split("/");
    let children = roots;
    let parent = "";
    for (const part of parts.slice(0, -1)) {
      parent = parent ? `${parent}/${part}` : part;
      let directory = directories.get(parent);
      if (!directory) {
        directory = { key: `dir:${parent}`, label: part, children: [], isLeaf: false };
        directories.set(parent, directory);
        children.push(directory);
      }
      children = directory.children!;
    }
    children.push({
      key: exportFileKey(file.path),
      label: parts[parts.length - 1],
      isLeaf: true,
      checkboxDisabled: file.required,
      path: file.path,
      required: file.required,
    });
  }
  function sort(nodes: TreeOption[]): void {
    nodes.sort((a, b) => {
      if (a.isLeaf !== b.isLeaf) return a.isLeaf ? 1 : -1;
      return String(a.label).localeCompare(String(b.label));
    });
    for (const node of nodes) if (node.children) sort(node.children);
  }
  sort(roots);
  return roots;
}
