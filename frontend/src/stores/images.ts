import { defineStore } from "pinia";
import { ref } from "vue";
import { Events } from "@wailsio/runtime";
import { ImageService } from "../../bindings/pvfine/services";
import type {
  ImageData,
  ImageIndexStatus,
  ImageReference,
} from "../../bindings/pvfine/services/models";

const idleStatus: ImageIndexStatus = {
  state: "idle",
  stage: "idle",
  directory: "",
  done: 0,
  total: 0,
  npkFiles: 0,
  imgFiles: 0,
  imageCount: 0,
  skipped: 0,
  duplicates: 0,
  error: "",
  generation: 0,
};

export const useImageStore = defineStore("images", () => {
  const status = ref<ImageIndexStatus>({ ...idleStatus });
  const revision = ref(0);
  const pending = new Map<string, Promise<ImageData | null>>();
  const resolved = new Map<string, ImageData | null>();
  let initialized = false;

  function eventStatus(event: any): ImageIndexStatus | null {
    const value = event?.data ?? event;
    if (!value || typeof value !== "object" || typeof value.state !== "string") return null;
    return value as ImageIndexStatus;
  }

  function acceptStatus(next: ImageIndexStatus | null): void {
    if (!next) return;
    if (next.generation !== status.value.generation) {
      pending.clear();
      resolved.clear();
      revision.value++;
    }
    status.value = next;
    if (next.state === "ready" || next.state === "error") {
      revision.value++;
      pending.clear();
      resolved.clear();
    }
  }

  async function initialize(): Promise<ImageIndexStatus> {
    if (initialized) return status.value;
    initialized = true;
    try {
      acceptStatus(await ImageService.Initialize());
    } catch (error) {
      initialized = false;
      console.error("initialize image index failed", error);
    }
    return status.value;
  }

  async function selectDirectory(): Promise<ImageIndexStatus> {
    const next = await ImageService.SelectNPKDirectory();
    acceptStatus(next);
    return status.value;
  }

  async function rebuild(): Promise<ImageIndexStatus> {
    const next = await ImageService.RebuildIndex();
    acceptStatus(next);
    return status.value;
  }

  function loadImage(reference: ImageReference | null | undefined): Promise<ImageData | null> {
    if (!reference || !reference.path || reference.index < 0) return Promise.resolve(null);
    const key = `${status.value.generation}\u0000${normalizePath(reference.path)}\u0000${reference.index}`;
    if (resolved.has(key)) return Promise.resolve(resolved.get(key) ?? null);
    const existing = pending.get(key);
    if (existing) return existing;
    const generation = status.value.generation;
    const request = ImageService.GetImage(reference.path, reference.index)
      .then((data) => {
        const result = data ?? null;
        if (status.value.generation === generation) {
          resolved.delete(key);
          resolved.set(key, result);
          while (resolved.size > 512) resolved.delete(resolved.keys().next().value!);
        }
        return result;
      })
      .catch((error) => {
        console.warn("load PVF image failed", reference.path, reference.index, error);
        return null;
      })
      .finally(() => {
        if (pending.get(key) === request) pending.delete(key);
      });
    pending.set(key, request);
    return request;
  }

  Events.On("image:index-progress", (event: any) => acceptStatus(eventStatus(event)));
  Events.On("image:index-ready", (event: any) => acceptStatus(eventStatus(event)));
  Events.On("image:index-error", (event: any) => acceptStatus(eventStatus(event)));

  return {
    status,
    revision,
    initialize,
    selectDirectory,
    rebuild,
    loadImage,
  };
});

function normalizePath(value: string): string {
  return value.replaceAll("\\", "/").replace(/^\/+|\/+$/g, "").toLowerCase();
}
