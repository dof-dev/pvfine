<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue';
import { NAlert, NButton, NCheckbox, NModal, NSelect, NSpin } from 'naive-ui';
import { FileGUIService } from '../../../bindings/pvfine/services';
import type { ItemEffectPreview } from '../../../bindings/pvfine/services/models';
import { useEditorStore } from '../../stores/editor';
import { useFileGUIStore } from '../../stores/fileGUI';
import { useImageStore } from '../../stores/images';
import { effectFrameIndex, particleInstances } from '../../gui/effectPlayback';
const props = defineProps<{ show: boolean; active: boolean; fileIndex: number; objectID?: string; particle?: string }>();
const emit = defineEmits<{ (e: 'update:show', value: boolean): void }>();
const editor = useEditorStore(), gui = useFileGUIStore(), images = useImageStore();
const document = ref<ItemEffectPreview | null>(null), error = ref(''), loading = ref(false), canvas = ref<HTMLCanvasElement | null>(null);
const paused = ref(false), speed = ref(1), enabled = ref<boolean[]>([]), missing = ref(0);
const visible = computed(() => props.show && props.active);
let generation = 0, raf = 0, elapsed = 0, previous = 0;
const bitmaps = new Map<string, HTMLImageElement>();
const key = (image: {path: string;index: number}) => `${image.path}\u0000${image.index}`;
function stop() { cancelAnimationFrame(raf); raf = 0; previous = 0; }
async function load() {
  const request = ++generation; stop(); document.value = null; bitmaps.clear(); error.value = ''; missing.value = 0; elapsed = 0;
  if (!visible.value) { loading.value = false; return; }
  loading.value = true;
  try {
    const result = await FileGUIService.ReadItemEffectPreview(props.fileIndex, props.objectID ?? '', props.particle ?? '', editor.tabs.filter(tab => tab.text !== tab.original).map(tab => ({ fileIndex: tab.index, path: tab.path, text: tab.text })));
    if (request !== generation) return;
    document.value = result; enabled.value = (result?.layers ?? []).map(() => true);
    await images.initialize();
    if (request !== generation) return;
    const references = new Map<string, {path: string;index: number}>();
    for (const layer of result?.layers ?? []) for (const frame of layer.animation?.frames ?? []) for (const sprite of frame.layers ?? []) references.set(key(sprite.image), sprite.image);
    if (references.size > 1200) throw new Error('预览贴图超过 1200 帧，请在文件预览中分别查看动画');
    // ImageStore batches frame reads; bound JS image decoding concurrency too.
    const queue = [...references.values()];
    await Promise.all(Array.from({length: Math.min(8, queue.length)}, async () => {
      while (queue.length && request === generation) {
        const reference = queue.shift()!;
        try {
          const data = await images.loadImage(reference);
          if (request !== generation) return;
          if (!data) { missing.value++; continue; }
          const bitmap = new Image(); bitmap.src = data.dataUrl; await bitmap.decode();
          if (request === generation) bitmaps.set(key(reference), bitmap);
        } catch { if (request === generation) missing.value++; }
      }
    }));
    if (request !== generation) return;
    await nextTick(); raf = requestAnimationFrame(draw);
  } catch (cause) { if (request === generation) error.value = String(cause); }
  finally { if (request === generation) loading.value = false; }
}
function draw(time: number) {
  if (!visible.value) return;
  const context = canvas.value?.getContext('2d');
  if (!context) { raf = requestAnimationFrame(draw); return; }
  if (previous && !paused.value) elapsed += Math.min(100, time - previous) * speed.value;
  previous = time;
  context.clearRect(0, 0, 720, 400); context.imageSmoothingEnabled = false;
  context.strokeStyle = '#77808b'; context.globalAlpha = .25; context.beginPath(); context.moveTo(0, 300); context.lineTo(720, 300); context.moveTo(360, 0); context.lineTo(360, 400); context.stroke(); context.globalAlpha = 1;
  for (const [index, layer] of (document.value?.layers ?? []).entries()) {
    if (!enabled.value[index] || !layer.animation) continue;
    const animation = layer.animation, frames = animation.frames ?? [], delays = frames.map(frame => frame.delayMs);
    const instances = layer.particle ? particleInstances({...layer.particle, position: layer.particle.position ?? [], area: layer.particle.area ?? [], alpha: layer.particle.alpha ?? [], velocity: layer.particle.velocity ?? [], acceleration: layer.particle.acceleration ?? []}, elapsed) : [{x: 0, y: 0, alpha: 1, age: elapsed}];
    for (const instance of instances) {
      const frame = frames[effectFrameIndex(delays, instance.age, layer.particle ? true : animation.loop)];
      if (!frame) continue;
      context.globalAlpha = instance.alpha;
      for (const sprite of frame.layers ?? []) {
        const bitmap = bitmaps.get(key(sprite.image)); if (!bitmap) continue;
        context.drawImage(bitmap, Math.round(360 + instance.x + sprite.x), Math.round(300 + instance.y + sprite.y));
      }
    }
  }
  context.globalAlpha = 1; raf = requestAnimationFrame(draw);
}
watch(() => [visible.value, props.fileIndex, props.objectID, props.particle, gui.epoch, gui.revision, images.revision], () => { void load(); }, {immediate: true});
onBeforeUnmount(() => { generation++; stop(); bitmaps.clear(); });
</script>
<template>
  <NModal :show="visible" preset="card" title="对象与粒子预览" style="width:min(840px,94vw)" @update:show="emit('update:show', $event)">
    <NSpin :show="loading">
      <NAlert v-if="error" type="error">{{ error }} <NButton text @click="load">重试</NButton></NAlert>
      <div class="stage"><canvas ref="canvas" width="720" height="400" aria-label="对象与粒子动画画布" /></div>
      <div class="controls"><NButton size="small" @click="paused = !paused">{{ paused ? '播放' : '暂停' }}</NButton><NButton size="small" @click="elapsed = 0">重新播放</NButton><NSelect v-model:value="speed" style="width:100px" :options="[.25,.5,1,2].map(value=>({value,label:`${value}×`}))" aria-label="预览速度" /></div>
      <p v-if="document?.objectPath" class="path">{{ document.objectPath }}</p>
      <label v-for="(layer,i) in document?.layers ?? []" :key="i" class="layer"><NCheckbox v-model:checked="enabled[i]">{{ layer.particle ? '粒子' : '基础动画' }} · {{ layer.path }}</NCheckbox></label>
      <NAlert v-for="(issue,i) in document?.issues ?? []" :key="i" type="warning" :show-icon="false">{{ issue }}</NAlert>
      <NAlert v-if="missing" type="warning" :show-icon="false">{{ missing }} 个贴图帧未能读取，请检查 NPK 资源目录。</NAlert>
      <p class="hint">按动画帧及粒子的寿命、生成频率、位移、重力和透明度近似预览；不执行动作脚本、碰撞、旋转和战斗逻辑。IMG 直接引用展示首帧。</p>
    </NSpin>
  </NModal>
</template>
<style scoped>.stage{background:var(--pvf-surface-panel,#202328);border:1px solid var(--pvf-border-subtle);overflow:hidden}.stage canvas{display:block;width:100%;height:auto;image-rendering:pixelated}.controls{display:flex;gap:8px;margin-top:12px}.path,.hint{font-size:12px;color:var(--pvf-text-muted);overflow-wrap:anywhere}.layer{display:block;padding:6px 0;overflow-wrap:anywhere}.hint{line-height:1.6}</style>
