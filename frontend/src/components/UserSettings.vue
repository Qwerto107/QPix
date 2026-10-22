<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { Plus } from "@lucide/vue";
import Modal from "./Modal.vue";
import SelectField from "./SelectField.vue";
import { api } from "../api";
import type { User } from "../types";
import {
  bytes,
  message,
  timestamp,
  preference,
  savePreference,
} from "../utils";
const props = defineProps<{ serverLimit: number; currentUserId: string }>();
const emit = defineEmits<{
  notice: [text: string, error?: boolean];
  changed: [];
}>();
const users = ref<User[]>([]);
const search = ref(""),
  statusFilter = ref(""),
  sortOrder = ref(preference("users.sort", "asc", ["asc", "desc"])),
  page = ref(1),
  pageLimit = ref(preference("users.limit", "20", ["20", "50", "100"]));
const filtered = computed(() =>
  users.value
    .filter(
      (row) =>
        row.username
          .toLocaleLowerCase()
          .includes(search.value.toLocaleLowerCase()) &&
        (!statusFilter.value ||
          (row.enabled ? "enabled" : "disabled") === statusFilter.value),
    )
    .sort((a, b) =>
      sortOrder.value === "asc"
        ? a.username.localeCompare(b.username, "zh-CN")
        : b.username.localeCompare(a.username, "zh-CN"),
    ),
);
const pages = computed(() =>
  Math.max(1, Math.ceil(filtered.value.length / Number(pageLimit.value))),
);
const displayed = computed(() =>
  filtered.value.slice(
    (page.value - 1) * Number(pageLimit.value),
    page.value * Number(pageLimit.value),
  ),
);
watch([search, statusFilter, sortOrder, pageLimit], () => {
  page.value = 1;
  savePreference("users.limit", pageLimit.value);
  savePreference("users.sort", sortOrder.value);
});
watch(pages, (value) => {
  if (page.value > value) page.value = value;
});
const loading = ref(true);
const error = ref("");
const open = ref(false);
const deleting = ref<User | null>(null);
const editing = ref<User | null>(null);
const busy = ref(false);
const formError = ref("");
const form = ref({
  username: "",
  role: "user",
  enabled: true,
  quota: 1,
  maxFile: 0,
  password: "",
  confirm: "",
});
async function load() {
  loading.value = true;
  error.value = "";
  try {
    users.value = await api<User[]>("/users");
  } catch (reason) {
    error.value = message(reason);
  } finally {
    loading.value = false;
  }
}
function edit(user?: User) {
  editing.value = user || null;
  form.value = {
    username: user?.username || "",
    role: user?.role || "user",
    enabled: user?.enabled ?? true,
    quota: user ? user.quota_bytes / 2 ** 30 : 1,
    maxFile: user
      ? Math.min(user.max_file_bytes, props.serverLimit) / 2 ** 20
      : 0,
    password: "",
    confirm: "",
  };
  formError.value = "";
  open.value = true;
}
async function save() {
  if (form.value.password !== form.value.confirm) {
    formError.value = "两次密码输入不一致";
    return;
  }
  busy.value = true;
  formError.value = "";
  try {
    await api(
      editing.value ? `/users/${editing.value.id}` : "/users",
      editing.value ? "PUT" : "POST",
      {
        username: form.value.username,
        role: form.value.role,
        enabled: form.value.enabled,
        quota_bytes: Math.round(form.value.quota * 2 ** 30),
        max_file_bytes: Math.round(form.value.maxFile * 2 ** 20),
        password: form.value.password,
        confirm_password: form.value.confirm,
      },
    );
    open.value = false;
    form.value.password = "";
    form.value.confirm = "";
    await load();
    emit("changed");
    emit("notice", "账号设置已保存");
  } catch (reason) {
    formError.value = message(reason);
  } finally {
    busy.value = false;
  }
}
async function removeUser() {
  if (!deleting.value) return;
  busy.value = true;
  formError.value = "";
  try {
    await api(`/users/${deleting.value.id}`, "DELETE");
    deleting.value = null;
    await load();
    emit("changed");
    emit("notice", "用户已删除");
  } catch (reason) {
    formError.value = message(reason);
  } finally {
    busy.value = false;
  }
}
onMounted(load);
</script>
<template>
  <section aria-label="用户管理">
    <div class="section-heading storage-heading">
      <p class="muted">
        注册关闭，由管理员创建账号。重置密码使登录会话失效，保留上传
        Token；停用账号会撤销其 Token。
      </p>
      <button class="button primary" @click="edit()">
        <Plus :size="16" />创建用户
      </button>
    </div>
    <div class="compact-toolbar">
      <input
        v-model="search"
        class="list-search"
        placeholder="搜索名称"
        aria-label="搜索名称"
      /><SelectField
        id="config-status"
        label="筛选状态"
        v-model="statusFilter"
        :options="[
          { value: '', label: '全部状态' },
          { value: 'enabled', label: '已启用' },
          { value: 'disabled', label: '已停用' },
        ]"
      /><SelectField
        id="config-sort"
        label="名称排序"
        v-model="sortOrder"
        :options="[
          { value: 'asc', label: '名称 ↑' },
          { value: 'desc', label: '名称 ↓' },
        ]"
      />
    </div>
    <p v-if="loading" class="muted">正在读取账号…</p>
    <p v-if="error" class="form-error" role="alert">
      {{ error }} <button class="text-button" @click="load">重试</button>
    </p>
    <div
      v-if="users.length"
      class="management-list"
      role="region"
      aria-label="用户表格，可横向滚动"
      tabindex="0"
    >
      <table class="management-table" aria-label="用户列表">
        <thead>
          <tr>
            <th scope="col">用户名</th>
            <th scope="col">角色</th>
            <th scope="col">状态</th>
            <th scope="col">容量用量 / 配额</th>
            <th scope="col">单张上限</th>
            <th scope="col">创建时间</th>
            <th scope="col" class="user-actions-column">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="user in displayed" :key="user.id">
            <th scope="row">
              <span class="management-name" :title="user.username">{{
                user.username
              }}</span>
            </th>
            <td>{{ user.role === "admin" ? "管理员" : "普通用户" }}</td>
            <td>
              <span class="tag" :class="{ 'is-muted': !user.enabled }">{{
                user.enabled ? "已启用" : "已停用"
              }}</span>
            </td>
            <td>
              <span class="management-number"
                >{{ bytes(user.used_bytes || 0) }} /
                {{
                  user.quota_bytes ? bytes(user.quota_bytes) : "不限容量"
                }}</span
              >
              <div
                v-if="user.quota_bytes"
                class="progress-track management-progress"
              >
                <div
                  :style="{
                    width: `${Math.min(100, ((user.used_bytes || 0) / user.quota_bytes) * 100)}%`,
                  }"
                />
              </div>
            </td>
            <td class="management-number">
              {{
                bytes(Math.min(user.max_file_bytes || serverLimit, serverLimit))
              }}
            </td>
            <td>{{ timestamp(user.created_at) }}</td>
            <td>
              <div class="management-actions user-actions">
                <button
                  class="button compact"
                  :aria-label="`管理 ${user.username} 的账号与配额`"
                  @click="edit(user)"
                >
                  管理账号与配额</button
                ><button
                  class="text-button danger-text"
                  :disabled="user.id === currentUserId"
                  :aria-label="`删除用户 ${user.username}`"
                  @click="
                    deleting = user;
                    formError = '';
                  "
                >
                  删除
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="!loading && !filtered.length" class="field-help">
      暂无符合条件的记录。
    </p>
    <div class="compact-pagination">
      <span>共 {{ filtered.length }} 条</span
      ><SelectField
        id="config-limit"
        label="每页数量"
        v-model="pageLimit"
        :options="
          ['20', '50', '100'].map((v) => ({ value: v, label: `${v} 条／页` }))
        "
      /><button class="button compact" :disabled="page <= 1" @click="page--">
        上一页</button
      ><span>{{ page }} / {{ pages }}</span
      ><button class="button compact" :disabled="page >= pages" @click="page++">
        下一页
      </button>
    </div>
    <p class="field-help storage-footnote">
      容量统计主文件保存大小，包含待删除项目；缩略图与数据库不计。上传先按原文件大小预占，完成后按实际保存大小结算。
    </p>
    <Modal
      v-if="open"
      :title="editing ? '管理账号' : '创建用户'"
      :busy="busy"
      @close="
        open = false;
        form.password = '';
        form.confirm = '';
      "
    >
      <form class="modal-body" @submit.prevent="save">
        <label class="field-label" for="account-name">用户名</label
        ><input
          id="account-name"
          v-model="form.username"
          required
          maxlength="64"
          :disabled="!!editing"
          pattern="[a-zA-Z0-9][a-zA-Z0-9_.\-]*"
          autocomplete="off"
        />
        <label class="field-label" for="account-role">角色</label>
        <p v-if="editing?.id === currentUserId" class="field-help">
          当前账号为管理员，不能更改自己的角色或停用自己。
        </p>
        <SelectField
          v-else
          id="account-role"
          label="角色"
          v-model="form.role"
          :options="[
            { value: 'user', label: '普通用户' },
            { value: 'admin', label: '管理员' },
          ]"
        />
        <p class="field-help">
          角色变更后，该账号需重新登录。管理员的个人 Token
          仍只具备本人相册权限。
        </p>
        <div class="form-two-columns">
          <div>
            <label class="field-label" for="account-quota"
              >容量配额（GiB）</label
            ><input
              id="account-quota"
              v-model.number="form.quota"
              type="number"
              min="0"
              max="1048576"
              step="any"
              required
            />
          </div>
          <div>
            <label class="field-label" for="account-max-file"
              >单张上限（MiB）</label
            ><input
              id="account-max-file"
              v-model.number="form.maxFile"
              type="number"
              min="0"
              :max="serverLimit / 2 ** 20"
              step="any"
              required
            />
          </div>
        </div>
        <p class="field-help">
          容量 0 表示不限；单张 0 表示跟随服务器。调低配额不会删除已有图片。
        </p>
        <p
          v-if="editing && editing.max_file_bytes > serverLimit"
          class="field-help"
        >
          该账号原单张上限高于当前服务器上限，现按服务器上限生效；表单已填入当前有效值，保存后采用此值。
        </p>
        <label v-if="editing?.id !== currentUserId" class="check-field"
          ><input v-model="form.enabled" type="checkbox" />启用账号</label
        >
        <template v-if="editing?.role !== 'admin'">
          <label class="field-label" for="account-password">{{
            editing ? "重置密码（留空保留）" : "初始密码"
          }}</label
          ><input
            id="account-password"
            v-model="form.password"
            type="password"
            autocomplete="new-password"
            maxlength="72"
            :required="!editing"
          />
          <label class="field-label" for="account-confirm">确认密码</label
          ><input
            id="account-confirm"
            v-model="form.confirm"
            type="password"
            autocomplete="new-password"
            maxlength="72"
            :required="!editing || !!form.password"
          />
        </template>
        <p v-else class="field-help">管理员密码请在账号设置中修改。</p>
        <p v-if="formError" class="form-error" role="alert">{{ formError }}</p>
        <div class="modal-actions">
          <button
            type="button"
            class="button"
            :disabled="busy"
            @click="
              open = false;
              form.password = '';
              form.confirm = '';
            "
          >
            取消</button
          ><button class="button primary" :disabled="busy">
            {{ busy ? "正在保存…" : "保存账号" }}
          </button>
        </div>
      </form>
    </Modal>
    <Modal
      v-if="deleting"
      title="删除用户"
      :busy="busy"
      @close="deleting = null"
    >
      <div class="modal-body">
        <p>
          永久删除用户“{{
            deleting.username
          }}”？存在图片或分享记录时不能删除，请先手动清理（包括已撤销的分享）。
        </p>
        <p class="field-help">
          删除成功后，同时清理该账号的空相册、登录会话和 Token。此操作无法恢复。
        </p>
        <p v-if="formError" class="form-error" role="alert">{{ formError }}</p>
        <div class="modal-actions">
          <button class="button" :disabled="busy" @click="deleting = null">
            取消</button
          ><button class="button danger" :disabled="busy" @click="removeUser">
            {{ busy ? "正在删除…" : "确认删除" }}
          </button>
        </div>
      </div>
    </Modal>
  </section>
</template>

<style scoped>
.user-actions-column {
  width: 180px;
}
.user-actions {
  flex-wrap: nowrap;
}
</style>
