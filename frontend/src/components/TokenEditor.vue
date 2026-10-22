<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import Modal from "./Modal.vue";
import SelectField from "./SelectField.vue";
import ExpirationField from "./ExpirationField.vue";
import { formatLocalDateTime } from "../dateTime";
import { resolveExpiration } from "../expiration";
import { api } from "../api";
import type {
  Album,
  APIToken,
  TokenAuthorization,
  TokenPermission,
} from "../types";
import { tokenPermissions } from "../tokenPermissions";
import { copy, message } from "../utils";
const props = defineProps<{ token?: APIToken | null; admin?: boolean }>();
const emit = defineEmits<{
  close: [];
  saved: [];
  notice: [text: string, error?: boolean];
}>();
const name = ref(props.token?.name || ""),
  mode = ref(props.token?.authorization.all_albums ? "all" : "selected"),
  permissions = ref<TokenPermission[]>([
    ...(props.token?.authorization.permissions || ["upload"]),
  ]),
  grants = ref<TokenAuthorization["albums"]>(
    (props.token?.authorization.albums || []).map((grant) => ({
      album_id: grant.album_id,
      permissions: [...grant.permissions],
    })),
  );
const period = ref(
    props.token ? (props.token.expires_at ? "custom" : "permanent") : "90",
  ),
  expires = ref(""),
  albums = ref<Album[]>([]),
  loading = ref(true),
  busy = ref(false),
  error = ref(""),
  newToken = ref("");
