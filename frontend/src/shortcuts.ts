import { defaultKeymap, historyKeymap } from "@codemirror/commands";
import { foldKeymap } from "@codemirror/language";
import { searchKeymap } from "@codemirror/search";

export const shortcutCommands = [
  { id: "archive.open", label: "打开归档", group: "文件", defaultBinding: "Mod+KeyO" },
  { id: "workspace.save", label: "保存当前内容", group: "文件", defaultBinding: "Mod+KeyS" },
  { id: "archive.saveAs", label: "归档另存为", group: "文件", defaultBinding: "Mod+Shift+KeyS" },
  { id: "workspace.close", label: "关闭当前标签或窗口", group: "文件", defaultBinding: "Mod+KeyW" },
  { id: "editor.closeOthers", label: "关闭其他标签页", group: "文件", defaultBinding: "Mod+Shift+KeyW" },
  { id: "editor.closeAll", label: "关闭全部标签页", group: "文件", defaultBinding: "Mod+Alt+KeyW" },
  { id: "editor.splitColumns", label: "左右分屏", group: "视图", defaultBinding: "Mod+Backslash" },
  { id: "editor.splitRows", label: "上下分屏", group: "视图", defaultBinding: "Mod+Shift+Backslash" },
  { id: "workspace.execute", label: "运行脚本或提交版本", group: "工作区", defaultBinding: "Mod+Enter" },
  { id: "search.advanced", label: "打开高级搜索", group: "工作区", defaultBinding: "Mod+Shift+KeyF" },
  { id: "version.open", label: "打开版本面板", group: "工作区", defaultBinding: "" },
  { id: "settings.open", label: "打开偏好设置", group: "工作区", defaultBinding: "Mod+Comma" },
  { id: "workspace.archive", label: "切换到归档编辑", group: "工作区", defaultBinding: "" },
  { id: "workspace.script", label: "切换到脚本工作区", group: "工作区", defaultBinding: "" },
] as const;

export type ShortcutCommandId = (typeof shortcutCommands)[number]["id"];
export type ShortcutOverrides = Record<string, string>;

const defaultBindings: Record<ShortcutCommandId, string> = Object.fromEntries(
  shortcutCommands.map((command) => [command.id, command.defaultBinding]),
) as Record<ShortcutCommandId, string>;

const modifierOrder = ["Mod", "Ctrl", "Meta", "Alt", "Shift"] as const;
const codeLabels: Record<string, string> = {
  Backslash: "\\", Slash: "/", BracketLeft: "[", BracketRight: "]",
  Minus: "-", Equal: "=", Comma: ",", Period: ".", Semicolon: ";",
  Quote: "'", Backquote: "`", Space: "Space",
};
const otherCodes = new Set([
  "Enter", "Backslash", "Slash", "BracketLeft", "BracketRight", "Minus", "Equal",
  "Comma", "Period", "Semicolon", "Quote", "Backquote", "ArrowUp", "ArrowDown",
  "ArrowLeft", "ArrowRight", "Home", "End", "PageUp", "PageDown", "Insert",
  "Delete", "Backspace", "Space", "Tab", "Escape",
]);

export function isMacPlatform(): boolean {
  if (typeof navigator === "undefined") return false;
  return /Macintosh|Mac OS X|MacIntel/i.test(`${navigator.platform} ${navigator.userAgent}`);
}

export function isValidShortcutCode(code: string): boolean {
  return /^Key[A-Z]$/.test(code) || /^Digit[0-9]$/.test(code) ||
    /^F(?:[1-9]|1[0-2])$/.test(code) || otherCodes.has(code);
}

export function isValidShortcutBinding(binding: string): boolean {
  const parts = binding.split("+");
  const code = parts.pop() ?? "";
  if (!isValidShortcutCode(code) || code === "Tab" || code === "Escape" ||
    (!/^F(?:[1-9]|1[0-2])$/.test(code) && !parts.some((part) => part !== "Shift"))) {
    return false;
  }
  return parts.every((part, index) => modifierOrder.indexOf(part as typeof modifierOrder[number]) >
    (index === 0 ? -1 : modifierOrder.indexOf(parts[index - 1] as typeof modifierOrder[number])));
}

export function bindingFromEvent(event: Pick<KeyboardEvent, "code" | "metaKey" | "ctrlKey" | "altKey" | "shiftKey">, isMac = isMacPlatform()): string | null {
  if (!isValidShortcutCode(event.code)) return null;
  const modifiers: string[] = [];
  if (isMac ? event.metaKey : event.ctrlKey) modifiers.push("Mod");
  if (isMac && event.ctrlKey) modifiers.push("Ctrl");
  if (!isMac && event.metaKey) modifiers.push("Meta");
  if (event.altKey) modifiers.push("Alt");
  if (event.shiftKey) modifiers.push("Shift");
  const binding = [...modifiers, event.code].join("+");
  return isValidShortcutBinding(binding) ? binding : null;
}

