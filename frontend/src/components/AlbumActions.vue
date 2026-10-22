<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from "vue";
import { Ellipsis, Pencil, ShieldCheck, Star, Trash2 } from "@lucide/vue";
import type { Album } from "../types";

type Action = "access" | "rename" | "default" | "delete";
const props = defineProps<{ album: Album; busy?: boolean }>();
const emit = defineEmits<{ select: [action: Action] }>();
const trigger = ref<HTMLButtonElement>(),
  menu = ref<HTMLElement>(),
  open = ref(false);
const position = ref({ top: "0px", left: "0px" });
const items = computed(() => [
  {
    value: "access" as Action,
    label: "访问设置",
    icon: ShieldCheck,
    disabled: false,
  },
  { value: "rename" as Action, label: "重命名", icon: Pencil, disabled: false },
  {
    value: "default" as Action,
    label: "设为默认",
    icon: Star,
    disabled: props.album.is_default,
  },
  {
    value: "delete" as Action,
    label: "删除相册",
    icon: Trash2,
    disabled: props.album.is_default,
  },
]);
function close(restore = false) {
  menu.value?.hidePopover();
  open.value = false;
  if (restore) trigger.value?.focus({ preventScroll: true });
}
function place() {
  if (!open.value || !trigger.value || !menu.value) return;
  const anchor = trigger.value.getBoundingClientRect(),
    box = menu.value.getBoundingClientRect();
  if (
    anchor.bottom < 0 ||
    anchor.top > innerHeight ||
    anchor.right < 0 ||
    anchor.left > innerWidth
  ) {
    close();
    return;
  }
  position.value = {
    top: `${Math.max(8, anchor.bottom + box.height + 6 <= innerHeight - 8 ? anchor.bottom + 6 : anchor.top - box.height - 6)}px`,
    left: `${Math.max(8, Math.min(anchor.right - box.width, document.documentElement.clientWidth - box.width - 8))}px`,
  };
}
async function reveal(last = false) {
  if (props.busy) return;
  if (open.value) {
    close(true);
    return;
  }
  open.value = true;
  // 顶层弹层避免被相册表格的滚动容器裁切，并由浏览器处理外部点击关闭。
  menu.value?.showPopover();
  await nextTick();
  place();
  const buttons = menu.value?.querySelectorAll<HTMLButtonElement>(
    "button:not(:disabled)",
  );
  buttons?.[last ? buttons.length - 1 : 0]?.focus({ preventScroll: true });
}
function choose(value: Action) {
  close(true);
  emit("select", value);
}
function keydown(event: KeyboardEvent) {
  if (event.key === "Escape") {
    event.preventDefault();
    event.stopPropagation();
    close(true);
    return;
  }
  if (event.key === "Tab") {
    close();
    return;
  }
  if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
  event.preventDefault();
  const buttons = [
    ...(menu.value?.querySelectorAll<HTMLButtonElement>(
      "button:not(:disabled)",
    ) || []),
  ];
  if (!buttons.length) return;
  const current = buttons.findIndex(
    (button) => button === document.activeElement,
  );
  const index =
    event.key === "Home"
      ? 0
      : event.key === "End"
        ? buttons.length - 1
        : (current + (event.key === "ArrowDown" ? 1 : -1) + buttons.length) %
          buttons.length;
  buttons[index]?.focus();
}
function stopListening() {
  window.removeEventListener("resize", place);
  document.removeEventListener("scroll", place, true);
}
watch(open, (value) => {
  if (value) {
    window.addEventListener("resize", place);
    document.addEventListener("scroll", place, true);
  } else stopListening();
});
watch(
  () => props.busy,
  (value) => {
    if (value) close();
  },
);
onBeforeUnmount(stopListening);
</script>

<template>
  <div class="album-actions">
    <button
      ref="trigger"
      type="button"
      class="icon-button"
      :aria-label="`${album.name}的更多操作`"
      title="更多操作"
      aria-haspopup="menu"
      :aria-expanded="open"
      :disabled="busy"
      @click="reveal()"
      @keydown.down.prevent="reveal()"
      @keydown.up.prevent="reveal(true)"
    >
      <Ellipsis :size="18" />
    </button>
    <div
      ref="menu"
      popover="auto"
      role="menu"
      :aria-label="`${album.name}的操作`"
      class="album-action-menu"
      :style="position"
      @toggle="open = ($event as ToggleEvent).newState === 'open'"
      @keydown="keydown"
    >
      <button
        v-for="item in items"
        :key="item.value"
        type="button"
        role="menuitem"
        :disabled="item.disabled || busy"
        :class="{ 'is-danger': item.value === 'delete' }"
        :title="
          item.value === 'delete' && album.is_default
            ? '请先指定另一个默认相册'
            : undefined
        "
        @click="choose(item.value)"
      >
        <component :is="item.icon" :size="15" /><span>{{ item.label }}</span>
      </button>
      <p v-if="album.is_default">删除前，请先指定另一个默认相册。</p>
    </div>
  </div>
</template>

<style scoped>
.album-actions {
  display: inline-flex;
}
.album-action-menu {
  position: fixed;
  inset: auto;
  margin: 0;
  width: 210px;
  max-width: calc(100vw - 16px);
  max-height: calc(100dvh - 16px);
  overflow: auto;
  padding: 5px;
  border: 1px solid #dce5df;
  border-radius: 10px;
  background: white;
  color: var(--ink);
  box-shadow: 0 8px 24px #18352e20;
}
.album-action-menu button {
  display: flex;
  align-items: center;
  gap: 9px;
  width: 100%;
  min-height: 34px;
  padding: 7px 9px;
  border-radius: 6px;
  text-align: left;
  font-size: 12px;
}
.album-action-menu button:hover:not(:disabled),
.album-action-menu button:focus-visible {
  background: var(--mint);
}
.album-action-menu button:focus-visible {
  outline: 2px solid #759980;
  outline-offset: -2px;
}
.album-action-menu .is-danger {
  margin-top: 4px;
  border-top: 1px solid var(--line);
  border-radius: 0 0 6px 6px;
  color: #aa4e42;
}
.album-action-menu .is-danger:hover:not(:disabled) {
  background: #fff0eb;
}
.album-action-menu p {
  margin: 5px 9px 4px;
  font-size: 10px;
  line-height: 1.6;
  color: #52655b;
}
</style>
