<script setup lang="ts">
import DateTimeField from "./DateTimeField.vue";
import SelectField from "./SelectField.vue";
import { expirationOptions } from "../expiration";
defineProps<{
  id: string;
  modelValue: string;
  custom: string;
  originalValue?: string | null;
  disabled?: boolean;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: string];
  "update:custom": [value: string];
}>();
</script>
<template>
  <fieldset
    class="expiration-field"
    :class="{ 'is-custom': modelValue === 'custom' }"
    :disabled="disabled"
    aria-label="有效期设置"
  >
    <div class="form-two-columns">
      <div>
        <label class="field-label" :for="`${id}-period`">有效期</label>
        <SelectField
          :id="`${id}-period`"
          label="有效期"
          :model-value="modelValue"
          :options="expirationOptions"
          @update:model-value="emit('update:modelValue', $event)"
        />
      </div>
      <div v-if="modelValue === 'custom'">
        <label class="field-label" :for="`${id}-expires`">到期时间</label>
        <DateTimeField
          :id="`${id}-expires`"
          label="到期时间"
          :model-value="custom"
          :original-value="originalValue || undefined"
          required
          :disabled="disabled"
          @update:model-value="emit('update:custom', $event)"
        />
      </div>
    </div>
    <p
      v-if="modelValue !== 'permanent' && modelValue !== 'custom'"
      class="field-help"
    >
      固定期限从本次保存时起计算。
    </p>
  </fieldset>
</template>
<style scoped>
.expiration-field {
  border: 0;
  padding: 0;
  margin: 0;
  min-width: 0;
}
@media (max-width: 600px) {
  .form-two-columns {
    grid-template-columns: 1fr;
    gap: 0;
  }
}
</style>
