<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import {
  NAlert,
  NButton,
  NCheckbox,
  NIcon,
  NInput,
  NSelect,
  useMessage,
} from "naive-ui";
import { FolderOpen24Regular, Sparkle24Regular } from "@vicons/fluent";
import { SkillService } from "../../bindings/pvfine/services";
import type { BuiltinSkill, SkillInstallResult } from "../../bindings/pvfine/services/models";

const message = useMessage();
const skills = ref<BuiltinSkill[]>([]);
const selected = ref<string[]>([]);
const target = ref("global");
const directory = ref("");
const resolved = ref("");
const pathError = ref("");
const loading = ref(false);
const ready = ref(false);
const overwrite = ref(false);
const results = ref<SkillInstallResult[]>([]);
const needsDirectory = computed(() => target.value === "project" || target.value === "custom");

const options = [
  { label: "通用 skill（~/.agents/skills）", value: "global" },
  { label: "项目级 skill（项目/.agents/skills）", value: "project" },
  { label: "Claude Code（~/.claude/skills）", value: "claude" },
  { label: "自定义 skill 根目录", value: "custom" },
];

let resolveSequence = 0;

async function resolveDirectory() {
  const sequence = ++resolveSequence;
  resolved.value = "";
  pathError.value = "";
  results.value = [];
  try {
    const path = await SkillService.ResolveDirectory(target.value, directory.value);
    if (sequence === resolveSequence) resolved.value = path;
  } catch (error: any) {
    if (sequence === resolveSequence) pathError.value = String(error?.message ?? error);
  }
}

watch([target, directory], resolveDirectory);
watch(overwrite, () => {
  results.value = [];
});

async function chooseDirectory() {
  try {
    const path = await SkillService.ChooseDirectory();
    if (path) directory.value = path;
  } catch (error: any) {
    message.error(String(error?.message ?? error));
  }
}

function toggle(id: string, checked: boolean) {
  selected.value = checked
    ? [...selected.value, id]
    : selected.value.filter((value) => value !== id);
  results.value = [];
}

async function install() {
  loading.value = true;
  try {
    results.value =
      (await SkillService.Install(selected.value, target.value, directory.value, overwrite.value)) ??
      [];
    const installed = results.value.filter((result) => result.status === "installed").length;
    const updated = results.value.filter((result) => result.status === "updated").length;
    const skipped = results.value.filter((result) => result.status === "skipped").length;
    const failed = results.value.filter((result) => result.status === "failed").length;
    if (failed) {
      message.error(
        `已安装 ${installed} 个，已更新 ${updated} 个，${failed} 个安装失败，请查看结果`
      );
    } else {
      message.success(
        `已安装 ${installed} 个，已更新 ${updated} 个，跳过 ${skipped} 个已有 skill`
      );
    }
  } catch (error: any) {
    message.error(String(error?.message ?? error));
  } finally {
    loading.value = false;
  }
}

onMounted(async () => {
  try {
    skills.value = (await SkillService.List()) ?? [];
    selected.value = skills.value.map((skill) => skill.id);
    ready.value = true;
    await resolveDirectory();
  } catch (error: any) {
    message.error(String(error?.message ?? error));
  }
});
</script>

