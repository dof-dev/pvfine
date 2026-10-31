<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import {
  NAlert,
  NButton,
  NCheckbox,
  NInput,
  NInputNumber,
  NModal,
  NSelect,
  NTag,
  useDialog,
  useMessage,
} from "naive-ui";
import { RuleService } from "../../bindings/pvfine/services";
import { useRuleEditorStore } from "../stores/ruleEditor";
import { useEditorStore } from "../stores/editor";
import { useExplorerStore } from "../stores/explorer";

const store = useRuleEditorStore();
const editor = useEditorStore();
const explorer = useExplorerStore();
const message = useMessage();
const dialog = useDialog();
interface Entry {
  id: string;
  kind: string;
  json: string;
  source: string;
  overridden: boolean;
  disabled: boolean;
  effective: boolean;
  dependencies: string[] | null;
}
const entries = ref<Entry[]>([]);
const revision = ref("");
const development = ref(false);
const relations = ref<string[]>([]);
const showDisabled = ref(false);
const selected = ref<Entry | null>(null);
const draft = ref<Record<string, any>>({});
const kind = ref("rule");
const raw = ref("");
const advanced = ref(false);
const busy = ref(false);
const error = ref("");
const dirty = ref(false);
const loaded = ref(false);
const idLocked = ref(false);
const originalID = ref("");
const title = computed(() =>
  store.target?.category === "rendering" ? "渲染规则" : "标注规则",
);
const visibleEntries = computed(() =>
  entries.value.filter((e) => showDisabled.value || !e.disabled),
);
const isAnnotation = computed(() => store.target?.category === "annotations");
const scope = ref("extension");
const scopeOptions = [
  { label: "当前文件 / 目录自身", value: "file" },
  { label: "同后缀", value: "extension" },
  { label: "目录下文件", value: "directory" },
  { label: "全局", value: "global" },
  { label: "自定义", value: "custom" },
];
const typeOptions = computed(() =>
  (draft.value.target?.kind === "token"
    ? ["text", "enum", "reference", "path", "image"]
    : ["text"]
  ).map((value) => ({ label: value, value })),
);
const targetOptions = computed(() =>
  (isAnnotation.value ? ["path", "section", "token"] : ["file", "section"]).map(
    (value) => ({ label: value, value }),
  ),
);