if (props.token?.expires_at) {
  const date = new Date(props.token.expires_at);
  expires.value = formatLocalDateTime(date);
}
const choices = computed(() => [
  ...albums.value.map((v) => ({ id: v.id, name: v.name })),
  ...grants.value
    .filter((g) => !albums.value.some((a) => a.id === g.album_id))
    .map((g) => ({
      id: g.album_id,
      name: `已删除相册 · ${g.album_id.slice(0, 8)}`,
    })),
]);
function selected(id: string) {
  return grants.value.find((v) => v.album_id === id);
}
function toggle(id: string) {
  if (selected(id))
    grants.value = grants.value.filter((v) => v.album_id !== id);
  else grants.value.push({ album_id: id, permissions: ["upload"] });
}
async function load() {
  loading.value = true;
  error.value = "";
  try {
    albums.value = await api<Album[]>(
      props.admin && props.token
        ? `/albums?scope=all&user_id=${encodeURIComponent(props.token.owner_id)}`
        : "/albums",
    );
    if (!props.token) {
      const album = albums.value.find((v) => v.is_default);
      if (album)
        grants.value = [{ album_id: album.id, permissions: ["upload"] }];
    }
  } catch (e) {
    error.value = message(e);
  } finally {
    loading.value = false;
  }
}
async function save() {
  error.value = "";
  let expiresAt: string | null;
  try {
    expiresAt = resolveExpiration(
      period.value,
      expires.value,
      props.token?.expires_at,
    );
  } catch (reason) {
    error.value = message(reason);
    return;
  }
  const authorization: TokenAuthorization =
    mode.value === "all"
      ? { all_albums: true, permissions: permissions.value, albums: [] }
      : { all_albums: false, permissions: [], albums: grants.value };
  if (
    authorization.all_albums
      ? !authorization.permissions.length
      : !authorization.albums.length ||
        authorization.albums.some((v) => !v.permissions.length)
  ) {
    error.value = "请选择相册，并为每个授权范围至少选择一种操作权限";
    return;
  }
  busy.value = true;
  try {
    const result = await api<{ token?: string }>(
      props.token ? `/tokens/${props.token.id}` : "/tokens",
      props.token ? "PUT" : "POST",
      {
        name: name.value,
        expires_at: expiresAt,
        authorization,
      },
    );
    emit("saved");
    emit(
      "notice",
      props.token ? "Token 权限已更新，立即生效" : "个人 Token 已创建",
    );
    if (result.token) newToken.value = result.token;
    else emit("close");
  } catch (e) {
    error.value = message(e);
  } finally {
    busy.value = false;
  }
}
async function copyToken() {
  try {
    await copy(newToken.value);
    emit("notice", "Token 已复制");
  } catch {
    emit("notice", "复制失败，请检查剪贴板权限", true);
  }
}
function close() {
  if (busy.value) return;
  newToken.value = "";
  emit("close");
}
onMounted(load);
</script>
<template>
  <Modal
    :title="token ? '编辑个人 Token' : '创建个人 Token'"
    wide
    :busy="busy"
    @close="close"
    ><div v-if="newToken" class="modal-body">
      <p>完整 Token 只显示这一次，请妥善保存。</p>
      <textarea
        :value="newToken"
        readonly
        rows="3"
        aria-label="新 Token"
      /><button class="button primary" @click="copyToken">复制 Token</button>
      <div class="modal-actions">
        <button class="button" @click="close">已妥善保存</button>
      </div>
    </div>
    <form v-else class="modal-body token-editor" @submit.prevent="save">
      <p v-if="token && admin" class="field-help">
        所属用户：{{ token.owner_name }}。仅能授权该用户自己的相册。
      </p>
      <label class="field-label" for="token-editor-name">名称</label
      ><input
        id="token-editor-name"
        v-model="name"
        maxlength="64"
        required
        :disabled="busy"
      />
      <ExpirationField
        id="token-editor"
        v-model="period"
        v-model:custom="expires"
        :original-value="token?.expires_at"
        :disabled="busy"
      />
      <label class="field-label">授权相册范围</label
      ><SelectField
        id="token-editor-mode"
        label="授权相册范围"
        v-model="mode"
        :options="[
          { value: 'selected', label: '指定相册，分别设置权限' },
          { value: 'all', label: '全部本人相册，统一权限' },
        ]"
      />
      <fieldset
        v-if="mode === 'all'"
        class="token-permission-options"
        :disabled="busy"
      >
        <legend>允许的操作</legend>
        <label
          v-for="option in tokenPermissions"
          :key="option.value"
          class="check-field"
          ><input
            v-model="permissions"
            type="checkbox"
            :value="option.value"
          />{{ option.label }}</label
        >
        <p class="field-help">
          包含以后新建的本人相册。只有同时授权“管理相册”时，Token
          才能创建新相册。
        </p>
      </fieldset>
      <template v-else
        ><p v-if="loading" role="status" class="muted">正在读取相册…</p>
        <div
          v-else
          class="management-list token-grants"
          role="region"
          aria-label="相册操作权限，可横向滚动"
          tabindex="0"
        >
          <table class="management-table">
            <thead>
              <tr>
                <th>相册</th>
                <th v-for="option in tokenPermissions" :key="option.value">
                  {{ option.label }}
                </th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="album in choices" :key="album.id">
                <th>
                  <label class="check-field"
                    ><input
                      type="checkbox"
                      :checked="!!selected(album.id)"
                      :aria-label="`授权相册 ${album.name}`"
                      :disabled="busy"
                      @change="toggle(album.id)"
                    /><span class="truncate" :title="album.name">{{
                      album.name
                    }}</span></label
                  >
                </th>
                <td v-for="option in tokenPermissions" :key="option.value">
                  <input
                    v-if="selected(album.id)"
                    v-model="selected(album.id)!.permissions"
                    type="checkbox"
                    :value="option.value"
                    :aria-label="`${album.name} · ${option.label}`"
                    :disabled="busy"
                  /><input
                    v-else
                    type="checkbox"
                    disabled
                    :aria-label="`${album.name} · ${option.label}`"
                  />
                </td>
              </tr>
              <tr v-if="!choices.length">
                <td colspan="7">暂无相册，请先在个人图库新建。</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p class="field-help">
          未勾选的相册不可通过此 Token
          访问。默认相册未获上传授权时，请在上传请求中指定已授权相册。
        </p></template
      >
      <p class="field-help">
        移动图片需要源相册的修改权限和目标相册的上传权限；管理相册包括访问设置，连同图片删除还需要删除图片权限。Token
        不支持系统管理、改密码或管理其他 Token。
      </p>
      <p v-if="error" class="form-error" role="alert">
        {{ error }}
        <button
          v-if="!albums.length"
          type="button"
          class="text-button"
          @click="load"
        >
          重新读取相册
        </button>
      </p>
      <div class="modal-actions">
        <button type="button" class="button" :disabled="busy" @click="close">
          取消</button
        ><button class="button primary" :disabled="busy || loading">
          {{ busy ? "正在保存…" : token ? "保存修改" : "创建 Token" }}
        </button>
      </div>
    </form></Modal
  >
</template>
