<script setup lang="ts">
import { ref, watch } from "vue";
import { api } from "../api";
import type { Settings } from "../types";
import { message } from "../utils";
const props = defineProps<{ settings: Settings }>();
const emit = defineEmits<{
  changed: [];
  reauthenticate: [];
  notice: [text: string, error?: boolean];
}>();
const appURL = ref(props.settings.app_url);
const secureCookie = ref(props.settings.secure_cookie);
const proxyText = ref(props.settings.trusted_proxies.join("\n"));
const busy = ref(false);
const error = ref("");
watch(
  () => props.settings,
  (value, previous) => {
    if (appURL.value === previous.app_url) appURL.value = value.app_url;
    if (secureCookie.value === previous.secure_cookie)
      secureCookie.value = value.secure_cookie;
    if (proxyText.value === previous.trusted_proxies.join("\n"))
      proxyText.value = value.trusted_proxies.join("\n");
  },
);
async function save() {
  busy.value = true;
  error.value = "";
  try {
    const result = await api<{ reauthenticate: boolean }>(
      "/settings",
      "PATCH",
      {
        app_url: appURL.value,
        secure_cookie: secureCookie.value,
        trusted_proxies: proxyText.value
          .split(/\r?\n/)
          .map((value) => value.trim())
          .filter(Boolean),
      },
    );
    if (result.reauthenticate) {
      emit("reauthenticate");
      emit("notice", "站点设置已保存，Cookie 策略变更，请重新登录");
    } else {
      emit("changed");
      emit("notice", "站点设置已保存");
    }
  } catch (reason) {
    error.value = message(reason);
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <form class="panel settings-panel" @submit.prevent="save">
    <h3>站点与代理</h3>
    <label class="field-label" for="site-origin">站点地址</label>
    <input
      id="site-origin"
      v-model="appURL"
      type="url"
      placeholder="https://qpix.example.com"
      :disabled="busy"
    />
    <p class="field-help">
      用于校验操作请求的
      Origin。留空按当前请求域名校验；修改后请从此地址访问管理界面。
    </p>
    <label class="field-label" for="trusted-proxies"
      >可信代理 IP / CIDR，每行一项</label
    >
    <textarea
      id="trusted-proxies"
      v-model="proxyText"
      rows="3"
      placeholder="127.0.0.1&#10;::1"
      :disabled="busy"
    />
    <p class="field-help">
      仅填写实际反向代理的地址，最多 200
      项。留空不信任转发头。填写后从右向左解析 X-Forwarded-For，识别真实客户端
      IP。
    </p>
    <label class="check-field"
      ><input v-model="secureCookie" type="checkbox" :disabled="busy" />仅通过
      HTTPS 发送登录 Cookie</label
    >
    <p class="field-help">
      启用前请通过 HTTPS 访问；代理需传递 X-Forwarded-Proto:
      https。改变此项会使全部登录和分享解锁会话失效，个人 Token 保持有效。
    </p>
    <p v-if="error" class="form-error" role="alert">{{ error }}</p>
    <div class="panel-actions">
      <button class="button primary" :disabled="busy">
        {{ busy ? "正在保存…" : "保存站点设置" }}
      </button>
    </div>
  </form>
</template>
