<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import {
  UploadCloud,
  ImagePlus,
  Check,
  X,
  RotateCcw,
  Copy,
  LoaderCircle,
  Folder,
} from "@lucide/vue";
import { uploadImage } from "../api";
import SelectField from "./SelectField.vue";
import QualitySlider from "./QualitySlider.vue";
import type { Album, ImageItem, StorageProfile } from "../types";
import { bytes, copy, imageLink, message, type LinkFormat } from "../utils";

const props = defineProps<{
  albums: Album[];
  initialAlbum?: string;
  maxBytes: number;
  storages: Pick<StorageProfile, "id" | "name" | "enabled">[];
  defaultStorage: string;
  defaultCompression: string;
  defaultQuality: number;
}>();
const emit = defineEmits<{
  uploaded: [];
  notice: [text: string, error?: boolean];
}>();
interface UploadItem {
  id: number;
  file: File;
  preview: string;
  album: string;
  storage: string;
  compression: string;
  quality: number;
  state: "waiting" | "uploading" | "done" | "error";
  progress: number;
  error?: string;
  image?: ImageItem;
}
const queue = ref<UploadItem[]>([]);
const input = ref<HTMLInputElement>();
const album = ref(props.initialAlbum || "");
watch(
  () => props.initialAlbum,
  (value) => {
    album.value = value || "";
  },
);
const storage = ref("");
const compression = ref("default");
const quality = ref(props.defaultQuality);
let qualityEdited = false;
watch(
  () => props.defaultQuality,
  (value) => {
    if (!qualityEdited) quality.value = value;
  },
);
function setQuality(value: number) {
  qualityEdited = true;
  quality.value = value;
}
const modeNames: Record<string, string> = {
  none: "保留原文件",
  preserve: "原格式压缩",
  webp: "转换为 WebP",
};
const compressionOptions = computed(() => [
  {
    value: "default",
    label: `跟随默认 · ${modeNames[props.defaultCompression]}`,
  },
  ...Object.entries(modeNames).map(([value, label]) => ({ value, label })),
]);
const selectedMode = computed(() =>
  compression.value === "default"
    ? props.defaultCompression
    : compression.value,
);
const storageOptions = computed(() => [
  { value: "", label: "默认存储" },
  ...enabledStorages.value.map((row) => ({ value: row.id, label: row.name })),
]);
const albumOptions = computed(() => [
  { value: "", label: "使用我的默认相册" },
  ...props.albums.map((row) => ({ value: row.id, label: row.name })),
]);
const enabledStorages = computed(() =>
  props.storages.filter((row) => row.enabled),
);
const dragging = ref(false);
let nextID = 0;
let workers = 0;
const done = computed(() =>
  queue.value.filter((item) => item.state === "done"),
);
const active = computed(() =>
  queue.value.some(
    (item) => item.state === "uploading" || item.state === "waiting",
  ),
);

function addFiles(files: File[] | FileList) {
  let rejected = 0;
  for (const file of Array.from(files)) {
    if (
      queue.value.length >= 100 ||
      file.size > props.maxBytes ||
      !/\.(jpe?g|png|gif|webp)$/i.test(file.name)
    ) {
      rejected++;
      continue;
    }
    queue.value.push({
      id: nextID++,
      file,
      preview: URL.createObjectURL(file),
      album: album.value,
      storage: storage.value || props.defaultStorage,
      compression:
        compression.value === "default"
          ? props.defaultCompression
          : compression.value,
      quality: quality.value,
      state: "waiting",
      progress: 0,
    });
  }
  if (rejected)
    emit(
      "notice",
      `${rejected} 个文件未加入：请检查格式、大小或队列上限（100 张）`,
      true,
    );
  run();
}

