<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { Plus, Copy } from "@lucide/vue";
import Modal from "./Modal.vue";
import { api } from "../api";
import type { Album, ImageItem, ImagePage, ShareItem } from "../types";
import { absolute, copy, message } from "../utils";
const props = defineProps<{
  albums: Album[];
  target?: { kind: "album" | "image"; id: string; name: string } | null;
}>();
const emit = defineEmits<{ notice: [text: string, error?: boolean] }>();
const rows = ref<ShareItem[]>([]);
const open = ref(false);
const busy = ref(false);
const error = ref("");
const loading = ref(true);
const formError = ref("");
const kind = ref<"album" | "image">("album");
const targetID = ref("");
const password = ref("");
const expires = ref("");
const images = ref<ImageItem[]>([]);
const imageSearch = ref("");
const imageLoading = ref(false);
const imageError = ref("");
const selectedImage = ref<{ id: string; name: string } | null>(null);
let searchRequest = 0;
const choices = computed(() => {
  const result = images.value.map((row) => ({ id: row.id, name: row.name }));
  if (
    selectedImage.value &&
    !result.some((row) => row.id === selectedImage.value!.id)
  )
    result.unshift(selectedImage.value);
  return result;
});
async function load() {
  loading.value = true;
  error.value = "";
  try {
    rows.value = await api<ShareItem[]>("/shares");
  } catch (reason) {
    error.value = message(reason);
  } finally {
    loading.value = false;
  }
}
async function searchImages() {
  const request = ++searchRequest;
  imageLoading.value = true;
  imageError.value = "";
  try {
    const data = await api<ImagePage>(
      `/images?limit=100&q=${encodeURIComponent(imageSearch.value)}`,
    );
    if (request === searchRequest) images.value = data.items;
  } catch (reason) {
    if (request === searchRequest) imageError.value = message(reason);
  } finally {
    if (request === searchRequest) imageLoading.value = false;
  }
}
function create(target?: {
  kind: "album" | "image";
  id: string;
  name: string;
}) {
  kind.value = target?.kind || "album";
  targetID.value = target?.id || "";
  selectedImage.value = target?.kind === "image" ? target : null;
  password.value = "";
  expires.value = "";
  formError.value = "";
  imageSearch.value = "";
  open.value = true;
  if (kind.value === "image") void searchImages();
}
watch(kind, () => {
  if (open.value && kind.value === "image") void searchImages();
});
watch(
  () => props.target,
  (target) => {
    if (target) create(target);
  },
  { immediate: true },
);
async function save() {
  busy.value = true;
  formError.value = "";
  try {
    await api("/shares", "POST", {
      kind: kind.value,
      target_id: targetID.value,
      password: password.value,
      expires_at: expires.value ? new Date(expires.value).toISOString() : null,
    });
    open.value = false;
    password.value = "";
    await load();
    emit("notice", "分享链接已创建");
  } catch (reason) {
    formError.value = message(reason);
  } finally {
    busy.value = false;
  }
}
async function copyLink(row: ShareItem) {
  try {
    await copy(absolute(row.url));
    emit("notice", "分享链接已复制");
  } catch {
    emit("notice", "复制失败，请检查剪贴板权限", true);
  }
}
const revoking = ref<ShareItem | null>(null);
async function revoke() {
  if (!revoking.value) return;
  busy.value = true;
  try {
    await api(`/shares/${revoking.value.id}`, "DELETE");
    revoking.value = null;
    await load();
    emit("notice", "分享已撤销，已解锁的浏览器也不能继续访问");
  } catch (reason) {
    emit("notice", message(reason), true);
  } finally {
    busy.value = false;
  }
}
function status(row: ShareItem) {
  return row.revoked_at
    ? "已撤销"
    : row.expires_at && new Date(row.expires_at).getTime() <= Date.now()
      ? "已到期"
      : "有效";
}
onMounted(load);
</script>
<template>
  <section aria-label="分享管理">
    <div class="section-heading storage-heading">
      <p class="muted">
        分享授予指定内容的只读访问。可设置密码、到期时间并随时撤销。
      </p>
      <button class="button primary" @click="create()">
        <Plus :size="16" />创建分享
      </button>
    </div>
    <p v-if="loading" class="muted">正在读取分享…</p>
    <p v-if="error" class="form-error" role="alert">
      {{ error }} <button class="text-button" @click="load">重试</button>
    </p>
    <div
      v-if="rows.length"
      class="management-list"
      role="region"
      aria-label="分享表格，可横向滚动"
      tabindex="0"
    >
      <table class="management-table" aria-label="分享列表">
        <colgroup>
          <col style="width: 31%" />
          <col style="width: 13%" />
          <col style="width: 21%" />
          <col style="width: 11%" />
          <col style="width: 24%" />
        </colgroup>
        <thead>
          <tr>
            <th scope="col">分享内容</th>
            <th scope="col">访问方式</th>
            <th scope="col">到期时间</th>
            <th scope="col">状态</th>
            <th scope="col">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in rows" :key="row.id">
            <th scope="row">
              <span class="management-name" :title="row.title">{{
                row.title
              }}</span
              ><span class="management-secondary">{{
                row.kind === "album" ? "相册" : "图片"
              }}</span
              ><span
                class="management-secondary management-truncate"
                :title="absolute(row.url)"
                >{{ absolute(row.url) }}</span
              >
            </th>
            <td>{{ row.has_password ? "密码保护" : "无需密码" }}</td>
            <td>
              {{
                row.expires_at
                  ? new Date(row.expires_at).toLocaleString()
                  : "长期有效"
              }}
            </td>
            <td>
              <span
                class="tag"
                :class="{ 'is-muted': status(row) !== '有效' }"
                >{{ status(row) }}</span
              >
            </td>
            <td>
              <div class="management-actions">
                <button
                  class="button compact"
                  :disabled="status(row) !== '有效'"
                  @click="copyLink(row)"
                >
                  <Copy :size="14" />复制链接</button
                ><a
                  v-if="status(row) === '有效'"
                  class="text-button"
                  :href="row.url"
                  target="_blank"
                  rel="noopener noreferrer"
                  >打开分享</a
                ><button
                  v-if="!row.revoked_at"
                  class="text-button danger-text"
                  @click="revoking = row"
                >
                  撤销
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-if="!loading && !error && !rows.length" class="panel empty-state">
      <h3>还没有分享链接</h3>
      <p>创建相册或图片分享，方便亲友查看。</p>
    </div>
    <Modal
      v-if="open"
      title="创建分享"
      :busy="busy"
      @close="
        open = false;
        password = '';
      "
    >
      <form class="modal-body" @submit.prevent="save">
        <label class="field-label" for="share-kind">分享类型</label
        ><select
          id="share-kind"
          v-model="kind"
          @change="targetID = ''"
          :disabled="busy"
        >
          <option value="album">相册</option>
          <option value="image">单张图片</option>
        </select>
        <template v-if="kind === 'image'"
          ><div class="share-search">
            <label class="field-label" for="share-image-search"
              >搜索图片名称</label
            ><input id="share-image-search" v-model="imageSearch" /><button
              type="button"
              class="button compact"
              :disabled="imageLoading"
              @click="searchImages"
            >
              {{ imageLoading ? "搜索中…" : "搜索" }}
            </button>
          </div>
          <p v-if="imageError" class="form-error">{{ imageError }}</p></template
        >
        <label class="field-label" for="share-target">{{
          kind === "album" ? "选择相册" : "选择图片"
        }}</label
        ><select id="share-target" v-model="targetID" required>
          <option value="" disabled>请选择</option>
          <template v-if="kind === 'album'"
            ><option v-for="album in albums" :key="album.id" :value="album.id">
              {{ album.name }}
            </option></template
          ><template v-else
            ><option v-for="image in choices" :key="image.id" :value="image.id">
              {{ image.name }} · {{ image.id.slice(0, 8) }}
            </option></template
          >
        </select>
        <label class="field-label" for="share-password">访问密码（可选）</label
        ><input
          id="share-password"
          v-model="password"
          type="password"
          maxlength="72"
          autocomplete="new-password"
          placeholder="留空无需密码，最多 72 字节"
        />
        <label class="field-label" for="share-expires">到期时间（可选）</label
        ><input id="share-expires" v-model="expires" type="datetime-local" />
        <p class="field-help">
          使用当前设备的本地时间，留空长期有效。相册分享包含之后加入该相册的图片。
        </p>
        <p v-if="formError" class="form-error" role="alert">{{ formError }}</p>
        <div class="modal-actions">
          <button
            type="button"
            class="button"
            :disabled="busy"
            @click="
              open = false;
              password = '';
            "
          >
            取消</button
          ><button class="button primary" :disabled="busy || !targetID">
            {{ busy ? "正在创建…" : "创建链接" }}
          </button>
        </div>
      </form>
    </Modal>
    <Modal
      v-if="revoking"
      title="撤销分享"
      :busy="busy"
      @close="revoking = null"
      ><div class="modal-body">
        <p>
          撤销「{{
            revoking.title
          }}」的分享链接？已解锁的访问者也将失去访问权限。
        </p>
        <div class="modal-actions">
          <button class="button" :disabled="busy" @click="revoking = null">
            取消</button
          ><button class="button danger" :disabled="busy" @click="revoke">
            确认撤销
          </button>
        </div>
      </div></Modal
    >
  </section>
</template>
