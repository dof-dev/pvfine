<script setup lang="ts">
import { rarityColor } from "../rarity";
import { onBeforeUnmount, onMounted, ref, watch } from "vue";
import {
  EditorView,
  keymap,
  lineNumbers,
  highlightActiveLine,
  highlightActiveLineGutter,
  drawSelection,
  highlightWhitespace,
  rectangularSelection,
  crosshairCursor,
  Decoration,
  tooltips,
  WidgetType,
  type DecorationSet,
} from "@codemirror/view";
import {
  EditorState,
  Compartment,
  StateEffect,
  StateField,
  type Range,
  type RangeSet,
} from "@codemirror/state";
import {
  HighlightStyle,
  foldGutter,
  foldKeymap,
  indentUnit,
  syntaxHighlighting,
} from "@codemirror/language";
import {
  defaultKeymap,
  history,
  historyKeymap,
  indentLess,
  insertTab,
} from "@codemirror/commands";
import { searchKeymap, highlightSelectionMatches } from "@codemirror/search";
import { autocompletion, type CompletionContext, type CompletionResult } from "@codemirror/autocomplete";
import { javascript } from "@codemirror/lang-javascript";
import { tags } from "@lezer/highlight";
import { vim } from "@replit/codemirror-vim";
import { NTooltip } from "naive-ui";
import { annotationAt, indexAnnotations, referenceAt, type AnnotationRange } from "../editorAnnotations";
import { pvfHighlighting, pvfLanguage } from "../pvfLanguage";
import { pvfSectionFolding } from "../pvfSectionFolding";
import type { EditorAnnotation } from "../../bindings/pvfine/services/models";
import type { AnnotationTagPlacement } from "../stores/settings";
import { useImageStore } from "../stores/images";
import { scriptCompletionSource as declarationCompletionSource } from "../scriptLanguageService";
import type { ResolvedThemeId } from "../theme";

const props = defineProps<{
  doc: string;
  language?: "pvf" | "javascript";
  readOnly?: boolean;
  annotations?: EditorAnnotation[];
  tagPlacement?: AnnotationTagPlacement;
  vimMode?: boolean;
  themeId: ResolvedThemeId;
}>();

const emit = defineEmits<{
  (e: "change", text: string): void;
  (e: "open-reference", fileIndex: number): void;
  (e: "edit-placeholder", request: PlaceholderEditRequest): void;
}>();

/** 一次「修改/创建占位符译文」请求:点击标签后由父组件弹框处理。 */
export interface PlaceholderEditRequest {
  tableIndex: number;
  key: string;
  value: string;
  fallback: boolean;
  /** 该键在字符串表里还不存在，需要新建。 */
  missing: boolean;
}

const host = ref<HTMLDivElement | null>(null);
let view: EditorView | null = null;
// 记录最近一次同步文本，避免父组件回传相同值时再次序列化全文。
let syncedDoc = props.doc;
const readOnlyComp = new Compartment();
const vimComp = new Compartment();
const editorThemeComp = new Compartment();
const images = useImageStore();

interface AnnotationDisplay {
  annotations: RangeSet<AnnotationRange>;
  placement: AnnotationTagPlacement;
}

const setAnnotations = StateEffect.define<EditorAnnotation[]>();
const setAnnotationPlacement = StateEffect.define<AnnotationTagPlacement>();
const setDiagnosticLine = StateEffect.define<number | null>();

const javascriptHighlighting = syntaxHighlighting(
  HighlightStyle.define([
    { tag: tags.comment, color: "var(--pvf-text-faint)", fontStyle: "italic" },
    { tag: [tags.string, tags.regexp], color: "var(--pvf-editor-syntax-string)" },
    { tag: [tags.number, tags.bool, tags.atom], color: "var(--pvf-editor-syntax-number)" },
    {
      tag: [tags.keyword, tags.controlKeyword],
      color: "var(--pvf-editor-syntax-heading)",
      fontWeight: "600",
    },
    { tag: tags.operator, color: "var(--pvf-text-secondary)" },
    { tag: tags.variableName, color: "var(--pvf-text-code)" },
    { tag: tags.definition(tags.variableName), color: "var(--pvf-editor-syntax-heading)" },
    { tag: tags.function(tags.variableName), color: "var(--pvf-editor-syntax-heading)" },
    { tag: tags.propertyName, color: "var(--pvf-editor-syntax-string)" },
    { tag: [tags.typeName, tags.className], color: "var(--pvf-editor-syntax-heading)" },
    { tag: [tags.punctuation, tags.bracket], color: "var(--pvf-text-muted)" },
    { tag: tags.invalid, color: "var(--pvf-error)" },
  ]),
);

