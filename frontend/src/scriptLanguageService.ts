import type {
  Completion,
  CompletionContext,
  CompletionResult,
} from "@codemirror/autocomplete";
import scriptApiDeclaration from "./script-api.d.ts?raw";

const scriptFileName = "/pvfine/active-script.pvf.js";
const declarationFileName = "/pvfine/script-api.d.ts";
const runtimeDeclarationFileName = "/pvfine/script-runtime.d.ts";

// The language service intentionally does not load TypeScript's bundled lib.d.ts:
// the script editor must only see the in-memory PVF API. Keep the small part of
// the standard type surface needed for the documented `for...of pvf.glob(...)`
// pattern available in memory, otherwise TypeScript widens the loop variable to
// `any` and loses the PVFFile -> PVFDocument relationship.
const runtimeDeclaration = `
interface IteratorResult<T> {
\tdone: boolean;
\tvalue: T;
}

interface Iterator<T> {
\tnext(...args: any[]): IteratorResult<T>;
}

interface Iterable<T> {
\t[Symbol.iterator](): Iterator<T>;
}

interface IterableIterator<T> extends Iterator<T>, Iterable<T> {}

interface Array<T> {
\treadonly length: number;
\t[n: number]: T;
\t[Symbol.iterator](): IterableIterator<T>;
}

interface ReadonlyArray<T> {
\treadonly length: number;
\t[n: number]: T;
\t[Symbol.iterator](): IterableIterator<T>;
}

interface SymbolConstructor {
\treadonly iterator: unique symbol;
}

declare const Symbol: SymbolConstructor;
`;

type TypeScriptModule = typeof import("typescript");

interface VirtualFile {
  text: string;
  version: number;
}

interface CompletionRequest {
  source: string;
  position: number;
}

/**
 * A small in-memory TypeScript Language Service host for the script editor.
 * It never reads from disk: the active JS source and the two in-memory
 * declaration files are the only files visible to TypeScript.
 */
class ScriptDeclarationLanguageService {
  private readonly files = new Map<string, VirtualFile>([
    [scriptFileName, { text: "", version: 0 }],
    [declarationFileName, { text: scriptApiDeclaration, version: 1 }],
    [runtimeDeclarationFileName, { text: runtimeDeclaration, version: 1 }],
  ]);

  private typescript: TypeScriptModule | null = null;
  private service: import("typescript").LanguageService | null = null;
  private loading: Promise<void> | null = null;

  async complete(request: CompletionRequest): Promise<CompletionResult | null> {
    try {
      await this.ensureReady();
    } catch {
      // The editor still has its lightweight fallback completion source when
      // the optional TypeScript chunk cannot be loaded.
      return null;
    }
    if (!this.service || !this.typescript) return null;

    this.updateScript(request.source);
    const info = this.service.getCompletionsAtPosition(
      scriptFileName,
      request.position,
      {
        includeCompletionsForModuleExports: false,
        includeInsertTextCompletions: true,
        triggerKind: this.typescript.CompletionTriggerKind.Invoked,
      },
    );
    if (!info || info.entries.length === 0) return null;

    const replacementSpan = info.optionalReplacementSpan;
    const entries = info.entries.filter((entry) => entry.kind !== "warning");
    const options: Completion[] = entries
      .map((entry) => ({
        label: entry.name,
        type: completionType(entry.kind),
        detail: entry.kindModifiers || entry.kind,
        apply: entry.insertText || entry.name,
      }));
    if (options.length === 0) return null;
    const fallbackSpan = identifierSpan(request.source, request.position);
    return {
      from: replacementSpan?.start ?? fallbackSpan.from,
      to: replacementSpan?.start !== undefined && replacementSpan?.length !== undefined
        ? replacementSpan.start + replacementSpan.length
        : fallbackSpan.to,
      options: options.map((option, index) => {
        const entry = entries[index];
        const details = this.service!.getCompletionEntryDetails(
          scriptFileName,
          request.position,
          entry.name,
          undefined,
          entry.source,
          undefined,
          entry.data,
        );
        if (!details) return option;
        const detail = this.typescript!.displayPartsToString(details.displayParts);
        const documentation = this.typescript!.displayPartsToString(details.documentation);
        return {
          ...option,
          detail: detail || option.detail,
          info: documentation || undefined,
        };
      }),
      validFor: /[\w$.-]*/,
    };
  }

  private async ensureReady(): Promise<void> {
    if (this.service) return;
    if (!this.loading) {
      this.loading = import("typescript").then((typescript) => {
        this.typescript = typescript;
        this.service = typescript.createLanguageService(this.createHost(typescript));
      });
    }
    await this.loading;
  }

  private updateScript(source: string): void {
    const file = this.files.get(scriptFileName)!;
    if (file.text === source) return;
    file.text = source;
    file.version++;
  }

  private createHost(typescript: TypeScriptModule): import("typescript").LanguageServiceHost {
    return {
      getCompilationSettings: () => ({
        allowJs: true,
        checkJs: false,
        noEmit: true,
        noLib: true,
        target: typescript.ScriptTarget.ESNext,
        module: typescript.ModuleKind.ESNext,
        strict: false,
      }),
      getScriptFileNames: () => [
        scriptFileName,
        declarationFileName,
        runtimeDeclarationFileName,
      ],
      getScriptVersion: (fileName) => String(this.files.get(fileName)?.version ?? 0),
      getScriptSnapshot: (fileName) => {
        const file = this.files.get(fileName);
        return file ? typescript.ScriptSnapshot.fromString(file.text) : undefined;
      },
      getCurrentDirectory: () => "/pvfine",
      getDefaultLibFileName: () => "lib.d.ts",
      fileExists: (fileName) => this.files.has(fileName),
      readFile: (fileName) => this.files.get(fileName)?.text,
      readDirectory: () => [],
      getNewLine: () => "\n",
      useCaseSensitiveFileNames: () => true,
      getScriptKind: (fileName) =>
        fileName.endsWith(".d.ts") ? typescript.ScriptKind.TS : typescript.ScriptKind.JS,
    };
  }
}

const languageService = new ScriptDeclarationLanguageService();

/** CodeMirror completion source backed by the virtual declaration file. */
export function scriptCompletionSource(
  context: CompletionContext,
): CompletionResult | Promise<CompletionResult | null> | null {
  const word = context.matchBefore(/[\w$]*/);
  const previous = context.state.sliceDoc(Math.max(0, context.pos - 1), context.pos);
  if (!context.explicit && (!word || word.from === word.to) && previous !== ".") return null;
  return languageService.complete({
    source: context.state.doc.toString(),
    position: context.pos,
  });
}

function completionType(kind: string): Completion["type"] {
  switch (kind) {
    case "method":
    case "function":
      return "function";
    case "property":
    case "memberVariable":
      return "property";
    case "class":
    case "interface":
      return "type";
    case "keyword":
      return "keyword";
    default:
      return "variable";
  }
}

function identifierSpan(source: string, position: number): { from: number; to: number } {
  let from = Math.max(0, Math.min(source.length, position));
  while (from > 0 && /[\w$]/.test(source[from - 1] ?? "")) from--;
  return { from, to: Math.max(from, Math.min(source.length, position)) };
}
