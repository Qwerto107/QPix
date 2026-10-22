<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  useId,
} from "vue";
import { Check, ChevronDown } from "@lucide/vue";

const props = defineProps<{
  id: string;
  label: string;
  modelValue: string;
  options: { value: string; label: string }[];
}>();
const emit = defineEmits<{ "update:modelValue": [value: string] }>();
const listID = useId();
const root = ref<HTMLElement>();
const menu = ref<HTMLElement>();
const open = ref(false);
const above = ref(false);
const menuHeight = ref(224);
const active = ref(0);
const selected = computed(() =>
  props.options.find((option) => option.value === props.modelValue),
);
let search = "";
let lastKey = 0;

function reveal() {
  const bounds = root.value?.getBoundingClientRect();
  const height = Math.min(props.options.length * 33 + 12, 224);
  if (!bounds) return;
  let top = 8,
    bottom = window.innerHeight - 8;
  // 同时考虑滚动容器的裁切边界，避免弹窗中的长菜单展开到不可见区域。
  for (
    let parent = root.value?.parentElement;
    parent;
    parent = parent.parentElement
  ) {
    if (!/auto|scroll|hidden|clip/.test(getComputedStyle(parent).overflowY))
      continue;
    const rect = parent.getBoundingClientRect();
    top = Math.max(top, rect.top + parent.clientTop);
    bottom = Math.min(
      bottom,
      rect.top + parent.clientTop + parent.clientHeight,
    );
  }
  const below = Math.max(0, bottom - bounds.bottom - 6);
  const upper = Math.max(0, bounds.top - top - 6);
  above.value = below < height && upper > below;
  menuHeight.value = Math.min(height, above.value ? upper : below);
  open.value = true;
  active.value = Math.max(
    0,
    props.options.findIndex((option) => option.value === props.modelValue),
  );
  scrollActive();
}
function scrollActive() {
  // 只滚动选项列表，避免打开菜单时带动整个上传页面跳动。
  nextTick(() => {
    const list = menu.value;
    const item = list?.querySelector<HTMLElement>(
      `[data-index="${active.value}"]`,
    );
    if (!list || !item) return;
    if (item.offsetTop < list.scrollTop) list.scrollTop = item.offsetTop;
    else if (
      item.offsetTop + item.offsetHeight >
      list.scrollTop + list.clientHeight
    ) {
      list.scrollTop = item.offsetTop + item.offsetHeight - list.clientHeight;
    }
  });
}
function choose(index: number) {
  const option = props.options[index];
  if (option) emit("update:modelValue", option.value);
  open.value = false;
}
function keydown(event: KeyboardEvent) {
  if (event.key === "Tab" || event.key === "Escape") {
    open.value = false;
    return;
  }
  if (["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
    event.preventDefault();
    if (!open.value) reveal();
    else if (event.key === "ArrowDown")
      active.value = Math.min(props.options.length - 1, active.value + 1);
    else if (event.key === "ArrowUp")
      active.value = Math.max(0, active.value - 1);
    if (event.key === "Home") active.value = 0;
    if (event.key === "End") active.value = props.options.length - 1;
    scrollActive();
  } else if (event.key === "Enter" || event.key === " ") {
    event.preventDefault();
    if (open.value) choose(active.value);
    else reveal();
  } else if (
    event.key.length === 1 &&
    !event.ctrlKey &&
    !event.metaKey &&
    !event.altKey
  ) {
    if (!open.value) reveal();
    search = Date.now() - lastKey > 700 ? event.key : search + event.key;
    lastKey = Date.now();
    const index = props.options.findIndex((option) =>
      option.label.toLocaleLowerCase().startsWith(search.toLocaleLowerCase()),
    );
    if (index >= 0) active.value = index;
    scrollActive();
  }
}
function outside(event: PointerEvent) {
  if (!root.value?.contains(event.target as Node)) open.value = false;
}
onMounted(() => document.addEventListener("pointerdown", outside));
onBeforeUnmount(() => document.removeEventListener("pointerdown", outside));
</script>

<template>
  <div
    ref="root"
    class="select-field"
    @focusout="!root?.contains($event.relatedTarget as Node) && (open = false)"
  >
    <button
      :id="id"
      type="button"
      class="select-trigger"
      role="combobox"
      :aria-label="label"
      aria-haspopup="listbox"
      :aria-expanded="open"
      :aria-controls="listID"
      :aria-activedescendant="open ? `${listID}-${active}` : undefined"
      @click="open ? (open = false) : reveal()"
      @keydown="keydown"
    >
      <span>{{ selected?.label || "请选择" }}</span>
      <ChevronDown :size="14" :class="{ 'select-chevron-open': open }" />
    </button>
    <ul
      v-if="open"
      ref="menu"
      :id="listID"
      role="listbox"
      :aria-label="label"
      class="select-menu"
      :class="{ 'select-menu-above': above }"
      :style="{ maxHeight: `${menuHeight}px` }"
    >
      <li
        v-for="(option, index) in options"
        :id="`${listID}-${index}`"
        :key="option.value"
        role="option"
        :aria-selected="option.value === modelValue"
        :data-index="index"
        :class="{ 'select-option-active': index === active }"
        @pointermove="active = index"
        @pointerdown.prevent
        @click="choose(index)"
      >
        <span>{{ option.label }}</span>
        <Check v-if="option.value === modelValue" :size="14" />
      </li>
    </ul>
  </div>
</template>
