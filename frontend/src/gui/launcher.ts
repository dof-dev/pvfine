import type { SearchHit, SearchResult } from "../../bindings/pvfine/services/models";
import type { GUIProvider } from "./types";

export interface GUISearchCursor { queryIndex: number; offset: number }

export function guiPathMatches(provider: GUIProvider, path: string): boolean {
  return provider.matches({ index: 0, path, text: "", editable: true });
}

export function newGUIFilePath(provider: GUIProvider, raw: string): string {
  const path = raw.trim().replaceAll("\\", "/").replace(/^\/+|\/+$/g, "");
  if (!path || path.split("/").some(part => !part || part === "." || part === "..")) {
    throw new Error("路径无效：不能为空，且不能包含空段、. 或 ..");
  }
  if (!guiPathMatches(provider, path)) throw new Error(`请选择支持的文件类型或路径：${provider.launcher.fileTypes}`);
  return path;
}

/** 先按类型搜索；关键词搜索过滤后继续翻页，避免无关结果遮住目标文件。 */
export async function searchGUIFiles(
  provider: GUIProvider,
  query: string,
  cursor: GUISearchCursor,
  search: (query: string, cursor: number, limit: number) => Promise<SearchResult | null>,
  isCurrent: () => boolean,
): Promise<{ hits: SearchHit[]; next: GUISearchCursor | null }> {
  const queries = query.trim() ? [query.trim()] : provider.launcher.queries;
  const next = { ...cursor };
  const hits = new Map<number, SearchHit>();
  // 限制单次扫描量，仍可通过“继续搜索”访问剩余结果。
  for (let page = 0; page < 10 && next.queryIndex < queries.length; page++) {
    const result = await search(queries[next.queryIndex]!, next.offset, 100);
    if (!isCurrent()) return { hits: [], next: null };
    for (const hit of result?.hits ?? []) {
      if (hit && guiPathMatches(provider, hit.path)) hits.set(hit.fileIndex, hit);
    }
    if ((result?.nextCursor ?? -1) < 0) { next.queryIndex++; next.offset = 0; }
    else next.offset = result!.nextCursor;
    if (hits.size) break;
  }
  return { hits: [...hits.values()], next: next.queryIndex < queries.length ? next : null };
}