class AnnotationWidget extends WidgetType {
  constructor(
    readonly annotation: EditorAnnotation,
    readonly openReference: (fileIndex: number) => void,
    readonly showTooltip: (annotation: EditorAnnotation, element: HTMLElement) => void,
    readonly hideTooltip: () => void,
    readonly editPlaceholder: (request: PlaceholderEditRequest) => void
  ) {
    super();
  }

  eq(other: AnnotationWidget): boolean {
    return (
      other.annotation.title === this.annotation.title &&
      other.annotation.content === this.annotation.content &&
      other.annotation.type === this.annotation.type &&
      other.annotation.rarity === this.annotation.rarity &&
      other.annotation.targetFileIndex === this.annotation.targetFileIndex &&
      other.annotation.image?.path === this.annotation.image?.path &&
      other.annotation.image?.index === this.annotation.image?.index &&
      other.annotation.inlineImage === this.annotation.inlineImage
    );
  }

  toDOM(): HTMLElement {
    const tag = document.createElement("span");
    const imageReference = this.annotation.image;
    const inlineImage = !!(imageReference && this.annotation.inlineImage);
    const placeholder = this.annotation.placeholder;
    tag.className = inlineImage
      ? "cm-annotation-inline-image"
      : `cm-annotation-tag cm-annotation-tag--${this.annotation.type || "text"}`;
    if (!inlineImage) {
      tag.textContent = this.annotation.title;
      const color = rarityColor(this.annotation.rarity);
      if (color) tag.style.color = color;
    }
    const hints = [
      this.annotation.targetFileIndex >= 0 ? "Cmd/Ctrl+单击打开目标文件" : "",
      placeholder ? "单击修改译文" : "",
    ].filter(Boolean);
    const tooltip = hintText(this.annotation, hints);
    tag.setAttribute("aria-label", tooltip || this.annotation.title);
    tag.contentEditable = "false";
    if (imageReference && inlineImage) {
      const imageSlot = document.createElement("span");
      imageSlot.className = "cm-annotation-inline-image-slot";
      imageSlot.setAttribute("aria-hidden", "true");
      tag.prepend(imageSlot);
      const entry: InlineImageSlot = {
        reference: imageReference,
        slot: imageSlot,
        request: 0,
        pending: false,
        loaded: false,
        loadedGeneration: -1,
      };
      inlineImageSlots.set(imageSlot, entry);
      queueMicrotask(() => loadInlineImage(entry));
    }
    if (tooltip || this.annotation.image) {
      tag.addEventListener("mouseenter", () => this.showTooltip(this.annotation, tag));
      tag.addEventListener("mouseleave", this.hideTooltip);
    }
    if (placeholder) {
      tag.classList.add("cm-annotation-tag--editable");
      tag.setAttribute("role", "button");
      tag.setAttribute("tabindex", "0");
      tag.addEventListener("click", (event) => {
        if (event.metaKey || event.ctrlKey) return;
        event.preventDefault();
        event.stopPropagation();
        this.editPlaceholder({
          tableIndex: placeholder.tableIndex,
          key: placeholder.key,
          value: this.annotation.title,
          fallback: !!placeholder.fallback,
          missing: !!placeholder.missing,
        });
      });
    }
    if (this.annotation.targetFileIndex >= 0) {
      tag.classList.add("cm-annotation-tag--link");
      tag.setAttribute("role", "button");
      tag.addEventListener("click", (event) => {
        if (!event.metaKey && !event.ctrlKey) return;
        event.preventDefault();
        event.stopPropagation();
        this.openReference(this.annotation.targetFileIndex);
      });
    }
    return tag;
  }

