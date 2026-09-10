<script setup lang="ts">
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
} from "@codemirror/state";
import {
  HighlightStyle,
  indentUnit,
  syntaxHighlighting,
} from "@codemirror/language";
import {
  defaultKeymap,
  history,
  historyKeymap,
  indentWithTab,
} from "@codemirror/commands";
import { searchKeymap, highlightSelectionMatches } from "@codemirror/search";
import { autocompletion, type CompletionContext, type CompletionResult } from "@codemirror/autocomplete";
import { javascript } from "@codemirror/lang-javascript";
import { tags } from "@lezer/highlight";
import { vim } from "@replit/codemirror-vim";
import { pvfHighlighting, pvfLanguage } from "../pvfLanguage";
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
}>();

const host = ref<HTMLDivElement | null>(null);
let view: EditorView | null = null;
const readOnlyComp = new Compartment();
const vimComp = new Compartment();
const editorThemeComp = new Compartment();
const images = useImageStore();

interface AnnotationDisplay {
  annotations: EditorAnnotation[];
  placement: AnnotationTagPlacement;
}

const setAnnotations = StateEffect.define<AnnotationDisplay>();

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
    readonly hideTooltip: () => void
  ) {
    super();
  }

  eq(other: AnnotationWidget): boolean {
    return (
      other.annotation.title === this.annotation.title &&
      other.annotation.content === this.annotation.content &&
      other.annotation.type === this.annotation.type &&
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
    tag.className = inlineImage
      ? "cm-annotation-inline-image"
      : `cm-annotation-tag cm-annotation-tag--${this.annotation.type || "text"}`;
    if (!inlineImage) tag.textContent = this.annotation.title;
    const tooltip = this.annotation.targetFileIndex >= 0
      ? `${this.annotation.content || this.annotation.title}\n\nCmd/Ctrl+单击可以跳转`
      : this.annotation.content;
    if (!this.annotation.image) tag.title = tooltip;
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
    if (this.annotation.image) {
      tag.addEventListener("mouseenter", () => this.showTooltip(this.annotation, tag));
      tag.addEventListener("mouseleave", this.hideTooltip);
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

const imageTooltip = ref<{
  visible: boolean;
  left: number;
  top: number;
  content: string;
  dataUrl: string;
  loading: boolean;
}>({ visible: false, left: 0, top: 0, content: "", dataUrl: "", loading: false });
let imageTooltipRequest = 0;
let activeImageReference: EditorAnnotation["image"] = null;

function showImageTooltip(annotation: EditorAnnotation, element: HTMLElement): void {
  if (!annotation.image) return;
  const rect = element.getBoundingClientRect();
  const width = 280;
  const left = Math.min(Math.max(8, rect.left), Math.max(8, window.innerWidth - width - 8));
  const top = rect.bottom + 8 < window.innerHeight - 80 ? rect.bottom + 8 : Math.max(8, rect.top - 8);
  const request = ++imageTooltipRequest;
  activeImageReference = annotation.image;
  imageTooltip.value = {
    visible: true,
    left,
    top,
    content: [
      annotation.content || `${annotation.image.path}[${annotation.image.index}]`,
      annotation.targetFileIndex >= 0 ? "Cmd/Ctrl+单击可以跳转" : "",
    ].filter(Boolean).join("\n\n"),
    dataUrl: "",
    loading: true,
  };
  void images.loadImage(annotation.image).then((data) => {
    if (request !== imageTooltipRequest) return;
    imageTooltip.value.dataUrl = data?.dataUrl ?? "";
    imageTooltip.value.loading = false;
  });
}

function hideImageTooltip(): void {
  imageTooltipRequest++;
  activeImageReference = null;
  imageTooltip.value.visible = false;
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

    const reference = activeImageReference;
    if (!reference || !imageTooltip.value.visible) return;
    const request = ++imageTooltipRequest;
    imageTooltip.value.dataUrl = "";
    imageTooltip.value.loading = true;
    void images.loadImage(reference).then((data) => {
      if (request !== imageTooltipRequest) return;
      imageTooltip.value.dataUrl = data?.dataUrl ?? "";
      imageTooltip.value.loading = false;
    });
  }
);

function annotationDecorations(
  state: EditorState,
  display: AnnotationDisplay
): DecorationSet {
  const ranges = display.annotations.flatMap((annotation) => {
    const targetStart = Math.max(0, Math.min(state.doc.length, annotation.start));
    const targetEnd = Math.max(targetStart, Math.min(state.doc.length, annotation.end));
    const result: Range<Decoration>[] = [];

    if (annotation.targetFileIndex >= 0 && targetStart < targetEnd) {
      result.push(
        Decoration.mark({ class: "cm-annotation-link" }).range(targetStart, targetEnd)
      );
    }

    if (display.placement !== "hidden" && annotation.title.trim() !== "") {
      const position =
        display.placement === "line-end" ? state.doc.lineAt(targetEnd).to : targetEnd;
      result.push(
        Decoration.widget({
          widget: new AnnotationWidget(
            annotation,
            (fileIndex) => emit("open-reference", fileIndex),
            showImageTooltip,
            hideImageTooltip
          ),
          side: 1,
        }).range(position)
      );
    }
    return result;
  }).sort((a, b) => a.from - b.from);
  return Decoration.set(ranges, true);
}

const annotationDisplayField = StateField.define<AnnotationDisplay>({
  create() {
    return {
      annotations: props.annotations ?? [],
      placement: props.tagPlacement ?? "after-target",
    };
  },
  update(display, transaction) {
    let next = display;
    if (transaction.docChanged) {
      next = {
        ...next,
        annotations: next.annotations.map((annotation) => ({
          ...annotation,
          start: transaction.changes.mapPos(annotation.start, 1),
          end: transaction.changes.mapPos(annotation.end, -1),
        })),
      };
    }
    for (const effect of transaction.effects) {
      if (effect.is(setAnnotations)) next = effect.value;
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
      if (effect.is(setAnnotations)) {
        next = annotationDecorations(transaction.state, effect.value);
      }
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
      indentWithTab,
    ]),
    EditorView.domEventHandlers({
      click(event, currentView) {
        const mouseEvent = event as MouseEvent;
        if (!mouseEvent.metaKey && !mouseEvent.ctrlKey) return false;
        const position = currentView.posAtCoords({
          x: mouseEvent.clientX,
          y: mouseEvent.clientY,
        });
        if (position === null) return false;
        const display = currentView.state.field(annotationDisplayField, false);
        const annotation = display?.annotations.find(
          (item) =>
            item.targetFileIndex >= 0 && item.start <= position && position < item.end
        );
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
    indentUnit.of("\t"),
    isJavaScript ? javascript() : pvfLanguage.extension,
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
      if (u.docChanged) emit("change", u.state.doc.toString());
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

onMounted(() => {
  view = new EditorView({
    state: EditorState.create({ doc: props.doc, extensions: makeExtensions(props.themeId) }),
    parent: host.value!,
  });
});

onBeforeUnmount(() => {
  view?.destroy();
  view = null;
});

// 外部文档切换(标签切换):内容不同才整体替换
watch(
  () => props.doc,
  (doc) => {
    if (!view) return;
    const current = view.state.doc.toString();
    if (doc !== current) {
      view.dispatch({ changes: { from: 0, to: current.length, insert: doc } });
    }
  }
);

watch(
  () => [props.annotations, props.tagPlacement] as const,
  ([annotations, placement]) => {
    view?.dispatch({
      effects: setAnnotations.of({
        annotations: annotations ?? [],
        placement: placement ?? "after-target",
      }),
    });
  },
  { deep: true }
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
  <Teleport to="body">
    <div
      v-if="imageTooltip.visible"
      class="annotation-image-tooltip"
      :style="{ left: `${imageTooltip.left}px`, top: `${imageTooltip.top}px` }"
      role="tooltip"
    >
      <div v-if="imageTooltip.loading" class="annotation-image-tooltip-loading">正在加载图片…</div>
      <img v-if="imageTooltip.dataUrl" :src="imageTooltip.dataUrl" alt="标注图片" />
      <div class="annotation-image-tooltip-text">{{ imageTooltip.content }}</div>
    </div>
  </Teleport>
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
.code-editor :deep(.cm-scroller) {
  flex: 1 1 auto;
  max-height: 100%;
  overflow: auto;
}
:global(.cm-tooltip-autocomplete) {
  z-index: 1000;
}
.annotation-image-tooltip {
  position: fixed;
  z-index: 2000;
  width: 280px;
  padding: 9px;
  color: var(--pvf-text-primary);
  pointer-events: none;
  background: var(--pvf-surface-elevated);
  border: 1px solid var(--pvf-border-subtle);
  border-radius: 6px;
  box-shadow: 0 8px 24px var(--pvf-effect-tooltip-shadow);
}
.annotation-image-tooltip img {
  display: block;
  max-width: 100%;
  max-height: 220px;
  margin: 0 auto 7px;
  object-fit: contain;
}
.annotation-image-tooltip-loading,
.annotation-image-tooltip-text {
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}
.annotation-image-tooltip-loading {
  margin-bottom: 7px;
  color: var(--pvf-text-muted);
}
</style>
