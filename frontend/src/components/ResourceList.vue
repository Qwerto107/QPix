<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from "vue";
import { Plus, Copy } from "@lucide/vue";
import Modal from "./Modal.vue";
import TokenEditor from "./TokenEditor.vue";
import { tokenSummary } from "../tokenPermissions";
import SelectField from "./SelectField.vue";
import { api } from "../api";
import type { APIToken, ShareItem, RecordPage, User } from "../types";
import {
  absolute,
  copy,
  message,
  timestamp,
  remaining,
  preference,
  savePreference,
} from "../utils";
const props = defineProps<{
  kind: "tokens" | "shares";
  all?: boolean;
  canCreateToken?: boolean;
  accounts: User[];
}>();
const emit = defineEmits<{ notice: [text: string, error?: boolean] }>();
type Row = APIToken & ShareItem;
const rows = ref<Row[]>([]),
  q = ref(""),
  status = ref(""),
  owner = ref(""),
  sort = ref(preference(`${props.kind}.sort`, "desc", ["asc", "desc"])),
  page = ref(1),
  limit = ref(preference(`${props.kind}.limit`, "20", ["20", "50", "100"])),
  total = ref(0),
  loading = ref(false),
  error = ref("");
const cursors = ref([""]),
  nextCursor = ref("");
const tokenEditor = ref(false),
  editingToken = ref<APIToken | null>(null);
function editToken(token?: APIToken) {
  editingToken.value = token || null;
  tokenEditor.value = true;
}
const busy = ref(false),
  formError = ref(""),
  revoking = ref<Row | null>(null),
  deleting = ref<Row | null>(null);
const now = ref(Date.now());
const clock = setInterval(() => (now.value = Date.now()), 30000);
let requestID = 0,
  timer: ReturnType<typeof setTimeout> | undefined;
