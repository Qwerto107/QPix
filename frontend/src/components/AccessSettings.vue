<script setup lang="ts">
import { onMounted, ref } from "vue";
import { api } from "../api";
import type { AccessRule } from "../types";
import { message } from "../utils";
type Draft = AccessRule & { ipText: string; refererText: string };
const emit = defineEmits<{ notice: [text: string, error?: boolean] }>();
const rules = ref<Draft[]>([]);
const loading = ref(true);
const error = ref("");
const busy = ref("");
const saveErrors = ref<Partial<Record<AccessRule["scope"], string>>>({});
const requesterIP = ref("");
const refererHost = ref("");
const bypassed = ref(false);
const labels = { admin: "管理端", upload: "上传接口", images: "图片直链" };
const descriptions = {
  admin: "管理员登录与管理操作，页面静态资源不受限。",
  upload: "所有图片上传请求，校验 IP 和登录／Token 身份，不校验 Referer。",
  images: "仅原图、预览图和缩略图直链，不影响分享、普通用户登录和操作。",
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
async function save(row: Draft) {
  if (busy.value) return;
  busy.value = row.scope;
  saveErrors.value[row.scope] = "";
  try {
    const updated = await api<AccessRule>(`/access/${row.scope}`, "PUT", {
      scope: row.scope,
      ip_enabled: row.ip_enabled,
      ips: lines(row.ipText),
      referer_enabled: row.referer_enabled,
      referers: lines(row.refererText),
      allow_empty_referer: row.allow_empty_referer,
    });
    Object.assign(row, updated, {
      ipText: updated.ips.join("\n"),
      refererText: updated.referers.join("\n"),
    });
    emit("notice", `${labels[row.scope]}规则已保存`);
  } catch (reason) {
    saveErrors.value[row.scope] = message(reason);
  } finally {
    busy.value = "";
  }
}
onMounted(load);
</script>
<template>
  <section class="access-settings" aria-label="访问控制">
    <div class="access-summary">
      <p class="access-current">
        <span
          >当前请求 IP：<strong>{{ requesterIP || "未知" }}</strong></span
        >
        <span
          >Referer 域名：<strong>{{ refererHost || "空" }}</strong></span
        >
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
    <div v-if="rules.length" class="settings-main">
      <form
        v-for="row in rules"
        :key="row.scope"
        class="panel settings-panel access-rule-form"
        :aria-labelledby="`access-title-${row.scope}`"
        :aria-busy="busy === row.scope"
        @submit.prevent="save(row)"
      >
        <h3 :id="`access-title-${row.scope}`">{{ labels[row.scope] }}</h3>
        <p class="muted">{{ descriptions[row.scope] }}</p>
        <fieldset
          class="access-rule-fields"
          :disabled="!!busy"
          :aria-label="`${labels[row.scope]}访问规则`"
        >
          <div class="access-rule-section">
            <label class="check-field"
              ><input v-model="row.ip_enabled" type="checkbox" />启用 IP
              白名单</label
            >
            <div v-if="row.ip_enabled" :id="`access-ip-fields-${row.scope}`">
              <label class="field-label" :for="`rule-ips-${row.scope}`"
                >允许的 IP / CIDR，每行一项</label
              >
              <textarea
                :id="`rule-ips-${row.scope}`"
                v-model="row.ipText"
                rows="3"
                required
                placeholder="192.0.2.10&#10;2001:db8::/32"
              />
              <p class="field-help">
                支持 IPv4、IPv6 和 CIDR 网段，最多 200 项。仅允许列表内的客户端
                IP 访问。
              </p>
            </div>
          </div>
          <div v-if="row.scope !== 'upload'" class="access-rule-section">
            <label class="check-field"
              ><input v-model="row.referer_enabled" type="checkbox" />启用
              Referer 限制</label
            >
            <div
              v-if="row.referer_enabled"
              :id="`access-referer-fields-${row.scope}`"
            >
              <label class="field-label" :for="`rule-referers-${row.scope}`"
                >允许的来源域名，每行一项</label
              >
              <textarea
                :id="`rule-referers-${row.scope}`"
                v-model="row.refererText"
                rows="3"
                required
                placeholder="example.com&#10;*.example.com"
              />
              <p class="field-help">
                域名不含协议、端口或路径，最多 200 项。*.example.com
                匹配子域名，不包含主域名。
              </p>
              <label class="check-field"
                ><input
                  v-model="row.allow_empty_referer"
                  type="checkbox"
                />允许空 Referer</label
              >
              <p class="field-help">
                IP 与 Referer 同时启用时须同时满足。Referer
                由客户端提供，不能代替身份验证。
              </p>
            </div>
          </div>
          <p
            v-if="
              row.scope === 'admin' && (row.ip_enabled || row.referer_enabled)
            "
            class="field-help"
          >
            保存时会检查当前请求是否仍可访问，避免将自己锁在管理端外。
          </p>
          <p v-if="row.scope === 'images'" class="field-help">
            相册可覆盖此处的 Referer 策略，全局图片 IP 白名单始终生效。
          </p>
          <p v-if="saveErrors[row.scope]" class="form-error" role="alert">
            {{ saveErrors[row.scope] }}
          </p>
          <div class="panel-actions">
            <button class="button primary">
              {{
                busy === row.scope
                  ? "正在保存…"
                  : `保存${labels[row.scope]}规则`
              }}
            </button>
          </div>
        </fieldset>
      </form>
    </div>
  </section>
</template>