  destroy(dom: HTMLElement): void {
    const imageSlot = dom.querySelector<HTMLElement>(".cm-annotation-inline-image-slot");
    if (imageSlot) inlineImageSlots.delete(imageSlot);
  }

  ignoreEvent(): boolean {
    return true;
  }
}

interface InlineImageSlot {
  reference: NonNullable<EditorAnnotation["image"]>;
  slot: HTMLElement;
  request: number;
  pending: boolean;
  loaded: boolean;
  loadedGeneration: number;
}

/** 标注标签的 tooltip:标注内容 + 可用操作提示。 */
function hintText(annotation: EditorAnnotation, hints: string[]): string {
  return [annotation.content || annotation.title, ...hints].filter(Boolean).join("\n\n");
}

const inlineImageSlots = new Map<HTMLElement, InlineImageSlot>();

function loadInlineImage(entry: InlineImageSlot): void {
  if (!entry.slot.isConnected || entry.pending) return;
  const generation = images.status.generation;
  if (entry.loaded && entry.loadedGeneration === generation) return;

  const request = ++entry.request;
  const revision = images.revision;
  entry.pending = true;
  entry.loaded = false;
  entry.slot.replaceChildren();
  void images.loadImage(entry.reference).then((data) => {
    if (request !== entry.request || !entry.slot.isConnected || images.status.generation !== generation) return;
    if (!data?.dataUrl) return;
    const image = document.createElement("img");
    image.src = data.dataUrl;
    image.alt = "";
    image.width = 16;
    image.height = 16;
    entry.slot.replaceChildren(image);
    entry.loaded = true;
    entry.loadedGeneration = generation;
  }).finally(() => {
    if (request !== entry.request) return;
    entry.pending = false;
    // 如果请求在索引切换/完成前返回空结果，补一次请求，避免正文图片
    // 因为首次加载早于索引完成而永久缺失。
    if (!entry.loaded && entry.slot.isConnected && images.revision !== revision) {
      loadInlineImage(entry);
    }
  });
}

type AnnotationTooltipPlacement = "bottom-start" | "top-start";

const annotationTooltip = ref<{
  visible: boolean;
  left: number;
  top: number;
  placement: AnnotationTooltipPlacement;
  content: string;
  dataUrl: string;
  loading: boolean;
}>({
  visible: false,
  left: 0,
  top: 0,
  placement: "bottom-start",
  content: "",
  dataUrl: "",
  loading: false,
});
let annotationTooltipRequest = 0;
let activeTooltipImageReference: EditorAnnotation["image"] = null;
let tooltipHideTimer: number | undefined;

function clearTooltipHideTimer(): void {
  if (tooltipHideTimer === undefined) return;
  window.clearTimeout(tooltipHideTimer);
  tooltipHideTimer = undefined;
}

function showAnnotationTooltip(annotation: EditorAnnotation, element: HTMLElement): void {
  clearTooltipHideTimer();
  const rect = element.getBoundingClientRect();
  const width = 320;
  const left = Math.min(Math.max(8, rect.left), Math.max(8, window.innerWidth - width - 8));
  const placement: AnnotationTooltipPlacement = rect.bottom + 260 < window.innerHeight
    ? "bottom-start"
    : "top-start";
  const top = placement === "bottom-start" ? rect.bottom : rect.top;
  const request = ++annotationTooltipRequest;
  activeTooltipImageReference = annotation.image;
  annotationTooltip.value = {
    visible: true,
    left,
    top,
    placement,
    content: [
      annotation.content || (annotation.image
        ? `${annotation.image.path}[${annotation.image.index}]`
        : annotation.title),
      annotation.targetFileIndex >= 0 ? "Cmd/Ctrl+单击可以跳转" : "",
    ].filter(Boolean).join("\n\n"),
    dataUrl: "",
    loading: !!annotation.image,
  };

  if (!annotation.image) return;
  void images.loadImage(annotation.image)
    .then((data) => {
      if (request !== annotationTooltipRequest) return;
      annotationTooltip.value.dataUrl = data?.dataUrl ?? "";
      annotationTooltip.value.loading = false;
    })
    .catch(() => {
      if (request !== annotationTooltipRequest) return;
      annotationTooltip.value.loading = false;
    });
}

