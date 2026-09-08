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
  WidgetType,
  type DecorationSet,
} from "@codemirror/view";
import { EditorState, Compartment, StateEffect, StateField } from "@codemirror/state";
import {
  defaultKeymap,
  history,
  historyKeymap,
  indentWithTab,
} from "@codemirror/commands";
import { searchKeymap, highlightSelectionMatches } from "@codemirror/search";
import { vim } from "@replit/codemirror-vim";
import { pvfHighlighting, pvfLanguage } from "../pvfLanguage";
import type { EditorAnnotation } from "../../bindings/pvfine/services/models";
import type { AnnotationTagPlacement } from "../stores/settings";

const props = defineProps<{
  doc: string;
  readOnly?: boolean;
  annotations?: EditorAnnotation[];
  tagPlacement?: AnnotationTagPlacement;
  vimMode?: boolean;
}>();

const emit = defineEmits<{
  (e: "change", text: string): void;
  (e: "open-reference", fileIndex: number): void;
}>();

const host = ref<HTMLDivElement | null>(null);
let view: EditorView | null = null;
const readOnlyComp = new Compartment();
const vimComp = new Compartment();

interface AnnotationDisplay {
  annotations: EditorAnnotation[];
  placement: AnnotationTagPlacement;
}

const setAnnotations = StateEffect.define<AnnotationDisplay>();

class AnnotationWidget extends WidgetType {
  constructor(
    readonly annotation: EditorAnnotation,
    readonly openReference: (fileIndex: number) => void
  ) {
    super();
  }

  eq(other: AnnotationWidget): boolean {
    return (
      other.annotation.title === this.annotation.title &&
      other.annotation.content === this.annotation.content &&
      other.annotation.type === this.annotation.type &&
      other.annotation.targetFileIndex === this.annotation.targetFileIndex
    );
  }

  toDOM(): HTMLElement {
    const tag = document.createElement("span");
    tag.className = `cm-annotation-tag cm-annotation-tag--${this.annotation.type || "text"}`;
    tag.textContent = this.annotation.title;
    const tooltip = this.annotation.targetFileIndex >= 0
      ? `${this.annotation.content || this.annotation.title}\n\nCtrl+单击可以跳转`
      : this.annotation.content;
    tag.title = tooltip;
    tag.setAttribute("aria-label", tooltip || this.annotation.title);
    tag.contentEditable = "false";
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

  ignoreEvent(): boolean {
    return true;
  }
}

function annotationDecorations(
  state: EditorState,
  display: AnnotationDisplay
): DecorationSet {
  if (display.placement === "hidden") return Decoration.none;
  const ranges = display.annotations
    .map((annotation) => {
      const targetEnd = Math.max(0, Math.min(state.doc.length, annotation.end));
      const position =
        display.placement === "line-end" ? state.doc.lineAt(targetEnd).to : targetEnd;
      return Decoration.widget({
        widget: new AnnotationWidget(annotation, (fileIndex) =>
          emit("open-reference", fileIndex)
        ),
        side: 1,
      }).range(position);
    })
    .sort((a, b) => a.from - b.from);
  return Decoration.set(ranges, true);
}

const annotationField = StateField.define<DecorationSet>({
  create(state) {
    return annotationDecorations(state, {
      annotations: props.annotations ?? [],
      placement: props.tagPlacement ?? "after-target",
    });
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

// 与暗色界面匹配的极简主题
const darkTheme = EditorView.theme(
  {
    "&": { height: "100%", fontSize: "13px", backgroundColor: "transparent" },
    ".cm-scroller": {
      fontFamily: "'SF Mono', Menlo, Consolas, 'Courier New', monospace",
      lineHeight: "1.55",
      userSelect: "text",
    },
    ".cm-gutters": {
      backgroundColor: "transparent",
      border: "none",
      color: "rgba(128,128,128,0.5)",
    },
    ".cm-content": { padding: "8px 0" },
    ".cm-activeLine": { backgroundColor: "rgba(128,128,128,0.08)" },
    ".cm-activeLineGutter": { backgroundColor: "rgba(128,128,128,0.08)" },
    ".cm-selectionMatch": { backgroundColor: "rgba(80,140,255,0.25)" },
  },
  { dark: true }
);

function makeExtensions() {
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
    keymap.of([...defaultKeymap, ...historyKeymap, ...searchKeymap, indentWithTab]),
    readOnlyComp.of(EditorState.readOnly.of(!!props.readOnly)),
    annotationField,
    pvfLanguage.extension,
    pvfHighlighting,
    darkTheme,
    EditorView.lineWrapping,
    EditorView.updateListener.of((u) => {
      if (u.docChanged) emit("change", u.state.doc.toString());
    }),
  ];
}

onMounted(() => {
  view = new EditorView({
    state: EditorState.create({ doc: props.doc, extensions: makeExtensions() }),
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
</script>

<template>
  <div ref="host" class="code-editor" />
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
  color: #b9d8ff;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  font-size: 11px;
  line-height: 16px;
  text-overflow: ellipsis;
  vertical-align: 1px;
  white-space: nowrap;
  user-select: none;
  background: #263c57;
  border: 1px solid #477db9;
  border-radius: 4px;
}
.code-editor :deep(.cm-annotation-tag--enum) {
  color: #ffdc9e;
  background: #4b3920;
  border-color: #a47a35;
}
.code-editor :deep(.cm-annotation-tag--reference) {
  color: #ace8cc;
  background: #213f34;
  border-color: #438a69;
}
.code-editor :deep(.cm-annotation-tag--link) {
  cursor: pointer;
}
.code-editor :deep(.cm-annotation-tag--link:hover) {
  filter: brightness(1.15);
}
.code-editor :deep(.cm-scroller) {
  flex: 1 1 auto;
  max-height: 100%;
  overflow: auto;
}
</style>