interface FormField {
  path: string;
  label: string;
  type?: "number" | "boolean" | "json" | "list";
}
const formFields = computed<FormField[]>(() => {
  const fields: FormField[] = [
    { path: "match.glob", label: "路径 glob" },
    { path: "match.extensions", label: "后缀列表", type: "list" },
  ];
  if (
    draft.value.target?.kind === "section" ||
    draft.value.target?.kind === "token"
  )
    fields.push({ path: "target.section", label: "Section 名称" });
  if (kind.value === "rule")
    fields.unshift({ path: "description", label: "规则说明" });
  if (isAnnotation.value) {
    fields.push({
      path: "pvfVersions",
      label: "PVF 版本（90US / 90CN / 110US / 115US / 115CN / 100CN / TW）",
      type: "list",
    });
    if (kind.value === "rule")
      fields.push(
        { path: "group", label: "分类" },
        { path: "field", label: "引用共享字段 ID" },
      );
    if (draft.value.target?.kind === "token") {
      fields.push(
        ...[
          "index",
          "offset",
          "recordTokens",
          "groupOffset",
          "tokensPerLineIndex",
          "contextIndex",
          "imagePathToken",
        ].map((path) => ({
          path: `target.${path}`,
          label: path,
          type: "number" as const,
        })),
        {
          path: "target.range",
          label: "范围 {start, endExclusive}（与 index 二选一）",
          type: "json",
        },
        { path: "target.standaloneValues", label: "整数分隔值", type: "json" },
      );
    }
    fields.push(
      { path: "annotation.title", label: "标注标题" },
      { path: "annotation.content", label: "说明内容" },
    );
    if (draft.value.annotation?.type === "enum")
      fields.push({
        path: "annotation.values",
        label: "枚举映射 JSON",
        type: "json",
      });
    if (draft.value.annotation?.type === "reference")
      fields.push({ path: "annotation.relation", label: "关联列表" });
    if (draft.value.annotation?.type === "path")
      fields.push({ path: "annotation.pathRoot", label: "关联路径根目录" });
    if (draft.value.annotation?.type === "image")
      fields.push({
        path: "annotation.inlineImage",
        label: "内联显示图片",
        type: "boolean",
      });
    if (kind.value === "field")
      fields.push({
        path: "preview",
        label:
          "预览配置 JSON（provider/providers、role、group、format、order、label）",
        type: "json",
      });
  } else
    fields.push(
      ...(draft.value.target?.kind === "section"
        ? ["tokensPerLine", "offset", "tokensPerLineIndex"]
        : ["tokensPerLine", "offset"]
      ).map((path) => ({
        path: `format.${path}`,
        label: path,
        type: "number" as const,
      })),
      {
        path: "format.standaloneValues",
        label: "独占一行的整数值",
        type: "json",
      },
      {
        path: "format.tokensPerLineByVersion",
        label: "按 PVF 版本设置每行数量 JSON",
        type: "json",
      },
      {
        path: "format.nestedSections",
        label: "嵌套 Section 列表",
        type: "list",
      },
    );
  return fields;
});
function get(path: string): any {
  return path.split(".").reduce((value, key) => value?.[key], draft.value);
}
function set(path: string, value: any) {
  const keys = path.split(".");
  let object = draft.value;
  for (const key of keys.slice(0, -1)) object = object[key] ??= {};
  const key = keys[keys.length - 1]!;
  if (value === null || value === undefined || value === "") delete object[key];
  else object[key] = value;
  dirty.value = true;
  raw.value = JSON.stringify(draft.value, null, 2);
}
function fieldText(field: FormField): string {
  const value = get(field.path);
  if (value === undefined) return "";
  return field.type === "json"
    ? JSON.stringify(value)
    : field.type === "list"
      ? value.join(", ")
      : String(value);
}
const fieldInputs = ref<Record<string, string>>({});
function syncInputs() {
  fieldInputs.value = Object.fromEntries(
    formFields.value.map((field) => [field.path, fieldText(field)]),
  );
}
function textInput(field: FormField, value: string) {
  fieldInputs.value[field.path] = value;
  if (field.path.startsWith("match.")) scope.value = "custom";
  if (field.type === "json") {
    try {
      set(field.path, value.trim() ? JSON.parse(value) : undefined);
      if (field.path === "target.range" && value.trim()) {
        delete draft.value.target.index;
        raw.value = JSON.stringify(draft.value, null, 2);
      }
      error.value = "";
    } catch {
      error.value = `${field.label} 必须是有效 JSON`;
      dirty.value = true;
    }
  } else
    set(
      field.path,
      field.type === "list"
        ? value
            .split(",")
            .map((v) => v.trim())
            .filter(Boolean)
        : value,
    );
}
function changeTarget(value: string) {
  const section = draft.value.target?.section || store.target?.section || "";
  draft.value.target = {
    kind: value,
    ...(value === "section" || value === "token" ? { section } : {}),
    ...(value === "token" ? { index: 0 } : {}),
  };
  if (isAnnotation.value && value !== "token") set("annotation.type", "text");
  if (!isAnnotation.value && value === "file") {
    delete draft.value.format.tokensPerLineIndex;
    delete draft.value.format.nestedSections;
  }
  dirty.value = true;
  raw.value = JSON.stringify(draft.value, null, 2);
  syncInputs();
}
function changeAnnotationType(value: string) {
  for (const key of ["values", "relation", "pathRoot", "inlineImage"])
    delete draft.value.annotation[key];
  if (value !== "image") delete draft.value.target.imagePathToken;
  if (value === "image") delete draft.value.target.contextIndex;
  set("annotation.type", value);
  syncInputs();
}
function chooseScope(value: string) {
  scope.value = value;
  const target = store.target;
  if (!target || value === "custom") return;
  const extension = target.path
    .split("/")
    .pop()
    ?.match(/\.[^.]+$/)?.[0];
  draft.value.match = {};
  if (value === "file") draft.value.match.glob = target.path;
  if (value === "extension") {
    if (extension && !target.directory)
      draft.value.match.extensions = [extension];
    else draft.value.match.glob = target.path;
  }
  if (value === "directory") {
    const directory = target.directory
      ? target.path
      : target.path.split("/").slice(0, -1).join("/");
    draft.value.match.glob = directory ? `${directory}/**` : "**";
  }
  dirty.value = true;
  raw.value = JSON.stringify(draft.value, null, 2);
  syncInputs();
}
function install(entry: Entry | null, nextKind = "rule") {
  selected.value = entry;
  kind.value = entry?.kind ?? nextKind;
  idLocked.value = !!entry;
  originalID.value = entry?.id ?? "";
  if (entry) {
    draft.value = JSON.parse(entry.json);
    scope.value = "custom";
  } else {
    const target = store.target!;
    draft.value = {
      id: `user.${kind.value}.${crypto.randomUUID()}`,
      match: {},
      target: {
        kind: isAnnotation.value
          ? target.section
            ? "section"
            : "path"
          : target.section
            ? "section"
            : "file",
        ...(target.section ? { section: target.section } : {}),
      },
      ...(isAnnotation.value
        ? { annotation: { title: "", type: "text" } }
        : { format: { tokensPerLine: 1 } }),
    };
    const defaultScope = target.directory
      ? isAnnotation.value
        ? "file"
        : "directory"
      : target.section
        ? "extension"
        : "file";
    chooseScope(defaultScope);
  }
  raw.value = JSON.stringify(draft.value, null, 2);
  error.value = "";
  dirty.value = false;
  advanced.value = false;
  syncInputs();
}
function selectEntry(entry: Entry | null, nextKind = "rule") {
  if (dirty.value)
    dialog.warning({
      title: "放弃未保存的规则修改？",
      content: "切换规则会丢弃当前输入。",
      positiveText: "放弃并切换",
      negativeText: "取消",
      onPositiveClick: () => install(entry, nextKind),
    });
  else install(entry, nextKind);
}
async function load(preserve = false) {
  if (!store.target) return;
  busy.value = true;
  error.value = "";
  try {
    const data = await RuleService.Query(store.target);
    entries.value = data.entries ?? [];
    revision.value = data.revision;
    development.value = data.development;
    relations.value = data.relations ?? [];
    loaded.value = true;
    if (preserve && selected.value) {
      const entry = entries.value.find(
        (e) => e.id === selected.value?.id && e.kind === selected.value?.kind,
      );
      install(entry ?? null);
    } else install(null);
  } catch (e: any) {
    error.value = e?.message ?? String(e);
    loaded.value = false;
  } finally {
    busy.value = false;
  }
}
function reloadDraft() {
  if (dirty.value)
    dialog.warning({
      title: "重新加载最新规则？",
      content: "将放弃当前输入并读取最新配置，避免覆盖外部修改。",
      positiveText: "重新加载",
      negativeText: "取消",
      onPositiveClick: () => load(true),
    });
  else void load(true);
}
function toggleAdvanced(value: boolean) {
  if (!value) {
    try {
      draft.value = JSON.parse(raw.value);
      syncInputs();
      error.value = "";
    } catch {
      error.value = "规则 JSON 无效";
      return;
    }
  } else raw.value = JSON.stringify(draft.value, null, 2);
  advanced.value = value;
}
async function apply(action: string) {
  let saved = false;
  busy.value = true;
  error.value = "";
  try {
    if (action === "save") {
      if (advanced.value) draft.value = JSON.parse(raw.value);
      else
        for (const field of formFields.value)
          if (field.type === "json" && fieldInputs.value[field.path]?.trim())
            JSON.parse(fieldInputs.value[field.path]!);
    }
    await RuleService.Apply({
      category: store.target!.category,
      revision: revision.value,
      kind: kind.value,
      id: idLocked.value ? originalID.value : draft.value.id,
      action,
      json: JSON.stringify(draft.value),
    });
    saved = true;
    dirty.value = false;
    await load();
    if (!loaded.value) return;
    busy.value = true;
    if (isAnnotation.value)
      await Promise.all([
        editor.refreshAnnotations(),
        explorer.refreshAnnotations(),
      ]);
    else {
      await editor.refreshRenderedText();
    }
    message.success("规则已保存并生效");
  } catch (e: any) {
    error.value =
      (saved ? "规则已保存，但界面刷新失败：" : "") + (e?.message ?? String(e));
  } finally {
    busy.value = false;
  }
}
function requestApply(action: string) {
  const shared = kind.value === "field";
  if (action !== "save" || shared) {
    dialog.warning({
      title: shared ? "修改共享字段" : "修改规则",
      content: shared
        ? `该操作会影响关联预览及引用规则：${selected.value?.dependencies?.join("、") || "无显式引用规则"}。${action === "disable" || action === "delete" ? "屏蔽字段会同步停用依赖规则；删除新增字段或开发环境字段会同步删除同来源的引用规则。" : ""}`
        : "删除覆盖会恢复内置规则；屏蔽会停止使用内置规则。",
      positiveText: "确定",
      negativeText: "取消",
      onPositiveClick: () => apply(action),
    });
  } else void apply(action);
}
function close() {
  if (busy.value) return;
  if (dirty.value)
    dialog.warning({
      title: "放弃未保存的规则修改？",
      positiveText: "放弃",
      negativeText: "取消",
      onPositiveClick: () => {
        store.visible = false;
      },
    });
  else store.visible = false;
}
watch(
  () => [store.visible, store.target],
  () => {
    if (store.visible) {
      loaded.value = false;
      entries.value = [];
      void load();
    }
  },
  { deep: true },
);
onMounted(async () => {
  try {
    for (const warning of (await RuleService.StartupWarnings()) ?? [])
      message.error(warning, { duration: 15000 });
  } catch {
    /* Runtime may not yet be available in browser previews. */
  }
});
</script>