function hideAnnotationTooltip(): void {
  clearTooltipHideTimer();
  annotationTooltipRequest++;
  activeTooltipImageReference = null;
  annotationTooltip.value.visible = false;
}

function scheduleHideTooltip(): void {
  clearTooltipHideTimer();
  tooltipHideTimer = window.setTimeout(() => {
    tooltipHideTimer = undefined;
    hideAnnotationTooltip();
  }, 180);
}

function cancelTooltipHide(): void {
  clearTooltipHideTimer();
}

function hiddenAnnotationTarget(event: Event, currentView: EditorView): HTMLElement | null {
  if (!(event.target instanceof HTMLElement)) return null;
  const target = event.target.closest<HTMLElement>(".cm-annotation-hover");
  return target && currentView.dom.contains(target) ? target : null;
}

watch(
  () => images.revision,
  () => {
    for (const [slot, entry] of inlineImageSlots) {
      if (!slot.isConnected) {
        inlineImageSlots.delete(slot);
        continue;
      }
      loadInlineImage(entry);
    }

    const reference = activeTooltipImageReference;
    if (!reference || !annotationTooltip.value.visible) return;
    const request = ++annotationTooltipRequest;
    annotationTooltip.value.dataUrl = "";
    annotationTooltip.value.loading = true;
    void images.loadImage(reference).then((data) => {
      if (request !== annotationTooltipRequest) return;
      annotationTooltip.value.dataUrl = data?.dataUrl ?? "";
      annotationTooltip.value.loading = false;
    }).catch(() => {
      if (request !== annotationTooltipRequest) return;
      annotationTooltip.value.loading = false;
    });
  }
);

function annotationDecorations(
  state: EditorState,
  display: AnnotationDisplay
): DecorationSet {
  const ranges: Range<Decoration>[] = [];
  for (const cursor = display.annotations.iter(); cursor.value; cursor.next()) {
    const annotation = cursor.value.annotation;
    const targetStart = cursor.from;
    const targetEnd = cursor.to;

    if (targetStart < targetEnd && (annotation.targetFileIndex >= 0 || display.placement === "hidden")) {
      const classes = [
        annotation.targetFileIndex >= 0 ? "cm-annotation-link" : "",
        display.placement === "hidden" ? "cm-annotation-hover" : "",
      ].filter(Boolean).join(" ");
      ranges.push(
        Decoration.mark({ class: classes }).range(targetStart, targetEnd)
      );
    }

    if (display.placement !== "hidden" && annotation.title.trim() !== "") {
      const position =
        display.placement === "line-end" ? state.doc.lineAt(targetEnd).to : targetEnd;
      ranges.push(
        Decoration.widget({
          widget: new AnnotationWidget(
            annotation,
            (fileIndex) => emit("open-reference", fileIndex),
            showAnnotationTooltip,
            scheduleHideTooltip,
            (request) => emit("edit-placeholder", request)
          ),
          side: 1,
        }).range(position)
      );
    }
  }
  return Decoration.set(ranges, true);
}

const annotationDisplayField = StateField.define<AnnotationDisplay>({
  create(state) {
    return {
      annotations: indexAnnotations(props.annotations ?? [], state.doc.length),
      placement: props.tagPlacement ?? "after-target",
    };
  },
  update(display, transaction) {
    let next = display;
    if (transaction.docChanged) {
      next = {
        ...next,
        annotations: next.annotations.map(transaction.changes),
      };
    }
    for (const effect of transaction.effects) {
      if (effect.is(setAnnotations)) {
        next = { ...next, annotations: indexAnnotations(effect.value, transaction.state.doc.length) };
      }
      if (effect.is(setAnnotationPlacement)) next = { ...next, placement: effect.value };
    }
    return next;
  },
});