async function load() {
  page.value = 1;
  cursors.value = [""];
  nextCursor.value = "";
  await fetchPage();
}
async function turnPage(delta: number) {
  const previous = page.value;
  if (delta > 0) {
    if (!nextCursor.value) return;
    cursors.value[page.value] = nextCursor.value;
  }
  page.value += delta;
  await fetchPage();
  if (error.value) page.value = previous;
}
async function fetchPage() {
  const request = ++requestID;
  loading.value = true;
  error.value = "";
  try {
    const params = new URLSearchParams({
      scope: props.all ? "all" : "own",
      q: q.value,
      status: status.value,
      user_id: owner.value,
      pagination: "cursor",
      cursor: cursors.value[page.value - 1] || "",
      limit: limit.value,
      order: sort.value,
    });
    const data = await api<RecordPage<Row>>(`/${props.kind}?${params}`);
    if (request !== requestID) return;
    rows.value = data.items;
    if (data.total !== null) total.value = data.total;
    nextCursor.value = data.next_cursor;
    if (!data.items.length && page.value > 1) {
      page.value--;
      await fetchPage();
    }
  } catch (e) {
    if (request === requestID) error.value = message(e);
  } finally {
    if (request === requestID) loading.value = false;
  }
}
watch([q, status, owner, sort, limit], () => {
  page.value = 1;
  if (timer) clearTimeout(timer);
  savePreference(`${props.kind}.limit`, limit.value);
  savePreference(`${props.kind}.sort`, sort.value);
  timer = setTimeout(load, 250);
});
watch(
  () => [props.kind, props.all],
  () => {
    owner.value = "";
    page.value = 1;
    void load();
  },
  { immediate: true },
);
async function revoke() {
  if (!revoking.value) return;
  busy.value = true;
  formError.value = "";
  try {
    await api(`/${props.kind}/${revoking.value.id}`, "DELETE");
    revoking.value = null;
    await load();
    emit("notice", "已撤销，链接或凭据立即失效");
  } catch (e) {
    formError.value = message(e);
  } finally {
    busy.value = false;
  }
}
async function copyText(value: string) {
  try {
    await copy(value);
    emit("notice", "已复制到剪贴板");
  } catch {
    emit("notice", "复制失败，请检查剪贴板权限", true);
  }
}
async function deleteRecord() {
  if (!deleting.value) return;
  busy.value = true;
  formError.value = "";
  try {
    await api(`/${props.kind}/${deleting.value.id}/record`, "DELETE");
    deleting.value = null;
    await load();
    emit(
      "notice",
      props.kind === "tokens" ? "已删除 Token 记录" : "已删除分享记录",
    );
  } catch (e) {
    formError.value = message(e);
  } finally {
    busy.value = false;
  }
}
function rowStatus(row: Row) {
  return row.revoked_at
    ? "已撤销"
    : row.expires_at && new Date(row.expires_at).getTime() <= now.value
      ? "已到期"
      : "有效";
}
onBeforeUnmount(() => {
  requestID++;
  if (timer) clearTimeout(timer);
  clearInterval(clock);
});
</script>
<template>
  <section
    :aria-label="
      kind === 'tokens'
        ? all
          ? '全站 Token 列表'
          : '个人 Token 列表'
        : '图片分享列表'
    "
  >
    <div class="compact-toolbar">
      <input
        v-model="q"
        class="list-search"
        :placeholder="
          kind === 'tokens' ? '搜索 Token 名称' : '搜索分享图片名称'
        "
        aria-label="搜索列表"
      /><SelectField
        id="resource-status"
        label="筛选状态"
        v-model="status"
        :options="[
          { value: '', label: '全部状态' },
          { value: 'active', label: '有效' },
          { value: 'expired', label: '已到期' },
          { value: 'revoked', label: '已撤销' },
        ]"
      /><SelectField
        v-if="all"
        id="resource-owner"
        label="筛选创建人"
        v-model="owner"
        :options="[
          { value: '', label: '所有创建人' },
          ...accounts.map((v) => ({ value: v.id, label: v.username })),
        ]"
      /><SelectField
        id="resource-sort"
        label="创建时间排序"
        v-model="sort"
        :options="[
          { value: 'desc', label: '创建时间 ↓' },
          { value: 'asc', label: '创建时间 ↑' },
        ]"
      /><button
        v-if="kind === 'tokens' && canCreateToken"
        class="button primary compact"
        @click="editToken()"
      >
        <Plus :size="15" />创建 Token
      </button>
    </div>
    <p class="field-help" v-if="kind === 'tokens'">
      {{
        all
          ? "管理全体用户的个人 Token，可修改权限、有效期或撤销。"
          : "管理自己创建的 Token，可按本人相册分配操作权限。"
      }}所有 Token 均无系统管理权限。
    </p>
    <p v-else class="field-help">
      管理已有图片分享，可复制链接、撤销或删除已撤销记录。创建分享请在图库中点击图片的“分享”。
    </p>
    <p v-if="error" class="form-error" role="alert">
      {{ error }} <button class="text-button" @click="load">重试</button>
    </p>
    <div
      class="management-list"
      role="region"
      aria-label="资源表格，可横向滚动"
      tabindex="0"
      :aria-busy="loading"
    >
      <table class="management-table resource-table">
        <thead>
          <tr>
            <th>{{ kind === "tokens" ? "名称／前缀" : "分享图片" }}</th>
            <th v-if="all">创建人</th>
            <th v-if="all">身份</th>
            <th v-if="kind === 'tokens'">授权范围／权限</th>
            <th>有效期／到期日期</th>
            <th>创建时间</th>
            <th v-if="kind === 'tokens'">上次使用</th>
            <th v-else>密码</th>
            <th>状态</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="loading">
            <td :colspan="(all ? 8 : 6) + (kind === 'tokens' ? 1 : 0)">
              正在加载…
            </td>
          </tr>
          <tr v-else-if="!rows.length">
            <td
              :colspan="(all ? 8 : 6) + (kind === 'tokens' ? 1 : 0)"
              class="table-empty"
            >
              暂无符合条件的记录
            </td>
          </tr>
          <template v-else
            ><tr v-for="row in rows" :key="row.id">
              <td>
                <strong
                  class="management-name"
                  :title="kind === 'tokens' ? row.name : row.title"
                  >{{ kind === "tokens" ? row.name : row.title }}</strong
                ><code v-if="kind === 'tokens'" class="management-secondary"
                  >{{ row.prefix }}…</code
                >
              </td>
              <td v-if="all">{{ row.owner_name }}</td>
              <td v-if="all">
                <span class="tag">{{
                  row.owner_role === "admin" ? "管理员" : "用户"
                }}</span>
              </td>
              <td v-if="kind === 'tokens'">
                <span
                  class="token-scope-summary"
                  :title="tokenSummary(row.authorization)"
                  >{{ tokenSummary(row.authorization) }}</span
                >
              </td>
              <td>
                <span>{{ remaining(row.expires_at, now) }}</span
                ><time v-if="row.expires_at" class="management-secondary">{{
                  timestamp(row.expires_at)
                }}</time>
              </td>
              <td class="management-number">{{ timestamp(row.created_at) }}</td>
              <td v-if="kind === 'tokens'">
                {{
                  row.last_used_at ? timestamp(row.last_used_at) : "从未使用"
                }}
              </td>
              <td v-else>{{ row.has_password ? "已设置" : "无" }}</td>
              <td>
                <span
                  class="tag"
                  :class="{ 'is-muted': rowStatus(row) !== '有效' }"
                  >{{ rowStatus(row) }}</span
                >
              </td>
              <td>
                <div class="management-actions">
                  <button
                    v-if="kind === 'tokens'"
                    class="text-button"
                    :disabled="!!row.revoked_at"
                    @click="editToken(row)"
                  >
                    编辑权限
                  </button>
                  <button
                    v-if="kind === 'shares'"
                    class="text-button"
                    :disabled="rowStatus(row) !== '有效'"
                    @click="copyText(absolute(row.url))"
                  >
                    <Copy :size="14" />复制</button
                  ><button
                    v-if="!row.revoked_at"
                    class="text-button danger-text"
                    :disabled="!!row.revoked_at"
                    @click="
                      revoking = row;
                      formError = '';
                    "
                  >
                    撤销
                  </button>
                  <button
                    v-if="row.revoked_at"
                    class="text-button danger-text"
                    @click="
                      deleting = row;
                      formError = '';
                    "
                  >
                    删除
                  </button>
                </div>
              </td>
            </tr></template
          >
        </tbody>
      </table>
    </div>
    <div class="compact-pagination">
      <span>共 {{ total }} 条</span
      ><SelectField
        id="resource-limit"
        label="每页条数"
        v-model="limit"
        :options="
          ['20', '50', '100'].map((v) => ({ value: v, label: `${v} 条／页` }))
        "
      /><button
        class="button compact"
        :disabled="page <= 1 || loading"
        @click="turnPage(-1)"
      >
        上一页</button
      ><span>第 {{ page }} 页</span
      ><button
        class="button compact"
        :disabled="!nextCursor || loading"
        @click="turnPage(1)"
      >
        下一页
      </button>
    </div>
    <TokenEditor
      v-if="tokenEditor"
      :token="editingToken"
      :admin="all"
      @close="tokenEditor = false"
      @saved="load"
      @notice="(text, bad) => emit('notice', text, bad)"
    />
    <Modal
      v-if="deleting"
      :title="kind === 'tokens' ? '删除 Token 记录' : '删除分享记录'"
      :busy="busy"
      @close="deleting = null"
    >
      <div class="modal-body">
        <p>
          永久删除已撤销的{{ kind === "tokens" ? "Token" : "分享" }}“{{
            kind === "tokens"
              ? (deleting as APIToken).name
              : (deleting as ShareItem).title
          }}”记录？此操作无法恢复，不影响已上传图片。
        </p>
        <p v-if="formError" class="form-error" role="alert">{{ formError }}</p>
        <div class="modal-actions">
          <button class="button" :disabled="busy" @click="deleting = null">
            取消
          </button>
          <button class="button danger" :disabled="busy" @click="deleteRecord">
            {{ busy ? "正在删除…" : "确认删除" }}
          </button>
        </div>
      </div>
    </Modal>
    <Modal
      v-if="revoking"
      title="确认撤销"
      :busy="busy"
      @close="revoking = null"
      ><div class="modal-body">
        <p>
          撤销“{{
            kind === "tokens" ? revoking.name : revoking.title
          }}”？撤销后立即失效，记录保留。
        </p>
        <p v-if="formError" class="form-error">{{ formError }}</p>
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
