<script setup lang="ts">
import { onMounted, ref } from "vue";
import { HardDrive, Plus, Cloud, LoaderCircle } from "@lucide/vue";
import Modal from "./Modal.vue";
import { api } from "../api";
import type { StorageProfile } from "../types";
import { message } from "../utils";
const emit = defineEmits<{
  changed: [];
  notice: [text: string, error?: boolean];
}>();
const rows = ref<StorageProfile[]>([]);
const loading = ref(true);
const error = ref("");
const action = ref("");
const editing = ref<StorageProfile | null>(null);
const showForm = ref(false);
const saving = ref(false);
const formError = ref("");
const deleting = ref<StorageProfile | null>(null);
const empty = () => ({
  name: "",
  kind: "local" as "local" | "s3",
  enabled: true,
  root: "",
  endpoint: "",
  region: "us-east-1",
  bucket: "",
  prefix: "",
  path_style: true,
  access_key: "",
  secret_key: "",
});
const form = ref(empty());
async function load() {
  loading.value = true;
  error.value = "";
  try {
    rows.value = await api<StorageProfile[]>("/storages");
  } catch (reason) {
    error.value = message(reason);
  } finally {
    loading.value = false;
  }
}
function edit(row?: StorageProfile) {
  editing.value = row || null;
  form.value = row
    ? { ...empty(), ...row, access_key: "", secret_key: "" }
    : empty();
  formError.value = "";
  showForm.value = true;
}
async function save() {
  saving.value = true;
  formError.value = "";
  try {
    // 显式构造请求，避免把视图状态或只读字段作为配置提交。
    const {
      name,
      kind,
      enabled,
      root,
      endpoint,
      region,
      bucket,
      prefix,
      path_style,
      access_key,
      secret_key,
    } = form.value;
    await api(
      editing.value ? `/storages/${editing.value.id}` : "/storages",
      editing.value ? "PUT" : "POST",
      {
        name,
        kind,
        enabled,
        root,
        endpoint,
        region,
        bucket,
        prefix,
        path_style,
        access_key,
        secret_key,
      },
    );
    showForm.value = false;
    form.value = empty();
    await load();
    emit("changed");
    emit("notice", "存储配置已保存");
  } catch (reason) {
    formError.value = message(reason);
  } finally {
    saving.value = false;
  }
}
async function perform(
  row: StorageProfile,
  operation: "default" | "test" | "delete",
) {
  action.value = `${row.id}-${operation}`;
  try {
    await api(
      `/storages/${row.id}${operation === "delete" ? "" : `/${operation}`}`,
      operation === "delete" ? "DELETE" : "POST",
    );
    deleting.value = null;
    if (operation !== "test") {
      await load();
      emit("changed");
    }
    emit(
      "notice",
      operation === "test"
        ? "读、写、删除测试通过"
        : operation === "default"
          ? "默认存储已更新"
          : "存储配置已删除",
    );
  } catch (reason) {
    emit("notice", message(reason), true);
  } finally {
    action.value = "";
  }
}
onMounted(load);
</script>
<template>
  <section aria-label="存储管理">
    <div class="section-heading storage-heading">
      <p class="muted">
        切换默认存储只影响新上传。停用后，已有图片仍可读取和删除。
      </p>
      <button class="button primary" @click="edit()">
        <Plus :size="17" />添加存储
      </button>
    </div>
    <p v-if="loading" class="muted">正在读取存储…</p>
    <p v-if="error" class="form-error" role="alert">
      {{ error }} <button class="text-button" @click="load">重试</button>
    </p>
    <div
      v-if="rows.length"
      class="management-list"
      role="region"
      aria-label="存储表格，可横向滚动"
      tabindex="0"
    >
      <table class="management-table" aria-label="存储列表">
        <colgroup>
          <col style="width: 18%" />
          <col style="width: 12%" />
          <col style="width: 13%" />
          <col style="width: 25%" />
          <col style="width: 8%" />
          <col style="width: 24%" />
        </colgroup>
        <thead>
          <tr>
            <th scope="col">存储名称</th>
            <th scope="col">类型</th>
            <th scope="col">状态</th>
            <th scope="col">存储位置</th>
            <th scope="col">图片数</th>
            <th scope="col">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in rows" :key="row.id">
            <th scope="row">
              <span class="management-name-with-icon">
                <component
                  :is="row.kind === 'local' ? HardDrive : Cloud"
                  :size="19"
                /><span class="management-name" :title="row.name">{{
                  row.name
                }}</span></span
              >
            </th>
            <td>{{ row.kind === "local" ? "本地磁盘" : "S3 兼容" }}</td>
            <td>
              <span class="tag" :class="{ 'is-muted': !row.enabled }">{{
                row.enabled ? "已启用" : "已停用"
              }}</span
              ><span v-if="row.is_default" class="management-secondary"
                >默认存储</span
              >
            </td>
            <td class="management-location">
              {{
                row.kind === "local"
                  ? row.root
                  : `${row.endpoint || "AWS S3"} / ${row.bucket}${row.prefix ? ` / ${row.prefix}` : ""}`
              }}
            </td>
            <td class="management-number">{{ row.images }}</td>
            <td>
              <div class="management-actions">
                <button
                  class="button compact"
                  :disabled="!!action"
                  @click="edit(row)"
                >
                  编辑
                </button>
                <button
                  class="button compact"
                  :disabled="!!action"
                  @click="perform(row, 'test')"
                >
                  <LoaderCircle
                    v-if="action === `${row.id}-test`"
                    :size="14"
                    class="spin"
                  />测试连接
                </button>
                <button
                  v-if="!row.is_default && row.enabled"
                  class="text-button"
                  :disabled="!!action"
                  @click="perform(row, 'default')"
                >
                  设为默认
                </button>
                <button
                  v-if="!row.is_default && row.id !== 'local'"
                  class="text-button danger-text"
                  :disabled="!!action || row.images > 0"
                  :title="row.images ? '仍有图片引用，不能删除' : ''"
                  @click="deleting = row"
                >
                  删除
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p class="field-help storage-footnote">
      S3 Bucket 请保持私有。图片通过 QPix
      网关访问，访问控制才会生效。已有图片引用的存储不能修改位置，请为新位置添加配置。
    </p>
    <Modal
      v-if="showForm"
      :title="editing ? '编辑存储' : '添加存储'"
      :busy="saving"
      @close="
        showForm = false;
        form = empty();
      "
    >
      <form class="storage-form" @submit.prevent="save">
        <label class="field-label" for="storage-name">名称</label
        ><input
          id="storage-name"
          v-model="form.name"
          maxlength="100"
          required
        />
        <label class="field-label" for="storage-kind">类型</label
        ><select
          id="storage-kind"
          v-model="form.kind"
          :disabled="!!editing?.images"
        >
          <option value="local">本地磁盘</option>
          <option value="s3">S3 兼容存储</option>
        </select>
        <fieldset class="storage-location-fields" :disabled="!!editing?.images">
          <template v-if="form.kind === 'local'"
            ><label class="field-label" for="storage-root">服务器目录</label
            ><input
              id="storage-root"
              v-model="form.root"
              placeholder="例如 /data/images"
              maxlength="2048"
              required
          /></template>
          <template v-else>
            <label class="field-label" for="storage-endpoint">Endpoint</label
            ><input
              id="storage-endpoint"
              v-model="form.endpoint"
              type="url"
              placeholder="留空使用 AWS S3"
              maxlength="2048"
            />
            <div class="form-two-columns">
              <div>
                <label class="field-label" for="storage-region">Region</label
                ><input
                  id="storage-region"
                  v-model="form.region"
                  maxlength="128"
                  required
                />
              </div>
              <div>
                <label class="field-label" for="storage-bucket">Bucket</label
                ><input
                  id="storage-bucket"
                  v-model="form.bucket"
                  maxlength="255"
                  required
                />
              </div>
            </div>
            <label class="field-label" for="storage-prefix">对象前缀</label
            ><input
              id="storage-prefix"
              v-model="form.prefix"
              placeholder="可选，例如 qpix"
              maxlength="512"
            />
            <label class="check-field"
              ><input v-model="form.path_style" type="checkbox" />使用 Path
              Style 寻址</label
            >
          </template>
        </fieldset>
        <template v-if="form.kind === 's3'">
          <label class="field-label" for="storage-access-key">Access Key</label
          ><input
            id="storage-access-key"
            v-model="form.access_key"
            type="password"
            autocomplete="off"
            :required="!editing?.has_credentials"
            maxlength="1024"
          />
          <label class="field-label" for="storage-secret-key">Secret Key</label
          ><input
            id="storage-secret-key"
            v-model="form.secret_key"
            type="password"
            autocomplete="new-password"
            :required="!editing?.has_credentials"
            maxlength="4096"
          />
          <p class="field-help">
            {{
              editing?.has_credentials
                ? "凭据已加密保存，两项均留空即可保留。"
                : "凭据在服务端加密保存，不会通过接口返回。"
            }}
          </p>
        </template>
        <label class="check-field"
          ><input
            v-model="form.enabled"
            type="checkbox"
            :disabled="editing?.is_default"
          />启用新上传</label
        >
        <p v-if="editing?.images" class="field-help">
          此存储已有图片，位置已锁定。仍可修改名称、凭据或停用新上传。
        </p>
        <p v-if="formError" class="form-error" role="alert">{{ formError }}</p>
        <div class="modal-actions">
          <button
            class="button"
            type="button"
            :disabled="saving"
            @click="
              showForm = false;
              form = empty();
            "
          >
            取消</button
          ><button class="button primary" :disabled="saving">
            {{ saving ? "正在保存…" : "保存存储" }}
          </button>
        </div>
      </form>
    </Modal>
    <Modal
      v-if="deleting"
      title="删除存储配置"
      :busy="!!action"
      @close="deleting = null"
    >
      <p class="confirm-copy">
        确认删除「{{ deleting.name }}」？只移除配置，不清空磁盘或 Bucket。
      </p>
      <div class="modal-actions">
        <button class="button" :disabled="!!action" @click="deleting = null">
          取消</button
        ><button
          class="button danger"
          :disabled="!!action"
          @click="perform(deleting, 'delete')"
        >
          删除配置
        </button>
      </div>
    </Modal>
  </section>
</template>