const annotationField = StateField.define<DecorationSet>({
  create(state) {
    return annotationDecorations(state, state.field(annotationDisplayField));
  },
  update(decorations, transaction) {
    let next = decorations.map(transaction.changes);
    for (const effect of transaction.effects) {
      if (effect.is(setAnnotations) || effect.is(setAnnotationPlacement)) {
        return annotationDecorations(transaction.state, transaction.state.field(annotationDisplayField));
      }
    }
    return next;
  },
  provide: (field) => EditorView.decorations.from(field),
});

const diagnosticLineField = StateField.define<DecorationSet>({
  create: () => Decoration.none,
  update(decorations, transaction) {
    let next = decorations.map(transaction.changes);
    for (const effect of transaction.effects) {
      if (!effect.is(setDiagnosticLine)) continue;
      if (effect.value === null) {
        next = Decoration.none;
        continue;
      }
      const line = transaction.state.doc.line(effect.value);
      next = Decoration.set([
        Decoration.line({ class: "cm-diagnostic-line" }).range(line.from),
      ]);
    }
    return next;
  },
  provide: (field) => EditorView.decorations.from(field),
});

function createEditorTheme(themeId: ResolvedThemeId) {
  return EditorView.theme(
    {
      "&": {
        height: "100%",
        fontSize: "13px",
        color: "var(--pvf-text-primary)",
        backgroundColor: "transparent",
      },
    ".cm-scroller": {
      fontFamily: "'SF Mono', Menlo, Consolas, 'Courier New', monospace",
      lineHeight: "1.55",
      userSelect: "text",
    },
    ".cm-gutters": {
      backgroundColor: "transparent",
      border: "none",
      color: "var(--pvf-editor-gutter-text)",
    },
    ".cm-content": { padding: "8px 0" },
    ".cm-activeLine": { backgroundColor: "var(--pvf-editor-active-line)" },
    ".cm-activeLineGutter": { backgroundColor: "var(--pvf-editor-active-line)" },
    ".cm-selectionMatch": { backgroundColor: "var(--pvf-editor-selection-match)" },
  },
  { dark: themeId === "dark" }
  );
}

function makeExtensions(themeId: ResolvedThemeId) {
  const isJavaScript = props.language === "javascript";
  return [
    lineNumbers(),
    highlightActiveLineGutter(),
    highlightActiveLine(),
    history(),
    drawSelection(),
    highlightWhitespace(),
    rectangularSelection(),
    crosshairCursor(),
    highlightSelectionMatches(),
    vimComp.of(props.vimMode ? vim() : []),
    keymap.of([
      ...defaultKeymap,
      ...historyKeymap,
      ...searchKeymap,
      ...(!isJavaScript ? foldKeymap : []),
      { key: "Tab", run: insertTab, shift: indentLess },
    ]),
    EditorView.domEventHandlers({
      mouseover(event, currentView) {
        const display = currentView.state.field(annotationDisplayField, false);
        if (display?.placement !== "hidden") return false;
        const target = hiddenAnnotationTarget(event, currentView);
        if (!target) return false;
        const annotation = annotationAt(display.annotations, currentView.posAtDOM(target, 0));
        if (annotation) showAnnotationTooltip(annotation, target);
        return false;
      },
      mouseout(event, currentView) {
        const target = hiddenAnnotationTarget(event, currentView);
        if (target && !(event.relatedTarget instanceof Node && target.contains(event.relatedTarget))) {
          scheduleHideTooltip();
        }
        return false;
      },
      click(event, currentView) {
        const mouseEvent = event as MouseEvent;
        if (!mouseEvent.metaKey && !mouseEvent.ctrlKey) return false;
        const position = currentView.posAtCoords({
          x: mouseEvent.clientX,
          y: mouseEvent.clientY,
        });
        if (position === null) return false;
        const display = currentView.state.field(annotationDisplayField, false);
        const annotation = display && referenceAt(display.annotations, position);
        if (!annotation) return false;
        mouseEvent.preventDefault();
        mouseEvent.stopPropagation();
        emit("open-reference", annotation.targetFileIndex);
        return true;
      },
    }),
    readOnlyComp.of(EditorState.readOnly.of(!!props.readOnly)),
    annotationDisplayField,
    annotationField,
    diagnosticLineField,
    indentUnit.of("\t"),
    isJavaScript ? javascript() : pvfLanguage.extension,
    ...(!isJavaScript ? [foldGutter(), ...pvfSectionFolding] : []),
    isJavaScript
      ? [
          tooltips({ parent: document.body, position: "fixed" }),
          javascriptHighlighting,
          autocompletion({ override: [scriptCompletionSource] }),
        ]
      : pvfHighlighting,
    editorThemeComp.of(createEditorTheme(themeId)),
    EditorView.lineWrapping,
    EditorView.updateListener.of((u) => {
      if (u.docChanged) {
        syncedDoc = u.state.doc.toString();
        emit("change", syncedDoc);
      }
    }),
  ];
}

