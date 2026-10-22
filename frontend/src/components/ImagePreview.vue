<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { ImageOff, Maximize2, ZoomIn, ZoomOut } from "@lucide/vue";

defineProps<{ src: string; alt: string }>();
const viewport = ref<HTMLElement>();
const ready = ref(false),
  failed = ref(false),
  fitting = ref(true);
const size = ref({ width: 0, height: 0 });
const bounds = ref({ width: 0, height: 0 });
const scale = ref(1),
  position = ref({ x: 0, y: 0 });
const dragging = ref(false);
const fitScale = computed(() =>
  size.value.width && size.value.height
    ? Math.min(
        Math.max(1, bounds.value.width - 32) / size.value.width,
        Math.max(1, bounds.value.height - 32) / size.value.height,
        1,
      )
    : 1,
);
const minimum = computed(() => Math.min(fitScale.value, 0.1));
const canPan = computed(
  () =>
    ready.value &&
    (size.value.width * scale.value > bounds.value.width ||
      size.value.height * scale.value > bounds.value.height),
);
const imageStyle = computed(() => ({
  width: `${size.value.width}px`,
  height: `${size.value.height}px`,
  transform: `translate(-50%, -50%) translate(${position.value.x}px, ${position.value.y}px) scale(${scale.value})`,
}));
let observer: ResizeObserver | undefined;
let drag:
  | { id: number; x: number; y: number; origin: { x: number; y: number } }
  | undefined;

function constrain(point: { x: number; y: number }) {
  const x = Math.max(
    0,
    (size.value.width * scale.value - bounds.value.width) / 2,
  );
  const y = Math.max(
    0,
    (size.value.height * scale.value - bounds.value.height) / 2,
  );
  return {
    x: Math.max(-x, Math.min(x, point.x)),
    y: Math.max(-y, Math.min(y, point.y)),
  };
}
function fit() {
  fitting.value = true;
  scale.value = fitScale.value;
  position.value = { x: 0, y: 0 };
}
function zoom(value: number, anchor = { x: 0, y: 0 }) {
  if (!ready.value) return;
  const next = Math.max(minimum.value, Math.min(8, value));
  const ratio = next / scale.value;
  fitting.value = false;
  scale.value = next;
  // 缩放时保留指针下的图像位置，再限制拖动范围，避免把图片移出视口。
  position.value = constrain({
    x: anchor.x - (anchor.x - position.value.x) * ratio,
    y: anchor.y - (anchor.y - position.value.y) * ratio,
  });
}
function loaded(event: Event) {
  const image = event.target as HTMLImageElement;
  size.value = { width: image.naturalWidth, height: image.naturalHeight };
  ready.value = true;
  fit();
}
function wheel(event: WheelEvent) {
  // 保留浏览器自身的 Ctrl + 滚轮缩放，普通滚轮仅调整当前图片。
  if (!ready.value || event.ctrlKey || !viewport.value) return;
  event.preventDefault();
  const rect = viewport.value.getBoundingClientRect();
  const delta =
    event.deltaY *
    (event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? rect.height : 1);
  zoom(scale.value * Math.exp(-Math.max(-100, Math.min(100, delta)) / 300), {
    x: event.clientX - rect.left - rect.width / 2,
    y: event.clientY - rect.top - rect.height / 2,
  });
}
function startDrag(event: PointerEvent) {
  if (!canPan.value || drag || event.button !== 0 || !viewport.value) return;
  event.preventDefault();
  viewport.value.focus({ preventScroll: true });
  viewport.value.setPointerCapture(event.pointerId);
  drag = {
    id: event.pointerId,
    x: event.clientX,
    y: event.clientY,
    origin: { ...position.value },
  };
  dragging.value = true;
}
function move(event: PointerEvent) {
  if (!drag || event.pointerId !== drag.id) return;
  position.value = constrain({
    x: drag.origin.x + event.clientX - drag.x,
    y: drag.origin.y + event.clientY - drag.y,
  });
}
function stopDrag(event: PointerEvent) {
  if (!drag || event.pointerId !== drag.id) return;
  drag = undefined;
  dragging.value = false;
  if (viewport.value?.hasPointerCapture(event.pointerId))
    viewport.value.releasePointerCapture(event.pointerId);
}
function keydown(event: KeyboardEvent) {
  if (!ready.value || event.ctrlKey || event.metaKey || event.altKey) return;
  switch (event.key) {
    case "+":
    case "=":
      zoom(scale.value * 1.25);
      break;
    case "-":
    case "_":
      zoom(scale.value / 1.25);
      break;
    case "0":
      fit();
      break;
    case "1":
      zoom(1);
      break;
    case "ArrowLeft":
    case "ArrowRight":
    case "ArrowUp":
    case "ArrowDown":
      if (!canPan.value) return;
      position.value = constrain({
        x:
          position.value.x +
          (event.key === "ArrowLeft"
            ? -40
            : event.key === "ArrowRight"
              ? 40
              : 0),
        y:
          position.value.y +
          (event.key === "ArrowUp" ? -40 : event.key === "ArrowDown" ? 40 : 0),
      });
      break;
    default:
      return;
  }
  event.preventDefault();
}
onMounted(() => {
  observer = new ResizeObserver(([entry]) => {
    if (!entry) return;
    bounds.value = {
      width: entry.contentRect.width,
      height: entry.contentRect.height,
    };
    if (fitting.value) fit();
    else {
      scale.value = Math.max(minimum.value, scale.value);
      position.value = constrain(position.value);
    }
  });
  if (viewport.value) observer.observe(viewport.value);
});
onBeforeUnmount(() => observer?.disconnect());
</script>

