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
        JPEG、PNG、静态 WebP、BMP、TIFF、ICO、AVIF、HEIC 和静态 JXL
        支持原格式压缩或转 WebP，按画质处理并保留透明度。最多处理 400 万像素，超过时保留原文件。
      </p>
      <p>
        动画、SVG、多页 TIFF、高位深及已识别的特殊色彩配置保留原文件。多尺寸 ICO
        原格式压缩保留所有尺寸，转 WebP 取最大尺寸。JPEG 转换时校正 EXIF 方向。
      </p>
      <div class="note-divider" />
      <p>
        压缩失败或体积没有变小，将保留原文件。压缩成功后不额外备份原文件；预览图和缩略图按需生成，已有图片不受影响。
      </p>
    </aside>
  </section>
</template>
