import { type EditorState, StateField } from "@codemirror/state";
import { foldService } from "@codemirror/language";

interface SectionTag {
  from: number;
  to: number;
  name: string;
  closing: boolean;
}

type FoldRange = { from: number; to: number };

// PVF section tags occupy their own line. This excludes tags inside quoted
// strings, values, and comments from the folding structure.
const sectionLine = /^\s*\[([^\]\r\n]+)\]\s*(?:#.*)?$/;

function sectionFolds(state: EditorState): Map<number, FoldRange> {
  const tags: SectionTag[] = [];
  let from = 0;
  for (const text of state.doc.iterLines()) {
    const match = sectionLine.exec(text);
    if (match && match[1] !== "/") {
      const closing = match[1].startsWith("/");
      tags.push({
        from,
        to: from + text.length,
        name: closing ? match[1].slice(1) : match[1],
        closing,
      });
    }
    from += text.length + 1;
  }

  const pairedNames = new Set(tags.filter((tag) => tag.closing).map((tag) => tag.name));
  const folds = new Map<number, FoldRange>();
  const stack: SectionTag[] = [];
  const unpaired = new Map<number, SectionTag>();
  const close = (section: SectionTag | undefined, to: number) => {
    if (section && to > section.to) folds.set(section.from, { from: section.to, to });
  };

  for (const tag of tags) {
    const depth = stack.length;
    close(unpaired.get(depth), tag.from - 1);
    unpaired.delete(depth);

    if (tag.closing) {
      const match = stack.findLastIndex((section) => section.name === tag.name);
      if (match < 0) continue;
      // A mismatched close also ends any still-open nested paired sections.
      for (let index = stack.length - 1; index > match; index--) {
        close(stack[index], tag.from - 1);
        unpaired.delete(index + 1);
      }
      close(stack[match], tag.to);
      stack.length = match;
      continue;
    }

    if (pairedNames.has(tag.name)) stack.push(tag);
    else unpaired.set(depth, tag);
  }

  for (const section of unpaired.values()) close(section, state.doc.length);
  for (const section of stack) close(section, state.doc.length);
  return folds;
}

const sectionFoldField = StateField.define<Map<number, FoldRange>>({
  create: sectionFolds,
  update(folds, transaction) {
    return transaction.docChanged ? sectionFolds(transaction.state) : folds;
  },
});

export const pvfSectionFolding = [
  sectionFoldField,
  foldService.of((state, lineStart) => state.field(sectionFoldField).get(lineStart) ?? null),
];
