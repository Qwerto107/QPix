<script setup lang="ts">
import { ref } from "vue";
import { Copy } from "@lucide/vue";
import Modal from "./Modal.vue";
import ExpirationField from "./ExpirationField.vue";
import { api } from "../api";
import { resolveExpiration } from "../expiration";
import { absolute, copy, message } from "../utils";
import type { ShareItem } from "../types";

const props = defineProps<{ target: { id: string; name: string } }>();
const emit = defineEmits<{
  close: [];
  notice: [text: string, error?: boolean];
}>();
const period = ref("permanent");
const expires = ref("");
const password = ref("");
const busy = ref(false);
const error = ref("");
const created = ref<ShareItem | null>(null);

async function save() {
  if (busy.value || created.value) return;
  busy.value = true;
  error.value = "";
  try {
    // 目标仅来自图库中的当前图片，不再请求图片列表或提供重新选择入口。
    created.value = await api<ShareItem>("/shares", "POST", {
      kind: "image",
      target_id: props.target.id,
      password: password.value,
      expires_at: resolveExpiration(period.value, expires.value),
    });
    password.value = "";
    emit("notice", "图片分享链接已创建");
  } catch (reason) {
    error.value = message(reason);
  } finally {
    busy.value = false;
  }
}
async function copyLink() {
  if (!created.value) return;
  try {
    await copy(absolute(created.value.url));
    emit("notice", "分享链接已复制");
  } catch {
    emit("notice", "复制失败，请手动复制链接", true);
  }
}
</script>

<template>
  <Modal
    :title="created ? '分享链接已创建' : '创建图片分享'"
    :busy="busy"
    @close="emit('close')"
  >
    <div class="modal-body share-editor">
      <div class="share-target">
        <span class="field-label">分享图片</span>
        <p class="share-target-name">{{ target.name }}</p>
      </div>
      <template v-if="created">
        <label class="field-label" for="created-share-url">分享链接</label>
        <div class="share-result">
          <input
            id="created-share-url"
            :value="absolute(created.url)"
            readonly
          />
          <button type="button" class="button" @click="copyLink">
            <Copy :size="14" />复制链接
          </button>
        </div>
        <p class="field-help">可在“我的分享”中查看、撤销此链接。</p>
        <div class="modal-actions">
          <button class="button primary" @click="emit('close')">完成</button>
        </div>
      </template>
      <form v-else @submit.prevent="save">
        <label class="field-label" for="share-password">访问密码（可选）</label>
        <input
          id="share-password"
          v-model="password"
          type="password"
          autocomplete="new-password"
          maxlength="72"
          :disabled="busy"
        />
        <ExpirationField
          id="share-editor"
          v-model="period"
          v-model:custom="expires"
          :disabled="busy"
        />
        <p v-if="error" class="form-error" role="alert">{{ error }}</p>
        <div class="modal-actions">
          <button
            type="button"
            class="button"
            :disabled="busy"
            @click="emit('close')"
          >
            取消
          </button>
          <button class="button primary" :disabled="busy">
            {{ busy ? "正在创建…" : "创建分享" }}
          </button>
        </div>
      </form>
    </div>
  </Modal>
</template>

<style scoped>
.share-editor {
  padding: 18px 22px;
}
.share-editor .field-label,
.share-editor :deep(.expiration-field .field-label) {
  margin: 14px 0 7px;
}
.share-target .field-label {
  margin-top: 0;
}
.share-editor :deep(.expiration-field:not(.is-custom) .form-two-columns) {
  grid-template-columns: 1fr;
}
.share-editor :deep(.expiration-field .select-field) {
  max-width: none;
}
.share-editor .modal-actions {
  margin-top: 18px;
}
.share-target-name {
  margin: 6px 0 0;
  overflow-wrap: anywhere;
  font-size: 12px;
  font-weight: 550;
  color: var(--ink);
}
.share-result {
  display: flex;
  align-items: center;
  gap: 8px;
}
.share-result input {
  flex: 1;
  min-width: 0;
}
.share-result button {
  flex-shrink: 0;
}
@media (max-width: 480px) {
  .share-result {
    flex-wrap: wrap;
  }
  .share-result input {
    flex-basis: 100%;
  }
}
</style>
