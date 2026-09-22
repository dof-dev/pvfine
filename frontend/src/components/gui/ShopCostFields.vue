<script setup lang="ts">
import { NButton, NInput } from "naive-ui";
import ItemPicker from "../ItemPicker.vue";
import { materialRow, type ShopCostForm } from "../../gui/shopForm";
const props = defineProps<{ modelValue: ShopCostForm; disabled?: boolean; goldEnabled?: boolean; materialsEnabled?: boolean }>();
const emit = defineEmits<{ (e: "update:modelValue", value: ShopCostForm): void }>();
function gold(value: string) { emit("update:modelValue", { ...props.modelValue, gold: value }); }
function update(index: number, values: object) { emit("update:modelValue", { ...props.modelValue, materials: props.modelValue.materials.map((row, i) => i === index ? { ...row, ...values } : row) }); }
function remove(index: number) { emit("update:modelValue", { ...props.modelValue, materials: props.modelValue.materials.filter((_, i) => i !== index) }); }
function add() { emit("update:modelValue", { ...props.modelValue, materials: [...props.modelValue.materials, materialRow()] }); }
</script>
<template>
  <div class="shop-cost-form">
    <label>金币价格</label>
    <NInput :value="modelValue.gold" :disabled="disabled || goldEnabled === false" placeholder="留空取消金币价格；0 表示零金币" @update:value="gold" />
    <label>兑换道具</label>
    <div v-for="(row, i) in modelValue.materials" :key="row.key" class="material-form-row">
      <ItemPicker :model-value="row.itemId" :label="row.name" :disabled="disabled || materialsEnabled === false"
        @update:model-value="update(i, { itemId: $event })" @select="update(i, { name: $event?.name ?? '' })" />
      <NInput class="material-quantity" :value="row.quantity" :disabled="disabled || materialsEnabled === false" placeholder="数量" aria-label="兑换道具数量" @update:value="update(i, { quantity: $event })" />
      <NButton :disabled="disabled || materialsEnabled === false" @click="remove(i)">移除</NButton>
    </div>
    <NButton dashed :disabled="disabled || materialsEnabled === false" @click="add">添加兑换道具</NButton>
  </div>
</template>
<style scoped>
.shop-cost-form { display: flex; flex-direction: column; gap: 8px; }
.shop-cost-form > label { margin-top: 6px; }
.material-form-row { display: flex; gap: 6px; align-items: center; }
.material-quantity { width: 90px; flex: 0 0 90px; }
small { color: var(--pvf-text-muted); }
</style>
