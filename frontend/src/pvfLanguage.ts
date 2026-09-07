import {
  HighlightStyle,
  StreamLanguage,
  StringStream,
  syntaxHighlighting,
} from "@codemirror/language";
import { tags } from "@lezer/highlight";

type PvfMode = "normal" | "string" | "section";

interface PvfState {
  mode: PvfMode;
}

const numberPattern = /^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?$/;

function consumeString(stream: StringStream, state: PvfState): string {
  while (!stream.eol()) {
    const ch = stream.next();
    if (ch !== "`") continue;

    // PVF escapes a literal backtick by doubling it.
    if (stream.peek() === "`") {
      stream.next();
      continue;
    }
    state.mode = "normal";
    break;
  }
  return "string";
}

function consumeSection(stream: StringStream, state: PvfState): string {
  while (!stream.eol()) {
    if (stream.next() === "]") {
      state.mode = "normal";
      break;
    }
  }
  return "heading";
}

function isTokenBoundary(ch: string): boolean {
  return /\s/.test(ch) || "`[]{}=,;".includes(ch);
}

export const pvfLanguage = StreamLanguage.define<PvfState>({
  name: "pvf",
  startState: () => ({ mode: "normal" }),
  token(stream, state) {
    if (state.mode === "string") return consumeString(stream, state);
    if (state.mode === "section") return consumeSection(stream, state);

    if (stream.eatSpace()) return null;

    if (stream.eat("#")) {
      // Comments are intentionally unstyled, but their contents must not be
      // mistaken for numbers or other tokens.
      stream.skipToEnd();
      return null;
    }

    if (stream.eat("`")) {
      state.mode = "string";
      return consumeString(stream, state);
    }

    if (stream.eat("[")) {
      state.mode = "section";
      return consumeSection(stream, state);
    }

    const start = stream.pos;
    stream.eatWhile((ch) => !isTokenBoundary(ch));
    if (stream.pos === start) {
      stream.next();
      return null;
    }

    return numberPattern.test(stream.current()) ? "number" : null;
  },
});

const pvfHighlightStyle = HighlightStyle.define([
  { tag: tags.number, color: "#d19a66" },
  { tag: tags.string, color: "#98c379" },
  { tag: tags.heading, color: "#61afef", fontWeight: "600" },
]);

export const pvfHighlighting = syntaxHighlighting(pvfHighlightStyle);