function run() {
  // 单个队列最多两个上传并发，失败项目由用户重试，避免自动重复提交。
  while (workers < 2) {
    const item = queue.value.find((item) => item.state === "waiting");
    if (!item) return;
    item.state = "uploading";
    workers++;
    uploadImage(
      item.file,
      item.album,
      (value) => {
        item.progress = value;
      },
      {
        storage_id: item.storage,
        compression: item.compression,
        quality: item.quality,
      },
    )
      .then((image) => {
        item.image = image;
        item.state = "done";
        emit("uploaded");
      })
      .catch((error) => {
        item.state = "error";
        item.error = message(error);
      })
      .finally(() => {
        workers--;
        run();
      });
  }
}
function retry(item: UploadItem) {
  item.state = "waiting";
  item.progress = 0;
  item.error = "";
  run();
}
function remove(item: UploadItem) {
  if (item.state === "uploading") return;
  URL.revokeObjectURL(item.preview);
  queue.value = queue.value.filter((row) => row.id !== item.id);
}
function clear() {
  for (const item of [...queue.value]) {
    if (item.state !== "uploading" && item.state !== "waiting") remove(item);
  }
}
async function copyLinks(items: UploadItem[], format: LinkFormat) {
  try {
    await copy(items.map((item) => imageLink(item.image!, format)).join("\n"));
    emit(
      "notice",
      items.length === 1
        ? "图片链接已复制"
        : `已复制 ${items.length} 张图片链接，每行一条`,
    );
  } catch {
    emit("notice", "复制失败，请检查浏览器剪贴板权限", true);
  }
}
function selectFiles(event: Event) {
  const target = event.target as HTMLInputElement;
  if (target.files) addFiles(target.files);
  target.value = "";
}
onBeforeUnmount(() => {
  for (const item of queue.value) URL.revokeObjectURL(item.preview);
});
defineExpose({ addFiles });
</script>

