<script setup lang="ts">
import { ref } from "vue";
import { Copy, ExternalLink, Trash2, Check, ImageOff } from "@lucide/vue";
import Modal from "./Modal.vue";
import { api } from "../api";
import type { Album, ImageItem } from "../types";
import {
  absolute,
  bytes,
  copy,
  date,
  imageLink,
  message,
  type LinkFormat,
} from "../utils";

const props = defineProps<{
  image: ImageItem;
  albums: Album[];
  canShare?: boolean;
}>();
const emit = defineEmits<{
  close: [];
  updated: [];
  remove: [image: ImageItem];
  notice: [text: string, error?: boolean];
  share: [image: ImageItem];
}>();
const name = ref(props.image.name);
const album = ref(props.image.album_id || "");
const busy = ref(false);
const imageFailed = ref(false);
const error = ref("");
async function save() {
  if (busy.value) return;
  busy.value = true;
  error.value = "";
  try {
    await api(`/images/${props.image.id}`, "PATCH", {
      name: name.value,
      album_id: album.value,
    });
    emit("updated");
    emit("notice", "图片信息已保存");
    emit("close");
  } catch (reason) {
    error.value = message(reason);
  } finally {
    busy.value = false;
  }
}
async function copyLink(format: LinkFormat) {
  try {
    await copy(imageLink(props.image, format));
    emit("notice", "链接已复制");
  } catch {
    emit("notice", "复制失败，请检查浏览器剪贴板权限", true);
  }
}
</script>

<template>
  <Modal title="图片详情" wide :busy="busy" @close="!busy && emit('close')">
    <div class="detail-layout">
      <div class="detail-preview">
        <img
          v-if="!imageFailed"
          :src="image.url"
          :alt="image.name"
          @error="imageFailed = true"
        />
        <div v-else class="empty-state">
          <ImageOff :size="36" />
          <p>图片暂时无法加载</p>
        </div>
      </div>
      <div class="detail-fields">
        <span class="tag">{{ image.format.toUpperCase() }}</span>
        <form @submit.prevent="save">
          <label class="field-label" for="image-name">图片名称</label
          ><input
            id="image-name"
            v-model="name"
            maxlength="255"
            required
            :disabled="!image.can_manage"
          />
          <label class="field-label" for="image-album">所属相册</label
          ><select
            id="image-album"
            v-model="album"
            :disabled="!image.can_manage"
          >
            <option value="">未分类</option>
            <option
              v-for="item in albums.filter(
                (row) => row.can_upload || row.id === image.album_id,
              )"
              :key="item.id"
              :value="item.id"
            >
              {{ item.name }}
            </option>
          </select>
          <dl class="image-meta">
            <div>
              <dt>尺寸</dt>
              <dd>{{ image.width }} × {{ image.height }}</dd>
            </div>
            <div>
              <dt>保存大小</dt>
              <dd>{{ bytes(image.size) }}</dd>
            </div>
            <div v-if="image.optimized">
              <dt>上传大小</dt>
              <dd>
                {{ bytes(image.original_size) }} · 节省
                {{ Math.round((1 - image.size / image.original_size) * 100) }}%
              </dd>
            </div>
            <div>
              <dt>上传日期</dt>
              <dd>{{ date(image.created_at) }}</dd>
            </div>
          </dl>
          <p v-if="error" class="form-error" role="alert">{{ error }}</p>
          <button
            v-if="image.can_manage"
            class="button primary full"
            :disabled="busy"
          >
            <Check :size="16" />{{ busy ? "正在保存…" : "保存修改" }}
          </button>
        </form>
        <div class="detail-links">
          <button
            v-if="canShare"
            class="button full"
            @click="emit('share', image)"
          >
            创建分享链接
          </button>
          <button class="button full" @click="copyLink('url')">
            <Copy :size="16" />复制直链
          </button>
          <div class="button-pair">
            <button class="button" @click="copyLink('markdown')">
              Markdown</button
            ><button class="button" @click="copyLink('html')">HTML</button>
          </div>
          <a
            class="text-button"
            :href="absolute(image.url)"
            target="_blank"
            rel="noopener noreferrer"
            ><ExternalLink :size="15" />打开图片</a
          >
        </div>
        <button
          v-if="image.can_manage"
          class="text-button danger-text"
          :disabled="busy"
          @click="emit('remove', image)"
        >
          <Trash2 :size="15" />删除这张图片
        </button>
      </div>
    </div>
  </Modal>
</template>