function scriptCompletionSource(
  context: CompletionContext,
): CompletionResult | Promise<CompletionResult | null> | null {
  const fallback = (): CompletionResult | null => {
    const word = context.matchBefore(/[\w$.-]*/);
    if (!word || (word.from === word.to && !context.explicit)) return null;
    return {
      from: word.from,
      options: [
        { label: "pvf", type: "variable", detail: "PVF 脚本 API" },
        { label: "pvf.files", type: "function", detail: "PVFFile[]" },
        { label: "pvf.find", type: "function", detail: "(path) => PVFFile | null" },
        { label: "pvf.glob", type: "function", detail: "(pattern) => PVFFile[]" },
        { label: "pvf.fileset", type: "function", detail: "(name) => PVFFileSet | null" },
        { label: "pvf.createFileset", type: "function", detail: "(name, paths?) => PVFFileSet" },
        { label: "fileset.getAll", type: "function", detail: "() => string[]" },
        { label: "fileset.setAll", type: "function", detail: "(paths) => number" },
        { label: "pvf.createFile", type: "function", detail: "(path, dataType?, text?) => PVFFile" },
        { label: "pvf.copyFile", type: "function", detail: "(from, to, overwrite?) => PVFFile" },
        { label: "pvf.deleteFile", type: "function", detail: "(path) => boolean" },
        { label: "pvf.lst", type: "function", detail: "(path) => PVFList" },
        { label: "list.get", type: "function", detail: "() => Record<string, string>" },
        { label: "list.forEach", type: "function", detail: "(id, path) => void，按文件顺序" },
        { label: "list.set", type: "function", detail: "(id, path) => void" },
        { label: "list.mset", type: "function", detail: "(entries) => void" },
        { label: "list.unset", type: "function", detail: "(id) => boolean" },
        { label: "list.getId", type: "function", detail: "(path) => string | null" },
        { label: "pvf.log", type: "function", detail: "记录脚本日志" },
        { label: "pvf.progress", type: "function", detail: "更新执行进度" },
        { label: "pvf.modifiedCount", type: "property", detail: "number" },
        { label: "pvf.scannedCount", type: "property", detail: "number" },
        { label: "file.parse", type: "function", detail: "() => PVFDocument" },
        { label: "file.write", type: "function", detail: "(document) => void" },
        { label: "document.section", type: "function", detail: "(path) => PVFSection | null" },
        { label: "document.sections", type: "function", detail: "(path) => PVFSection[]" },
        { label: "document.warnings", type: "function", detail: "() => PVFParseWarning[]" },
        { label: "section.get", type: "function", detail: "(index?) => PVFScalar" },
        { label: "section.set", type: "function", detail: "(value, index?) => void" },
        { label: "section.append", type: "function", detail: "(value) => void" },
      ],
      validFor: /[\w$.-]*/,
    };
  };
  const result = declarationCompletionSource(context);
  if (result && typeof (result as Promise<CompletionResult | null>).then === "function") {
    return (result as Promise<CompletionResult | null>).then((value) => value ?? fallback());
  }
  return result ?? fallback();
}