<template>
  <div class="upload-layout">
    <div class="upload-main">
      <div
        class="dropzone"
        :class="{ dragging }"
        @dragover.prevent="dragging = true"
        @dragleave.prevent="dragging = false"
        @drop.prevent.stop="
          dragging = false;
          addFiles($event.dataTransfer!.files);
        "
      >
        <div class="upload-symbol">
          <UploadCloud :size="32" :stroke-width="1.6" />
        </div>
        <h2>把图片放在这里</h2>
        <p>拖拽图片，或按 Ctrl / ⌘ + V 粘贴剪贴板图片</p>
        <button class="button primary" @click="input?.click()">
          <ImagePlus :size="17" />选择图片
        </button>
        <span class="dropzone-hint"
          >JPG、PNG、GIF、静态 WebP · 单张不超过 {{ bytes(maxBytes) }}</span
        >
        <input
          ref="input"
          type="file"
          accept="image/jpeg,image/png,image/gif,image/webp"
          multiple
          hidden
          @change="selectFiles"
        />
      </div>
      <div v-if="queue.length" class="panel queue-panel">
        <div class="section-heading">
          <h3>
            上传队列
            <span class="muted">{{ done.length }} / {{ queue.length }}</span>
          </h3>
          <button class="text-button" @click="clear">清除已结束</button>
        </div>
        <div v-for="item in queue" :key="item.id" class="queue-row">
          <img :src="item.preview" alt="" class="queue-preview" />
          <div class="queue-details">
            <strong>{{ item.file.name }}</strong
            ><span v-if="item.state === 'error'" class="error-text">{{
              item.error
            }}</span
            ><span v-else
              >{{ bytes(item.file.size) }} ·
              {{
                item.state === "done"
                  ? "上传完成"
                  : item.state === "waiting"
                    ? "等待上传"
                    : item.progress === 100
                      ? "正在处理图片…"
                      : `上传中 ${item.progress}%`
              }}</span
            >
            <span
              >{{ modeNames[item.compression]
              }}{{
                item.compression !== "none" ? ` · 画质 ${item.quality}` : ""
              }}</span
            >
            <span v-if="item.image?.optimized" class="success-text"
              >已压缩为 {{ bytes(item.image.size) }}，节省
              {{
                Math.round(
                  (1 - item.image.size / item.image.original_size) * 100,
                )
              }}%</span
            >
            <span v-else-if="item.image && item.compression !== 'none'">{{
              item.image.compression_status === "unsupported" ||
              item.image.compression_status === "metadata"
                ? "此图片保留原文件"
                : item.image.compression_status === "pixel_limit"
                  ? "超过 400 万像素，已保留原文件"
                  : item.image.compression_status === "failed"
                    ? "压缩失败，已保留原文件"
                    : "体积未减小，已保留原文件"
            }}</span>
            <div v-if="item.state === 'uploading'" class="progress-track">
              <div :style="{ width: `${item.progress}%` }" />
            </div>
            <button
              v-if="item.state === 'done' && item.image"
              type="button"
              class="text-button queue-copy"
              :aria-label="`复制 ${item.file.name} 直链`"
              @click="copyLinks([item], 'url')"
            >
              <Copy :size="14" />复制直链
            </button>
          </div>
          <Check
            v-if="item.state === 'done'"
            :size="18"
            class="success-text"
          /><LoaderCircle
            v-if="item.state === 'uploading'"
            :size="18"
            class="spin muted"
          />
          <button
            v-if="item.state === 'error'"
            class="icon-button"
            aria-label="重试上传"
            @click="retry(item)"
          >
            <RotateCcw :size="17" />
          </button>
          <button
            v-if="item.state !== 'uploading' && item.state !== 'waiting'"
            class="icon-button"
            aria-label="移除队列记录"
            @click="remove(item)"
          >
            <X :size="17" />
          </button>
        </div>
      </div>
    </div>
    <aside class="upload-aside">
      <div class="panel">
        <h3>上传选项</h3>
        <label class="field-label" for="upload-storage">存储位置</label>
        <SelectField
          id="upload-storage"
          v-model="storage"
          label="存储位置"
          :options="storageOptions"
        />
        <label class="field-label" for="upload-compression">图片处理</label>
        <SelectField
          id="upload-compression"
          v-model="compression"
          label="图片处理"
          :options="compressionOptions"
        />
        <QualitySlider
          id="upload-quality"
          :model-value="quality"
          :disabled="selectedMode === 'none'"
          @update:model-value="setQuality"
        />
        <p class="field-help">
          {{
            selectedMode === "none"
              ? "保留原文件时无需调整画质。"
              : "数值越高画质越好，不代表体积缩减百分比。"
          }}
        </p>
        <p class="field-help">选项只作用于之后加入队列的图片。</p>
      </div>
      <div class="panel">
        <h3><Folder :size="17" />存入相册</h3>
        <label class="field-label" for="upload-album">新加入的图片将存入</label
        ><SelectField
          id="upload-album"
          v-model="album"
          label="存入相册"
          :options="albumOptions"
        />
        <p class="field-help">之后也可以在图片详情里移动相册。</p>
      </div>
      <div class="panel upload-notes">
        <span class="eyebrow">YOUR IMAGES, YOUR CHOICE</span>
        <h3>按你的方式保存。</h3>
        <p>
          可选择原格式压缩或转换为
          WebP，保留透明度和图片尺寸。仅采用更小的结果，成功后不另存原文件。
        </p>
        <div class="note-divider" />
        <p>私有图片需要登录授权；对外访问请设置相册访客权限或创建分享链接。</p>
      </div>
      <div v-if="done.length" class="panel">
        <h3>批量复制（{{ done.length }} 张）</h3>
        <button class="button full" @click="copyLinks(done, 'url')">
          <Copy :size="16" />复制全部直链</button
        ><button class="button full" @click="copyLinks(done, 'markdown')">
          复制全部 Markdown</button
        ><button class="button full" @click="copyLinks(done, 'html')">
          复制全部 HTML
        </button>
        <p class="field-help">每张图片一行；单张复制请使用队列中的按钮。</p>
      </div>
      <p v-if="active" class="field-help">上传完成前请保持页面打开。</p>
    </aside>
  </div>
</template>