<template>
  <NModal
    :show="store.visible"
    preset="card"
    :title="title"
    style="width: min(1120px, 95vw)"
    :mask-closable="false"
    :close-on-esc="false"
    @close="close"
  >
    <div class="rule-context">
      {{ store.target?.path }}
      <span v-if="store.target?.section">[{{ store.target.section }}]</span> ·
      {{ development ? "开发：保存到源配置" : "生产：保存用户规则" }}
    </div>
    <NAlert v-if="error" type="error" style="margin: 12px 0">{{
      error
    }}</NAlert>
    <div class="rule-layout">
      <aside>
        <div class="rule-actions">
          <NButton :disabled="busy || !loaded" @click="selectEntry(null)"
            >新增规则</NButton
          ><NButton
            v-if="isAnnotation"
            :disabled="busy || !loaded"
            @click="selectEntry(null, 'field')"
            >新增字段</NButton
          >
        </div>
        <NCheckbox v-model:checked="showDisabled">显示已屏蔽规则</NCheckbox>
        <button
          v-for="entry in visibleEntries"
          :key="entry.kind + entry.id"
          class="rule-entry"
          :class="{
            selected:
              selected?.id === entry.id && selected?.kind === entry.kind,
          }"
          :disabled="busy"
          @click="selectEntry(entry)"
        >
          <strong>{{ entry.id }}</strong
          ><small
            >{{ entry.kind === "field" ? "共享字段 · " : ""
            }}{{ entry.source }}</small
          >
          <span
            ><NTag
              size="small"
              :type="
                entry.disabled
                  ? 'warning'
                  : entry.effective
                    ? 'success'
                    : 'default'
              "
              >{{
                entry.disabled
                  ? "已屏蔽"
                  : entry.effective
                    ? "匹配生效"
                    : "当前不生效"
              }}</NTag
            >
            <NTag v-if="entry.overridden" size="small">用户覆盖</NTag></span
          >
          <small>{{
            JSON.parse(entry.json).match?.glob ||
            JSON.parse(entry.json).match?.extensions?.join(", ") ||
            "全局"
          }}</small>
        </button>
        <p v-if="!visibleEntries.length">当前目标没有匹配规则。</p>
      </aside>
      <main v-if="loaded">
        <div class="rule-actions">
          <NCheckbox
            :checked="advanced"
            :disabled="busy"
            @update:checked="toggleAdvanced"
            >高级 JSON</NCheckbox
          ><NButton :disabled="busy" @click="reloadDraft">重新加载配置</NButton>
        </div>
        <NInput
          v-if="advanced"
          :value="raw"
          type="textarea"
          :autosize="{ minRows: 18, maxRows: 25 }"
          :disabled="busy"
          @update:value="
            (value) => {
              raw = value;
              dirty = true;
            }
          "
        />
        <div v-else class="rule-form">
          <label
            >ID<NInput
              :value="draft.id"
              :disabled="idLocked || busy"
              @update:value="(value) => set('id', value)"
          /></label>
          <label
            >作用范围<NSelect
              :value="scope"
              :options="scopeOptions"
              :disabled="busy"
              @update:value="chooseScope"
          /></label>
          <label
            >目标类型<NSelect
              :value="draft.target?.kind"
              :options="targetOptions"
              :disabled="busy"
              @update:value="changeTarget"
          /></label>
          <label v-if="isAnnotation"
            >标注类型<NSelect
              :value="draft.annotation?.type"
              :options="typeOptions"
              :disabled="busy"
              @update:value="changeAnnotationType"
          /></label>
          <label v-for="field in formFields" :key="field.path"
            >{{ field.label }}
            <NInputNumber
              v-if="field.type === 'number'"
              :value="get(field.path) ?? null"
              :min="0"
              :precision="0"
              :disabled="busy"
              clearable
              @update:value="(value) => set(field.path, value)"
            />
            <NCheckbox
              v-else-if="field.type === 'boolean'"
              :checked="!!get(field.path)"
              :disabled="busy"
              @update:checked="(value) => set(field.path, value)"
            />
            <NSelect
              v-else-if="field.path === 'annotation.relation'"
              :value="get(field.path) ?? null"
              :options="relations.map((value) => ({ label: value, value }))"
              :disabled="busy"
              clearable
              filterable
              @update:value="(value) => set(field.path, value)"
            />
            <NInput
              v-else
              :value="fieldInputs[field.path] ?? ''"
              :type="
                field.path === 'annotation.content' || field.type === 'json'
                  ? 'textarea'
                  : 'text'
              "
              :disabled="busy"
              @update:value="(value) => textInput(field, value)"
            />
          </label>
        </div>
        <p v-if="kind === 'field'" class="rule-context">
          共享字段的修改可能同时影响结构化预览。
        </p>
        <div class="rule-actions rule-footer">
          <NButton
            type="primary"
            :loading="busy"
            :disabled="!!selected?.disabled"
            @click="requestApply('save')"
            >保存并生效</NButton
          >
          <NButton
            v-if="selected && !selected.disabled"
            :disabled="busy"
            @click="requestApply('delete')"
            >{{ selected.overridden ? "删除覆盖，恢复内置" : "删除" }}</NButton
          >
          <NButton
            v-if="
              selected &&
              !development &&
              !selected.disabled &&
              !selected.source.endsWith('.user.json')
            "
            :disabled="busy"
            @click="requestApply('disable')"
            >屏蔽规则</NButton
          >
          <NButton
            v-if="
              selected &&
              !development &&
              (selected.disabled || selected.overridden)
            "
            :disabled="busy"
            @click="requestApply('restore')"
            >{{
              selected.source.endsWith(".user.json") ? "启用规则" : "恢复内置"
            }}</NButton
          >
        </div>
      </main>
    </div>
  </NModal>
