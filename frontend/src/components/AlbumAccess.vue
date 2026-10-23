<script setup lang="ts">
import { ref } from "vue";
import { Copy } from "@lucide/vue";
import Modal from "./Modal.vue";
import SelectField from "./SelectField.vue";
import { api } from "../api";
import type { Album } from "../types";
import { copy, message } from "../utils";
const props = defineProps<{ album: Album }>();
const emit = defineEmits<{
  close: [];
  saved: [];
  notice: [text: string, error?: boolean];
}>();
const guest = ref(props.album.guest_visible),
  mode = ref(props.album.referer_mode || "inherit"),
  refs = ref(props.album.referers.join("\n")),
  empty = ref(props.album.allow_empty_referer),
  busy = ref(false),
  error = ref("");
async function copyAlbumID() {
  try {
    await copy(props.album.id);
    emit("notice", "相册 ID 已复制");
  } catch {
    emit("notice", "复制失败，请检查剪贴板权限", true);
  }
}
async function save() {
  busy.value = true;
  error.value = "";
  try {
    await api(`/albums/${props.album.id}/access`, "PUT", {
      guest_visible: guest.value,
      referer_mode: mode.value,
      referers: refs.value
        .split(/\r?\n/)
        .map((v) => v.trim())
        .filter(Boolean),
      allow_empty_referer: empty.value,
    });
    emit("saved");
    emit("notice", "相册访问设置已保存");
    emit("close");
  } catch (e) {
    error.value = message(e);
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <Modal :title="`访问设置 · ${album.name}`" :busy="busy" @close="emit('close')"
    ><form class="modal-body" @submit.prevent="save">
      <label class="field-label" for="album-id">相册 ID</label>
      <div class="album-id-field">
        <input id="album-id" :value="album.id" readonly spellcheck="false" />
        <button type="button" class="button" @click="copyAlbumID">
          <Copy :size="15" />复制
        </button>
      </div>
      <p class="field-help">调用 API 时填入 <code>album_id</code> 参数。</p>
      <label class="check-field"
        ><input
          v-model="guest"
          type="checkbox"
        />允许访客通过图片直链访问</label
      >
      <p class="field-help">
        关闭后，访客不能访问图片直链。单张图片的有效分享链接仍可授予独立访问权限。
      </p>
      <label class="field-label">Referer 策略</label
      ><SelectField
        id="album-referer"
        label="Referer 策略"
        v-model="mode"
        :options="[
          { value: 'inherit', label: '继承全局规则' },
          { value: 'custom', label: '自定义域名白名单' },
          { value: 'unrestricted', label: '不限制 Referer' },
        ]"
      />
      <template v-if="mode === 'custom'"
        ><label class="field-label" for="album-referer-domains"
          >允许的域名</label
        ><textarea
          id="album-referer-domains"
          v-model="refs"
          rows="5"
          placeholder="example.com&#10;*.example.com"
          required
        />
        <p class="field-help">
          每行一个域名，不含协议或路径。通配子域名不包含主域名。
        </p>
        <label class="check-field"
          ><input v-model="empty" type="checkbox" />允许空
          Referer（直接打开链接等请求）</label
        ></template
      >
      <p class="field-help">
        仅对原图、预览图和缩略图直链生效，全局图片 IP
        白名单始终有效。分享通过密码、有效期和撤销状态控制。
      </p>
      <p v-if="error" class="form-error" role="alert">{{ error }}</p>
      <div class="modal-actions">
        <button
          type="button"
          class="button"
          :disabled="busy"
          @click="emit('close')"
        >
          取消</button
        ><button class="button primary" :disabled="busy">
          {{ busy ? "正在保存…" : "保存设置" }}
        </button>
      </div>
    </form></Modal
  >
</template>

<style scoped>
.album-id-field {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
}
.album-id-field input {
  min-width: 0;
  font-family: "Cascadia Code", Consolas, monospace;
  font-size: 12px;
}
</style>
