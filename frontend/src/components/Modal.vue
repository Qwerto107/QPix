<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import { X } from "@lucide/vue";

const props = defineProps<{ title: string; wide?: boolean; busy?: boolean }>();
const emit = defineEmits<{ close: [] }>();
const panel = ref<HTMLElement>();
const previousFocus = document.activeElement as HTMLElement | null;
const previousOverflow = document.body.style.overflow;

function keydown(event: KeyboardEvent) {
  if (event.key === "Escape" && !props.busy) {
    event.preventDefault();
    emit("close");
  }
  if (event.key !== "Tab" || !panel.value) return;
  const elements = Array.from(
    panel.value.querySelectorAll<HTMLElement>(
      'button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), a[href], [tabindex="0"]',
    ),
  ).filter((element) => element.offsetParent !== null);
  const first = elements[0],
    last = elements.at(-1);
  if (!first) {
    event.preventDefault();
    panel.value.focus();
    return;
  }
  if (
    event.shiftKey &&
    (document.activeElement === first || document.activeElement === panel.value)
  ) {
    event.preventDefault();
    last?.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}
onMounted(async () => {
  document.body.style.overflow = "hidden";
  document.addEventListener("keydown", keydown);
  await nextTick();
  panel.value?.focus();
});
onBeforeUnmount(() => {
  document.body.style.overflow = previousOverflow;
  document.removeEventListener("keydown", keydown);
  previousFocus?.focus();
});
</script>

<template>
  <Teleport to="body">
    <div class="modal-backdrop" @click.self="!busy && emit('close')">
      <section
        ref="panel"
        class="modal"
        :class="{ wide }"
        role="dialog"
        aria-modal="true"
        :aria-label="title"
        tabindex="-1"
      >
        <header class="modal-heading">
          <h2>{{ title }}</h2>
          <button
            class="icon-button"
            aria-label="关闭弹窗"
            :disabled="busy"
            @click="emit('close')"
          >
            <X :size="20" />
          </button>
        </header>
        <slot />
      </section>
    </div>
  </Teleport>
</template>
