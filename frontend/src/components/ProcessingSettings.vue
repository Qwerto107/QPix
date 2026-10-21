<script setup lang="ts">
import { onMounted, ref } from "vue";
import { api } from "../api";
import type { ProcessingSettings } from "../types";
import { message } from "../utils";
import SelectField from "./SelectField.vue";
import QualitySlider from "./QualitySlider.vue";
const emit = defineEmits<{
  changed: [];
  notice: [text: string, error?: boolean];
}>();
const form = ref<ProcessingSettings>({ mode: "none", quality: 82 });
const loading = ref(true);
const loaded = ref(false);
const busy = ref(false);
const error = ref("");
async function load() {
  loading.value = true;
  error.value = "";
  try {
    form.value = await api<ProcessingSettings>("/processing");
    loaded.value = true;
  } catch (reason) {
    error.value = message(reason);
  } finally {
    loading.value = false;
  }
}
async function save() {
  busy.value = true;
  error.value = "";
  try {
    form.value = await api<ProcessingSettings>(
      "/processing",
      "PUT",
      form.value,
    );
    emit("changed");
    emit("notice", "图片处理设置已保存");
  } catch (reason) {
    error.value = message(reason);
  } finally {
    busy.value = false;
  }
}
onMounted(load);
</script>
<template>
  <section class="settings-layout">
    <form class="panel settings-panel" @submit.prevent="save">
      <h3>上传压缩</h3>
      <p class="muted">
        设置新上传图片的默认处理方式，也可以在上传时单独选择。
      </p>
      <p v-if="loading" class="muted">正在读取设置…</p>
      <template v-else-if="loaded">
        <label class="field-label" for="processing-mode">默认模式</label>
        <SelectField
          id="processing-mode"
          :model-value="form.mode"
          label="默认模式"
          :options="[
            { value: 'none', label: '保留原文件' },
            { value: 'preserve', label: '原格式压缩' },
            { value: 'webp', label: '转换为 WebP' },
          ]"
          @update:model-value="form.mode = $event as ProcessingSettings['mode']"
        />
        <QualitySlider
          id="processing-quality"
          v-model="form.quality"
          :disabled="form.mode === 'none'"
        />
        <p class="field-help">
          建议 82。数值越高画质越好，不代表体积缩减百分比。PNG
          也按画质量化颜色，保留透明通道。
        </p>
        <div class="panel-actions">
          <button class="button primary" :disabled="busy">
            {{ busy ? "正在保存…" : "保存处理设置" }}
          </button>
        </div>
      </template>
      <p v-if="error" class="form-error" role="alert">
        {{ error }}
        <button
          v-if="loading === false"
          type="button"
          class="text-button"
          @click="load"
        >
          重新读取
        </button>
      </p>
    </form>
    <aside class="panel upload-notes">
      <span class="eyebrow">SMALLER, WHEN IT HELPS</span>
      <h3>只保存更小的结果</h3>
      <p>
        原格式压缩保留 JPEG、PNG、静态 WebP 的格式；也可统一转换为有损 WebP。
        两种方式均按所选画质处理并保留透明度。尺寸不缩放。为控制内存，最多处理
        400 万像素，超过时保留原文件。
      </p>
      <p>
        GIF、APNG、16 位 PNG 和含特殊色彩配置的图片保留原文件。JPEG 转换时校正
        EXIF 方向。
      </p>
      <div class="note-divider" />
      <p>
        压缩失败或体积没有变小，将保留原文件。压缩成功时仅保存处理后的图片及缩略图，不额外备份原文件；已有图片不受影响。
      </p>
    </aside>
  </section>
</template>