export function effectiveBinding(command: ShortcutCommandId, overrides: ShortcutOverrides): string {
  return Object.prototype.hasOwnProperty.call(overrides, command) ? overrides[command] : defaultBindings[command];
}

export function commandForBinding(binding: string, overrides: ShortcutOverrides): ShortcutCommandId | null {
  return shortcutCommands.find((command) => binding && effectiveBinding(command.id, overrides) === binding)?.id ?? null;
}

export function assignBinding(command: ShortcutCommandId, binding: string, overrides: ShortcutOverrides): {
  overrides: ShortcutOverrides;
  displaced: ShortcutCommandId | null;
} {
  const next = { ...overrides };
  const displaced = binding ? commandForBinding(binding, next) : null;
  if (displaced && displaced !== command) next[displaced] = "";
  if (binding === defaultBindings[command]) delete next[command];
  else next[command] = binding;
  return { overrides: next, displaced: displaced === command ? null : displaced };
}

export function formatBinding(binding: string, isMac = isMacPlatform()): string {
  if (!binding) return "未设置";
  return binding.split("+").map((part) => {
    if (part === "Mod") return isMac ? "⌘" : "Ctrl";
    if (part === "Ctrl") return "Ctrl";
    if (part === "Meta") return isMac ? "⌘" : "Win";
    if (part === "Alt") return isMac ? "⌥" : "Alt";
    if (part === "Shift") return isMac ? "⇧" : "Shift";
    if (part.startsWith("Key")) return part.slice(3);
    if (part.startsWith("Digit")) return part.slice(5);
    return codeLabels[part] ?? part;
  }).join(isMac ? " " : "+");
}

// Native menu and OS shortcuts cannot be reliably intercepted by the webview.
const nativeReserved = new Set([
  "Mod+KeyQ", "Mod+KeyH", "Mod+KeyM", "Mod+Space", "Alt+Space", "Alt+F4",
  "Mod+KeyA", "Mod+KeyC", "Mod+KeyV", "Mod+KeyX", "Mod+KeyZ",
  "Mod+Shift+KeyZ", "Mod+KeyY", "Mod+KeyF", "Mod+KeyR",
]);

function codeMirrorBindingToCanonical(key: string): string | null {
  const parts = key.split("-");
  const rawCode = parts.pop() ?? "";
  const code = rawCode.length === 1 && /[a-z]/i.test(rawCode)
    ? `Key${rawCode.toUpperCase()}`
    : rawCode.length === 1 && /[0-9]/.test(rawCode)
      ? `Digit${rawCode}`
      : ({ "\\": "Backslash", "/": "Slash", "[": "BracketLeft", "]": "BracketRight" } as Record<string, string>)[rawCode] ?? rawCode;
  const modifiers = parts.map((part) => part === "Cmd" ? "Meta" : part === "Ctrl" ? "Ctrl" : part);
  modifiers.sort((a, b) => modifierOrder.indexOf(a as typeof modifierOrder[number]) - modifierOrder.indexOf(b as typeof modifierOrder[number]));
  const binding = [...modifiers, code].join("+");
  return isValidShortcutBinding(binding) ? binding : null;
}

const editorReserved = new Set<string>();
function reserveEditorBinding(binding: string): void {
  editorReserved.add(binding);
  editorReserved.add(binding.replace(/^Ctrl\+/, "Mod+").replace(/^Meta\+/, "Mod+"));
}
for (const entry of [...defaultKeymap, ...historyKeymap, ...searchKeymap, ...foldKeymap]) {
  for (const key of [entry.key, entry.mac, entry.win, entry.linux]) {
    if (!key) continue;
    const canonical = codeMirrorBindingToCanonical(key);
    if (canonical) {
      reserveEditorBinding(canonical);
      if (entry.shift && !canonical.includes("+Shift+")) {
        const parts = canonical.split("+");
        const code = parts.pop();
        if (code) reserveEditorBinding([...parts, "Shift", code].join("+"));
      }
    }
  }
}

export function reservedBindingReason(binding: string): string | null {
  if (!isValidShortcutBinding(binding)) return "需要修饰键或 F1–F12 功能键";
  if (nativeReserved.has(binding)) return "系统或原生编辑菜单已占用此组合";
  if (editorReserved.has(binding)) return "代码编辑器已占用此组合";
  return null;
}

export function dispatchShortcut(
  event: KeyboardEvent,
  overrides: ShortcutOverrides,
  overlayOpen: boolean,
  execute: (command: ShortcutCommandId) => void,
  allowInOverlay?: (command: ShortcutCommandId) => boolean,
  isAvailable?: (command: ShortcutCommandId) => boolean,
  isMac = isMacPlatform(),
): ShortcutCommandId | null {
  const binding = bindingFromEvent(event, isMac);
  if (!binding) return null;
  const command = commandForBinding(binding, overrides);
  if (!command || event.defaultPrevented || event.isComposing || event.repeat) return null;
  if (overlayOpen && !allowInOverlay?.(command)) return null;
  if (isAvailable && !isAvailable(command)) return null;
  event.preventDefault();
  execute(command);
  return command;
}
