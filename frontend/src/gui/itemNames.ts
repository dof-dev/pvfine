import { ref } from 'vue';

/** One read per distinct ID, with stale responses isolated from the visible archive. */
export function createItemNameSession(read: (ids: string[]) => PromiseLike<Record<string, string | undefined> | null>) {
  const names = ref<Record<string, string>>({});
  const loading = ref(false);
  const error = ref('');
  let generation = 0;

  function invalidate(clear = false) {
    generation++;
    loading.value = false;
    error.value = '';
    if (clear) names.value = {};
  }

  async function load(values: string[]) {
    const ids = [...new Set(values.map(value => value.trim()).filter(value => /^\d+$/.test(value)))];
    const request = ++generation;
    names.value = Object.fromEntries(ids.filter(id => Object.hasOwn(names.value, id)).map(id => [id, names.value[id]!]));
    error.value = '';
    loading.value = ids.length > 0;
    if (!ids.length) return;
    try {
      const result = await read(ids);
      if (request !== generation) return;
      names.value = Object.fromEntries(ids.map(id => [id, result?.[id] ?? '']));
    } catch (cause) {
      if (request === generation) error.value = String(cause);
    } finally {
      if (request === generation) loading.value = false;
    }
  }

  return { names, loading, error, load, invalidate };
}
