/**
 * JavaScript script workspace API exposed by pvfine at runtime.
 *
 * These declarations are intentionally independent from the generated Wails
 * DTO bindings: they describe the objects available inside a sandboxed
 * `.pvf.js` script.
 */
export type SectionPath = string | readonly string[];
export type PVFScalar = number | string;

export type PVFTokenType =
  | "integer"
  | "float"
  | "string"
  | "quoted"
  | "block5"
  | "block7";

export interface PVFValue {
  readonly type: PVFTokenType;
  readonly value: PVFScalar;
  readonly pool?: "utf8" | "utf16";
}

export type PVFValueInput = PVFScalar | PVFValue;

export type PVFParseWarningCode =
  | "orphan-close"
  | "missing-close"
  | "mismatched-close";

export interface PVFParseWarning {
  readonly code: PVFParseWarningCode;
  readonly message: string;
  /** Zero-based token index; points one past the last token for EOF warnings. */
  readonly tokenIndex: number;
  readonly path: readonly string[];
}

export interface PVFSection {
  readonly name: string;
  readonly path: readonly string[];
  readonly occurrence: number;
  readonly hasEndTag: boolean;

  values(): PVFValue[];
  children(): PVFSection[];

  get(index?: number): PVFScalar | undefined;
  getValue(index?: number): PVFValue | undefined;
  set(value: PVFValueInput, index?: number): void;
  setValues(values: PVFValueInput[]): void;
  append(value: PVFValueInput): void;
  appendSection(
    name: string,
    values?: PVFValueInput[],
    options?: { endTag?: boolean },
  ): PVFSection;
  delete(): void;
}

export interface PVFDocument {
  sections(path: SectionPath): PVFSection[];
  section(path: SectionPath, occurrence?: number): PVFSection | null;
  warnings(): PVFParseWarning[];

  get(
    path: SectionPath,
    occurrence?: number,
    valueIndex?: number,
  ): PVFScalar | undefined;
  getValue(
    path: SectionPath,
    occurrence?: number,
    valueIndex?: number,
  ): PVFValue | undefined;
  set(
    path: SectionPath,
    value: PVFValueInput,
    options?: {
      occurrence?: number;
      valueIndex?: number;
      create?: boolean;
      endTag?: boolean;
    },
  ): boolean;
  delete(path: SectionPath, occurrence?: number): boolean;
  appendSection(
    name: string,
    values?: PVFValueInput[],
    options?: { endTag?: boolean },
  ): PVFSection;
}

export interface PVFFile {
  readonly index: number;
  readonly path: string;
  readonly type: number;
  readonly size: number;

  text(): string;
  setText(text: string): void;
  parse(): PVFDocument;
  write(document: PVFDocument): void;
}

export interface PVFTypes {
  readonly script: number;
  readonly unicode: number;
}

export interface PVFNamespace {
  files(): PVFFile[];
  find(path: string): PVFFile | null;
  glob(pattern: string): PVFFile[];
  log(...values: unknown[]): void;
  progress(done: number, total: number, message?: string): void;
  readonly modifiedCount: number;
  readonly scannedCount: number;
  readonly types: PVFTypes;
}

declare global {
  namespace pvf {
    function files(): PVFFile[];
    function find(path: string): PVFFile | null;
    function glob(pattern: string): PVFFile[];
    function log(...values: unknown[]): void;
    function progress(done: number, total: number, message?: string): void;
    const modifiedCount: number;
    const scannedCount: number;
    const types: PVFTypes;
  }
}