</template>
<style scoped>
.rule-context {
  color: var(--pvf-text-muted);
  overflow-wrap: anywhere;
}
.rule-layout {
  display: grid;
  grid-template-columns: 300px 1fr;
  gap: 20px;
  margin-top: 12px;
}
aside,
main {
  max-height: 65vh;
  overflow: auto;
  min-width: 0;
}
.rule-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 12px;
}
.rule-entry {
  display: flex;
  flex-direction: column;
  gap: 7px;
  width: 100%;
  text-align: left;
  margin-top: 10px;
  padding: 12px;
  border: 1px solid var(--pvf-border-color, #555);
  border-radius: 6px;
  background: transparent;
  color: inherit;
  cursor: pointer;
  overflow-wrap: anywhere;
}
.rule-entry.selected {
  border-color: #63a9e8;
}
.rule-entry small {
  opacity: 0.7;
}
.rule-form {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
.rule-form label {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}
.rule-footer {
  position: sticky;
  bottom: 0;
  margin: 18px 0 0;
  padding: 12px 0;
  background: var(--pvf-panel-bg, #202129);
  z-index: 1;
}
@media (max-width: 800px) {
  .rule-layout {
    grid-template-columns: 1fr;
  }
  .rule-layout aside {
    max-height: 180px;
  }
  .rule-form {
    grid-template-columns: 1fr;
  }
}
</style>