<template>
  <div class="detail-preview">
    <div
      ref="viewport"
      class="detail-image-viewport"
      :class="{ 'can-pan': canPan, 'is-dragging': dragging }"
      role="region"
      aria-label="图片预览，支持滚轮和加减键缩放，方向键移动，0 适应窗口，1 原始大小"
      tabindex="0"
      @wheel="wheel"
      @pointerdown="startDrag"
      @pointermove="move"
      @pointerup="stopDrag"
      @pointercancel="stopDrag"
      @lostpointercapture="stopDrag"
      @keydown="keydown"
    >
      <img
        v-if="!failed"
        v-show="ready"
        class="detail-zoom-image"
        :src="src"
        :alt="alt"
        :style="imageStyle"
        :draggable="false"
        @load="loaded"
        @error="
          failed = true;
          ready = false;
        "
      />
      <div v-if="failed" class="empty-state">
        <ImageOff :size="36" />
        <p>图片暂时无法加载</p>
      </div>
      <p v-else-if="!ready" class="detail-preview-status" role="status">
        正在加载图片…
      </p>
    </div>
    <div class="detail-zoom-toolbar" role="group" aria-label="图片缩放控制">
      <button
        class="icon-button"
        type="button"
        aria-label="缩小图片"
        title="缩小（−）"
        :disabled="!ready || scale <= minimum"
        @click="zoom(scale / 1.25)"
      >
        <ZoomOut :size="17" />
      </button>
      <output class="detail-zoom-value" aria-label="当前缩放比例">{{
        ready ? `${Math.round(scale * 100)}%` : "—"
      }}</output>
      <button
        class="icon-button"
        type="button"
        aria-label="放大图片"
        title="放大（+）"
        :disabled="!ready || scale >= 8"
        @click="zoom(scale * 1.25)"
      >
        <ZoomIn :size="17" />
      </button>
      <button
        class="button compact detail-original-size"
        type="button"
        aria-label="原始大小（100%）"
        :disabled="!ready"
        @click="zoom(1)"
      >
        100%
      </button>
      <button
        class="button compact"
        type="button"
        :disabled="!ready"
        @click="fit"
      >
        <Maximize2 :size="15" />适应窗口
      </button>
    </div>
  </div>
</template>
