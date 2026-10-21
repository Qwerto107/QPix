<script setup lang="ts">
defineProps<{ id: string; modelValue: number; disabled?: boolean }>();
const emit = defineEmits<{ "update:modelValue": [value: number] }>();
</script>

<template>
  <div class="quality-heading">
    <label class="field-label" :for="id">画质</label>
    <output :for="id">{{ modelValue }}<span> / 100</span></output>
  </div>
  <input
    :id="id"
    class="quality-slider"
    type="range"
    min="1"
    max="100"
    step="1"
    :value="modelValue"
    :disabled="disabled"
    :style="{ '--quality-progress': `${((modelValue - 1) / 99) * 100}%` }"
    @input="
      emit(
        'update:modelValue',
        Number(($event.target as HTMLInputElement).value),
      )
    "
  />
  <div class="quality-scale"><span>体积优先</span><span>画质优先</span></div>
</template>