function revealPosition(lineNumber: number, columnNumber = 1): void {
  if (!view) return;
  const line = Math.max(1, Math.min(view.state.doc.lines, Math.trunc(lineNumber)));
  const column = Math.max(1, Math.trunc(columnNumber));
  const lineInfo = view.state.doc.line(line);
  const position = Math.min(lineInfo.to, lineInfo.from + column - 1);
  view.dispatch({
    selection: { anchor: position },
    effects: [
      setDiagnosticLine.of(line),
      EditorView.scrollIntoView(position, { y: "center" }),
    ],
  });
  view.focus();
}

/** 在光标处插入文本(如字符串表引用),并把光标放到插入内容之后。 */
function insertText(text: string): boolean {
  if (!view) return false;
  const range = view.state.selection.main;
  view.dispatch({
    changes: { from: range.from, to: range.to, insert: text },
    selection: { anchor: range.from + text.length },
  });
  view.focus();
  return true;
}

defineExpose({ revealPosition, insertText });

onMounted(() => {
  view = new EditorView({
    state: EditorState.create({ doc: props.doc, extensions: makeExtensions(props.themeId) }),
    parent: host.value!,
  });
});

onBeforeUnmount(() => {
  hideAnnotationTooltip();
  view?.destroy();
  view = null;
});

// 外部文档切换(标签切换):内容不同才整体替换
watch(
  () => props.doc,
  (doc) => {
    if (!view) return;
    if (doc !== syncedDoc) {
      syncedDoc = doc;
      view.dispatch({
        changes: { from: 0, to: view.state.doc.length, insert: doc },
        effects: setDiagnosticLine.of(null),
      });
    }
  }
);

// 标注整体替换时才重建，禁止深度 watch 大数组。位置显示切换沿用已映射的范围。
watch(
  () => props.annotations,
  (annotations) => view?.dispatch({ effects: setAnnotations.of(annotations ?? []) })
);
watch(
  () => props.tagPlacement,
  (placement) => {
    hideAnnotationTooltip();
    view?.dispatch({ effects: setAnnotationPlacement.of(placement ?? "after-target") });
  }
);

watch(
  () => props.readOnly,
  (ro) => {
    view?.dispatch({
      effects: readOnlyComp.reconfigure(EditorState.readOnly.of(!!ro)),
    });
  }
);

watch(
  () => props.vimMode,
  (enabled) => {
    view?.dispatch({
      effects: vimComp.reconfigure(enabled ? vim() : []),
    });
  }
);

watch(
  () => props.themeId,
  (themeId) => {
    view?.dispatch({
      effects: editorThemeComp.reconfigure(createEditorTheme(themeId)),
    });
  }
);
</script>

<template>
  <div ref="host" class="code-editor" />
  <NTooltip
    :show="annotationTooltip.visible"
    trigger="manual"
    :x="annotationTooltip.left"
    :y="annotationTooltip.top"
    :placement="annotationTooltip.placement"
    to="body"
    :raw="true"
    :show-arrow="false"
    :delay="0"
    :duration="0"
    :keep-alive-on-hover="true"
    :z-index="2000"
    content-class="annotation-tooltip-content"
  >
    <div
      class="annotation-tooltip-inner"
      role="tooltip"
      @mouseenter="cancelTooltipHide"
      @mouseleave="scheduleHideTooltip"
    >
      <div v-if="annotationTooltip.loading" class="annotation-tooltip-loading">正在加载图片…</div>
      <img v-if="annotationTooltip.dataUrl" :src="annotationTooltip.dataUrl" alt="标注图片" />
      <div class="annotation-tooltip-text">{{ annotationTooltip.content }}</div>
    </div>
  </NTooltip>
</template>

