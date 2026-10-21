<script setup lang="ts">
import { onMounted, ref } from "vue";
import Modal from "./Modal.vue";
import { api } from "../api";
import type { AccessRule } from "../types";
import { message } from "../utils";
type Draft = AccessRule & { ipText: string; refererText: string };
const emit = defineEmits<{ notice: [text: string, error?: boolean] }>();
const rules = ref<Draft[]>([]);
const loading = ref(true);
const error = ref("");
const busy = ref("");
const editing = ref<Draft | null>(null);
const requesterIP = ref("");
const refererHost = ref("");
const bypassed = ref(false);
const labels = { admin: "管理端", upload: "上传接口", images: "图片外链" };
const descriptions = {
  admin: "管理员登录与管理操作，页面静态资源不受限。",
  upload: "所有图片上传请求，包括上传 Token。",
  images: "图片、缩略图、分享及普通用户登录与操作。",
};
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const data = await api<{
      items: AccessRule[];
      requester_ip: string;
      referer_host: string;
      bypassed: boolean;
    }>("/access");
    rules.value = data.items.map((row) => ({
      ...row,
      ipText: row.ips.join("\n"),
      refererText: row.referers.join("\n"),
    }));
    requesterIP.value = data.requester_ip;
    refererHost.value = data.referer_host;
    bypassed.value = data.bypassed;
  } catch (reason) {
    error.value = message(reason);
  } finally {
    loading.value = false;
  }
}
function lines(value: string) {
  return value
    .split(/\r?\n/)
    .map((item) => item.trim())
    .filter(Boolean);
}
function edit(row: Draft) {
  editing.value = { ...row };
}
async function save(row: Draft) {
  busy.value = row.scope;
  try {
    const updated = await api<AccessRule>(`/access/${row.scope}`, "PUT", {
      scope: row.scope,
      ip_enabled: row.ip_enabled,
      ips: lines(row.ipText),
      referer_enabled: row.referer_enabled,
      referers: lines(row.refererText),
      allow_empty_referer: row.allow_empty_referer,
    });
    Object.assign(
      rules.value.find((item) => item.scope === row.scope)!,
      updated,
      {
        ipText: updated.ips.join("\n"),
        refererText: updated.referers.join("\n"),
      },
    );
    editing.value = null;
    emit("notice", `${labels[row.scope]}规则已保存`);
  } catch (reason) {
    emit("notice", message(reason), true);
  } finally {
    busy.value = "";
  }
}
onMounted(load);
</script>
<template>
  <section class="access-settings" aria-label="访问控制">
    <div class="access-summary">
      <p>
        当前请求 IP：<strong>{{ requesterIP || "未知" }}</strong> · Referer
        域名：<strong>{{ refererHost || "空" }}</strong>
      </p>
      <p class="field-help">
        IP 与 Referer 同时启用时，必须同时满足。域名不包含协议、路径或端口，支持
        *.example.com（不含主域名）。Referer 由客户端提供，不能代替身份验证。
      </p>
      <p v-if="bypassed" class="form-error" role="alert">
        应急开关已开启，所有访问规则当前均被跳过。恢复配置后请关闭服务端
        QPIX_DISABLE_ACCESS_RULES。
      </p>
    </div>
    <p v-if="loading" class="muted">正在读取访问规则…</p>
    <p v-if="error" class="form-error" role="alert">
      {{ error }} <button class="text-button" @click="load">重试</button>
    </p>
    <div
      v-if="rules.length"
      class="management-list"
      role="region"
      aria-label="访问规则表格，可横向滚动"
      tabindex="0"
    >
      <table class="management-table" aria-label="访问规则列表">
        <colgroup>
          <col style="width: 32%" />
          <col style="width: 24%" />
          <col style="width: 28%" />
          <col style="width: 16%" />
        </colgroup>
        <thead>
          <tr>
            <th scope="col">适用范围</th>
            <th scope="col">IP 白名单</th>
            <th scope="col">Referer 限制</th>
            <th scope="col">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in rules" :key="row.scope">
            <th scope="row">
              <span class="management-name">{{ labels[row.scope] }}</span
              ><span class="management-secondary">{{
                descriptions[row.scope]
              }}</span>
            </th>
            <td>
              <span class="tag" :class="{ 'is-muted': !row.ip_enabled }">{{
                row.ip_enabled ? "已启用" : "未启用"
              }}</span
              ><span v-if="row.ip_enabled" class="management-secondary"
                >{{ row.ips.length }} 项 IP / CIDR</span
              >
            </td>
            <td>
              <span class="tag" :class="{ 'is-muted': !row.referer_enabled }">{{
                row.referer_enabled ? "已启用" : "未启用"
              }}</span
              ><span v-if="row.referer_enabled" class="management-secondary"
                >{{ row.referers.length }} 项来源域名 ·
                {{
                  row.allow_empty_referer ? "允许空来源" : "拒绝空来源"
                }}</span
              >
            </td>
            <td>
              <button
                class="button compact"
                :aria-label="`编辑${labels[row.scope]}规则`"
                @click="edit(row)"
              >
                编辑规则
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <Modal
      v-if="editing"
      :title="`${labels[editing.scope]}访问规则`"
      :busy="!!busy"
      @close="editing = null"
    >
      <form class="modal-body" @submit.prevent="save(editing)">
        <p class="field-help">{{ descriptions[editing.scope] }}</p>
        <label class="check-field"
          ><input
            v-model="editing.ip_enabled"
            type="checkbox"
            :disabled="!!busy"
          />启用 IP 白名单</label
        >
        <label class="field-label" for="rule-ips">IP / CIDR，每行一项</label>
        <textarea
          id="rule-ips"
          v-model="editing.ipText"
          rows="4"
          placeholder="192.0.2.10&#10;2001:db8::/32"
          :disabled="!editing.ip_enabled || !!busy"
        />
        <label class="check-field"
          ><input
            v-model="editing.referer_enabled"
            type="checkbox"
            :disabled="!!busy"
          />启用 Referer 限制</label
        >
        <label class="field-label" for="rule-referers"
          >允许的来源域名，每行一项</label
        >
        <textarea
          id="rule-referers"
          v-model="editing.refererText"
          rows="4"
          placeholder="example.com&#10;*.example.com"
          :disabled="!editing.referer_enabled || !!busy"
        />
        <label class="check-field"
          ><input
            v-model="editing.allow_empty_referer"
            type="checkbox"
            :disabled="!editing.referer_enabled || !!busy"
          />允许空 Referer</label
        >
        <p v-if="editing.scope === 'admin'" class="field-help">
          保存时会检查当前请求是否仍可访问，避免将自己锁在管理端外。
        </p>
        <div class="modal-actions">
          <button
            class="button"
            type="button"
            :disabled="!!busy"
            @click="editing = null"
          >
            取消
          </button>
          <button class="button primary" :disabled="!!busy">
            {{ busy ? "正在保存…" : "保存规则" }}
          </button>
        </div>
      </form>
    </Modal>
  </section>
</template>
