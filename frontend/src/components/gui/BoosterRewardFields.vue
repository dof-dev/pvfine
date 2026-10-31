<script setup lang="ts">
import { NInput } from 'naive-ui';
import type { BoosterRow } from '../../gui/itemBooster';
import ItemPicker from '../ItemPicker.vue';
defineProps<{row:BoosterRow;random:boolean;label?:string;disabled?:boolean}>();
</script>
<template>
 <div class="reward-fields">
  <label class="reward-picker">物品<ItemPicker v-model="row.id" :label="label" :disabled="disabled" /></label>
  <label>数量<NInput v-model:value="row.quantity" aria-label="物品数量" :disabled="disabled" /></label>
  <label v-if="random">权重<NInput v-model:value="row.weight" aria-label="物品权重" :disabled="disabled" /></label>
  <template v-if="row.avatarParams"><label v-for="(_,i) in row.avatarParams" :key="i">附加参数 {{i+1}}<NInput v-model:value="row.avatarParams[i]" :aria-label="`附加参数${i+1}`" :disabled="disabled" /></label></template>
 </div>
</template>
<style scoped>
.reward-fields{display:flex;flex-wrap:wrap;gap:12px}.reward-fields label{display:grid;gap:6px;width:100px;font-size:12px;color:var(--pvf-text-secondary)}.reward-fields .reward-picker{flex:1;min-width:200px}@media(max-width:600px){.reward-fields .reward-picker{flex-basis:100%}}
</style>