<template>
  <section class="settings-group" aria-label="内置 skill 安装">
    <div class="group-header">
      <div class="group-title">内置 Skill 扩展</div>
      <div class="group-subtitle">
        为外部 AI Agent 安装 PVF 专用操作指令与编辑规范，配合 MCP 服务精准理解和修改归档
      </div>
    </div>

    <div class="settings-card">
      <!-- Skill 多选列表 -->
      <div class="skills-card-body">
        <div class="skills-card-header">
          <NIcon :size="16" class="skills-header-icon"><Sparkle24Regular /></NIcon>
          <span class="skills-header-text">可安装的 Agent 指南与规范</span>
        </div>

        <div class="skills-list">
          <label
            v-for="skill in skills"
            :key="skill.id"
            class="skill-item"
            :class="{ 'skill-item--checked': selected.includes(skill.id) }"
          >
            <NCheckbox
              :checked="selected.includes(skill.id)"
              :disabled="loading"
              @update:checked="toggle(skill.id, $event)"
            />
            <div class="skill-item-info">
              <span class="skill-item-title">{{ skill.title }}</span>
              <span class="skill-item-desc">{{ skill.description }}</span>
            </div>
          </label>
        </div>
      </div>

      <div class="setting-card-divider" />

      <!-- 安装位置选择 -->
      <div class="setting-item">
        <div class="setting-item-icon">
          <NIcon :size="18"><FolderOpen24Regular /></NIcon>
        </div>
        <div class="setting-item-content">
          <div class="setting-item-label">安装目标位置</div>
          <div class="setting-item-desc">选择 Skill 存放的目标环境或客户端目录</div>

          <div class="target-select-row">
            <NSelect
              v-model:value="target"
              :options="options"
              :disabled="loading"
              size="small"
              class="target-select"
            />
          </div>

          <div v-if="needsDirectory" class="directory-picker-row">
            <NInput
              v-model:value="directory"
              :disabled="loading"
              size="small"
              :placeholder="target === 'project' ? '选择项目根目录' : '输入 skill 根目录的绝对路径'"
              aria-label="skill 安装目录"
            />
            <NButton size="small" secondary :disabled="loading" @click="chooseDirectory">
              选择目录
            </NButton>
          </div>

          <div v-if="resolved" class="resolved-path-badge">
            <span class="path-label">目标路径：</span>
            <span class="path-val">{{ resolved }}</span>
          </div>
          <div v-else-if="pathError" class="path-error-text" role="status">
            {{ pathError }}
          </div>
        </div>
      </div>

      <div class="setting-card-divider" />

      <!-- 操作与覆盖选项 -->
      <div class="skill-footer-row">
        <NCheckbox v-model:checked="overwrite" :disabled="loading" size="small">
          覆盖已有 skill（更新内置文件，保留用户额外文件）
        </NCheckbox>

        <NButton
          type="primary"
          secondary
          size="small"
          :loading="loading"
          :disabled="!ready || !resolved || !selected.length || loading"
          @click="install"
        >
          安装选中的 Skill ({{ selected.length }})
        </NButton>
      </div>

      <!-- 安装结果反馈 -->
      <div v-if="results.length > 0" class="install-results-area">
        <NAlert
          v-for="result in results"
          :key="result.id"
          :type="
            result.status === 'failed'
              ? 'error'
              : result.status === 'installed' || result.status === 'updated'
              ? 'success'
              : 'info'
          "
          :title="`${result.id} · ${result.message}`"
          :bordered="false"
          size="small"
          class="result-alert-item"
        >
          <span class="destination-text">{{ result.path }}</span>
        </NAlert>
      </div>
    </div>
  </section>
</template>

<style scoped>
.settings-group {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.group-header {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.group-title {
  color: var(--pvf-text-primary);
  font-size: 13px;
  font-weight: 600;
}
.group-subtitle {
  color: var(--pvf-text-faint);
  font-size: 12px;
}

.settings-card {
  background: var(--pvf-surface-card);
  border: 1px solid var(--pvf-border-subtle);
  border-radius: 10px;
  overflow: hidden;
}
.setting-card-divider {
  height: 1px;
  margin: 0 16px;
  background: var(--pvf-border-faint);
}

.skills-card-body {
  padding: 14px 16px;
}

.skills-card-header {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 10px;
}
.skills-header-icon {
  color: var(--pvf-primary);
}
.skills-header-text {
  font-size: 12px;
  font-weight: 600;
  color: var(--pvf-text-secondary);
}

.skills-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.skill-item {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 10px 12px;
  background: var(--pvf-surface-subtle);
  border: 1px solid var(--pvf-border-faint);
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.15s ease;
}
.skill-item:hover {
  background: var(--pvf-surface-hover);
  border-color: var(--pvf-border-subtle);
}
.skill-item--checked {
  border-color: var(--pvf-primary-soft);
  background: var(--pvf-primary-soft);
}

.skill-item-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.skill-item-title {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--pvf-text-primary);
}
.skill-item-desc {
  font-size: 11.5px;
  color: var(--pvf-text-faint);
  line-height: 1.45;
}

.setting-item {
  display: flex;
  align-items: flex-start;
  gap: 14px;
  padding: 14px 16px;
}
.setting-item-icon {
  flex: 0 0 34px;
  height: 34px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--pvf-surface-subtle);
  border: 1px solid var(--pvf-border-faint);
  border-radius: 8px;
  color: var(--pvf-primary);
  margin-top: 2px;
}
.setting-item-content {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.setting-item-label {
  color: var(--pvf-text-primary);
  font-size: 13px;
  font-weight: 500;
}
.setting-item-desc {
  color: var(--pvf-text-faint);
  font-size: 12px;
  line-height: 1.45;
}

.target-select-row {
  margin-top: 4px;
}
.target-select {
  max-width: 340px;
}

.directory-picker-row {
  display: flex;
  gap: 8px;
  margin-top: 4px;
}

.resolved-path-badge {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  margin-top: 4px;
  background: var(--pvf-surface-subtle);
  border: 1px solid var(--pvf-border-faint);
  border-radius: 6px;
  font-size: 11.5px;
}
.path-label {
  color: var(--pvf-text-faint);
  flex: 0 0 auto;
}
.path-val {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  color: var(--pvf-text-primary);
  word-break: break-all;
}

.path-error-text {
  color: var(--pvf-error);
  font-size: 11.5px;
  margin-top: 4px;
}

.skill-footer-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
  padding: 12px 16px;
  background: var(--pvf-surface-subtle);
}

.install-results-area {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 16px;
  border-top: 1px solid var(--pvf-border-faint);
}
.result-alert-item {
  border-radius: 7px;
}
.destination-text {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 11px;
  word-break: break-all;
}
</style>