<style scoped>
.code-editor {
  flex: 1;
  min-width: 0;
  min-height: 0;
  height: auto;
  overflow: hidden;
}
.code-editor :deep(.cm-editor),
.code-editor :deep(.cm-scroller) {
  min-height: 0;
  height: 100%;
}
.code-editor :deep(.cm-annotation-tag) {
  display: inline-flex;
  align-items: center;
  max-width: 220px;
  height: 18px;
  margin-left: 7px;
  padding: 0 6px;
  overflow: hidden;
  color: var(--pvf-editor-annotation-text);
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  font-size: 11px;
  line-height: 16px;
  text-overflow: ellipsis;
  vertical-align: 1px;
  white-space: nowrap;
  user-select: none;
  background: var(--pvf-editor-annotation-surface);
  border: 1px solid var(--pvf-editor-annotation-border);
  border-radius: 4px;
}
.code-editor :deep(.cm-annotation-tag--enum) {
  color: var(--pvf-editor-annotation-enum-text);
  background: var(--pvf-editor-annotation-enum-surface);
  border-color: var(--pvf-editor-annotation-enum-border);
}
.code-editor :deep(.cm-annotation-tag--reference) {
  color: var(--pvf-editor-annotation-reference-text);
  background: var(--pvf-editor-annotation-reference-surface);
  border-color: var(--pvf-editor-annotation-reference-border);
}
/* 字符串表占位符的译文：文档里仍是占位符，这里只做展示。 */
.code-editor :deep(.cm-annotation-tag--placeholder) {
  font-style: italic;
  border-style: dashed;
}
.code-editor :deep(.cm-annotation-tag--editable) {
  cursor: pointer;
}
/* 表里还没有这个键：提示需要填写，点击即可创建。 */
.code-editor :deep(.cm-annotation-tag--placeholder-missing) {
  color: var(--pvf-error);
  border-style: dashed;
  border-color: var(--pvf-error);
}
.code-editor :deep(.cm-annotation-tag--editable:hover) {
  filter: brightness(1.15);
  text-decoration: underline dotted var(--pvf-editor-annotation-link);
  text-underline-offset: 2px;
}
.code-editor :deep(.cm-annotation-inline-image) {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  margin-left: 7px;
  vertical-align: middle;
  line-height: 1;
  user-select: none;
}
.code-editor :deep(.cm-annotation-inline-image-slot) {
  display: inline-flex;
  flex: 0 0 16px;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
}
.code-editor :deep(.cm-annotation-inline-image-slot img) {
  display: block;
  width: 16px;
  height: 16px;
  object-fit: contain;
}
.code-editor :deep(.cm-annotation-tag--link) {
  cursor: pointer;
}
.code-editor :deep(.cm-annotation-tag--link:hover) {
  filter: brightness(1.15);
}
.code-editor :deep(.cm-annotation-link) {
  cursor: pointer;
  text-decoration: underline dotted var(--pvf-editor-annotation-link);
  text-underline-offset: 2px;
}
.code-editor :deep(.cm-diagnostic-line) {
  background: var(--pvf-error-surface);
  box-shadow: inset 3px 0 0 var(--pvf-error);
}
.code-editor :deep(.cm-scroller) {
  flex: 1 1 auto;
  max-height: 100%;
  overflow: auto;
}
:global(.cm-tooltip-autocomplete) {
  z-index: 1000;
}
:global(.annotation-tooltip-content) {
  max-width: 320px;
  padding: 9px;
  color: var(--pvf-text-primary);
  pointer-events: auto;
  user-select: text;
  background: var(--pvf-surface-elevated);
  border: 1px solid var(--pvf-border-subtle);
  border-radius: 6px;
  box-shadow: 0 8px 24px var(--pvf-effect-tooltip-shadow);
}
.annotation-tooltip-inner {
  max-width: 300px;
  cursor: text;
  user-select: text;
}
.annotation-tooltip-inner img {
  display: block;
  max-width: 100%;
  max-height: 220px;
  margin: 0 auto 7px;
  object-fit: contain;
}
.annotation-tooltip-loading,
.annotation-tooltip-text {
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}
.annotation-tooltip-loading {
  margin-bottom: 7px;
  color: var(--pvf-text-muted);
}
</style>
