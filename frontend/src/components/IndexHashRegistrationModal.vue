<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { NButton, NInput, NModal, NSelect, useMessage } from "naive-ui";
import { useArchiveStore } from "../stores/archive";
import type { IndexHashTarget } from "../../bindings/pvfine/services/models";

const props = defineProps<{ show: boolean }>();
const emit = defineEmits<{ "update:show": [value: boolean] }>();

const archive = useArchiveStore();
const message = useMessage();
const loading = ref(false);
const targets = ref<IndexHashTarget[]>([]);
const listPath = ref("");
const idsText = ref("");

const selectOptions = computed(() =>
  targets.value.map((target) => ({
    label: `${target.listPath}  →  ${target.indexHashPath}`,
    value: target.listPath,
  })),
);

watch(
  () => props.show,
  (show) => {
    if (show) void loadTargets();
  },
);

async function loadTargets(): Promise<void> {
  loading.value = true;
  try {
    targets.value = await archive.indexHashTargets();
    if (!targets.value.some((target) => target.listPath === listPath.value)) {
      listPath.value = targets.value[0]?.listPath ?? "";
    }
  } catch (error: any) {
    message.error(`读取 indexhash 列表失败：${error?.message ?? error}`);
    emit("update:show", false);
  } finally {
    loading.value = false;
  }
}

function close(): void {
  if (!loading.value) emit("update:show", false);
}

async function confirm(): Promise<void> {
  if (loading.value) return;
  if (!listPath.value) {
    message.warning("请选择 lst");
    return;
  }
  const ids = idsText.value.split(/[\s,，;；]+/).filter(Boolean);
  if (ids.length === 0) {
    message.warning("请输入至少一个 id");
    return;
  }
  loading.value = true;
  try {
    const result = await archive.registerMissingIndexHashes(listPath.value, ids);
    if (!result) throw new Error("后端没有返回处理结果");
    const invalid = result.invalidIds ?? [];
    if (invalid.length > 0) {
      message.warning(`已跳过无效 id：${invalid.join(", ")}`);
    }
    if (result.added > 0) {
      message.success(`已写入 ${result.added} 个 hash，已有 ${result.existing} 个跳过`);
    } else if (result.existing > 0) {
      message.info(`输入的 id 都已有 hash，共跳过 ${result.existing} 个`);
    } else {
      message.warning("没有写入新的 hash");
    }
    idsText.value = "";
    emit("update:show", false);
  } catch (error: any) {
    message.error(`注册 hash 失败：${error?.message ?? error}`);
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    title="注册 hash"
    style="width: min(620px, calc(100vw - 32px))"
    :mask-closable="!loading"
    @update:show="emit('update:show', $event)"
  >
    <div class="hash-registration-form">
      <div class="hash-registration-hint">
        只处理当前 110page 归档。输入的 id 中，已经存在于所选 indexhash 的会自动跳过。
      </div>
      <NSelect
        v-model:value="listPath"
        :options="selectOptions"
        :loading="loading && targets.length === 0"
        placeholder="选择 lst"
      />
      <NInput
        v-model:value="idsText"
        type="textarea"
        :autosize="{ minRows: 3, maxRows: 8 }"
        placeholder="输入多个 id，用空格、逗号或换行分隔"
      />
    </div>
    <template #footer>
      <div class="hash-registration-footer">
        <NButton size="small" :disabled="loading" @click="close">取消</NButton>
        <NButton size="small" type="primary" :loading="loading" @click="confirm">
          确定
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.hash-registration-form {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.hash-registration-hint {
  color: var(--pvf-text-muted);
  font-size: 12px;
  line-height: 1.5;
}
.hash-registration-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
