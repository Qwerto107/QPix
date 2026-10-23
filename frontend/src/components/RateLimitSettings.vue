<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { api } from "../api";
import type {
  RateLimitRule,
  RateLimitSettings,
  RateLimitScope,
} from "../types";
import { message } from "../utils";
import SelectField from "./SelectField.vue";

type Draft = RateLimitRule & { window: number; unit: string };
const emit = defineEmits<{ notice: [text: string, error?: boolean] }>();
const entries: { scope: RateLimitScope; label: string; description: string }[] =
  [
    {
      scope: "login",
      label: "登录",
      description: "按来源 IP 计数，包含成功与失败请求",
    },
    {
      scope: "share_unlock",
      label: "分享解锁",
      description: "按来源 IP 计数，所有分享共用",
    },
    { scope: "password_change", label: "修改密码", description: "按账号计数" },
    {
      scope: "upload",
      label: "上传图片",
      description: "按账号计数，登录会话与 Token 共用",
    },
    {
      scope: "share_create",
      label: "创建分享",
      description: "按账号计数，登录会话与 Token 共用",
    },
  ];
const units = [
  { value: "1", label: "秒" },
  { value: "60", label: "分钟" },
  { value: "3600", label: "小时" },
];
const drafts = ref<Partial<Record<RateLimitScope, Draft>>>({});
const defaults = ref<RateLimitSettings["defaults"]>();
const saved = ref("");
const loading = ref(true);
const busy = ref(false);
const error = ref("");

function draft(rule: RateLimitRule): Draft {
  const unit =
    rule.window_seconds % 3600 === 0
      ? "3600"
      : rule.window_seconds % 60 === 0
        ? "60"
        : "1";
  return { ...rule, window: rule.window_seconds / Number(unit), unit };
}
function values() {
  return Object.fromEntries(
    entries.map(({ scope }) => {
      const row = drafts.value[scope]!;
      return [
        scope,
        {
          enabled: row.enabled,
          requests: row.requests,
          window_seconds: row.window * Number(row.unit),
        },
      ];
    }),
  );
}
const loaded = computed(() =>
  entries.every(({ scope }) => !!drafts.value[scope]),
);
const changed = computed(
  () => loaded.value && JSON.stringify(values()) !== saved.value,
);
function apply(data: RateLimitSettings) {
  entries.forEach(({ scope }) => {
    drafts.value[scope] = draft(data.rules[scope]);
  });
  defaults.value = data.defaults;
  saved.value = JSON.stringify(values());
}
function restore(scope?: RateLimitScope) {
  if (!defaults.value) return;
  for (const entry of entries) {
    if (!scope || entry.scope === scope)
      drafts.value[entry.scope] = draft(defaults.value[entry.scope]);
  }
}
async function load() {
  loading.value = true;
  error.value = "";
  try {
    apply(await api<RateLimitSettings>("/rate-limits"));
  } catch (reason) {
    error.value = message(reason);
  } finally {
    loading.value = false;
  }
}
async function save() {
  if (busy.value || !changed.value) return;
  busy.value = true;
  error.value = "";
  try {
    apply(
      await api<RateLimitSettings>("/rate-limits", "PUT", { rules: values() }),
    );
    emit("notice", "限频策略已保存并生效");
  } catch (reason) {
    error.value = message(reason);
  } finally {
    busy.value = false;
  }
}
onMounted(load);
</script>

<template>
  <section class="panel settings-panel rate-settings" aria-label="限频策略">
    <h3>限频策略</h3>
    <p class="muted">
      限制同一来源在固定时间窗口内的请求次数。关闭限频仍须通过身份及访问权限校验。
    </p>
    <p v-if="loading" class="muted" role="status">正在读取限频策略…</p>
    <form v-else-if="loaded" :aria-busy="busy" @submit.prevent="save">
      <fieldset class="rate-fields" :disabled="busy">
        <div class="rate-head" aria-hidden="true">
          <span>操作与计数范围</span><span>状态</span><span>请求上限</span
          ><span>时间窗口</span><span></span>
        </div>
        <template v-for="entry in entries" :key="entry.scope">
          <div v-if="drafts[entry.scope]" class="rate-row">
            <div class="rate-description">
              <h4>{{ entry.label }}</h4>
              <p>{{ entry.description }}</p>
            </div>
            <label class="check-field rate-enabled">
              <input
                v-model="drafts[entry.scope]!.enabled"
                type="checkbox"
                :aria-label="`启用${entry.label}限频`"
              />
              {{ drafts[entry.scope]!.enabled ? "启用" : "停用" }}
            </label>
            <label class="rate-count">
              <span class="rate-mobile-label">请求上限</span>
              <input
                v-model.number="drafts[entry.scope]!.requests"
                type="number"
                min="1"
                max="1000000"
                step="1"
                required
                :disabled="!drafts[entry.scope]!.enabled"
                :aria-label="`${entry.label}请求上限`"
              />
              <span>次</span>
            </label>
            <div class="rate-window">
              <span class="rate-mobile-label">时间窗口</span>
              <input
                v-model.number="drafts[entry.scope]!.window"
                type="number"
                min="1"
                :max="86400 / Number(drafts[entry.scope]!.unit)"
                step="1"
                required
                :disabled="!drafts[entry.scope]!.enabled"
                :aria-label="`${entry.label}时间窗口`"
              />
              <fieldset
                :disabled="!drafts[entry.scope]!.enabled"
                class="rate-unit"
              >
                <SelectField
                  :id="`rate-unit-${entry.scope}`"
                  v-model="drafts[entry.scope]!.unit"
                  :label="`${entry.label}时间单位`"
                  :options="units"
                />
              </fieldset>
            </div>
            <button
              type="button"
              class="text-button rate-restore"
              :aria-label="`恢复${entry.label}默认值`"
              @click="restore(entry.scope)"
            >
              恢复默认
            </button>
          </div>
        </template>
      </fieldset>
      <div class="rate-footer">
        <p class="field-help">
          保存后立即生效，并清空已修改规则的计数。时间窗口最长 24
          小时；恢复默认后需保存。
        </p>
        <div class="panel-actions">
          <button
            type="button"
            class="button"
            :disabled="busy"
            @click="restore()"
          >
            全部恢复默认
          </button>
          <button class="button primary" :disabled="busy || !changed">
            {{ busy ? "正在保存…" : "保存策略" }}
          </button>
        </div>
      </div>
    </form>
    <p v-if="error" class="form-error" role="alert">
      {{ error }}
      <button v-if="!loaded" type="button" class="text-button" @click="load">
        重试
      </button>
    </p>
  </section>
</template>

<style scoped>
.rate-settings {
  grid-column: 1 / -1;
}
.rate-settings > .muted {
  color: #596d65;
}
.rate-fields,
.rate-unit {
  border: 0;
  margin: 0;
  padding: 0;
  min-width: 0;
}
.rate-head,
.rate-row {
  display: grid;
  grid-template-columns: minmax(220px, 1fr) 76px 128px 200px 80px;
  gap: 18px;
  align-items: center;
}
.rate-head {
  padding: 20px 0 10px;
  color: #596d65;
  font-size: 12px;
}
.rate-row {
  padding: 16px 0;
  border-top: 1px solid var(--line);
}
.rate-description h4 {
  margin: 0 0 5px;
  font-size: 14px;
}
.rate-description p {
  margin: 0;
  color: #596d65;
  font-size: 12px;
  line-height: 1.6;
}
.rate-enabled {
  margin: 0;
  white-space: nowrap;
}
.rate-count,
.rate-window {
  display: flex;
  align-items: center;
  gap: 8px;
  font-variant-numeric: tabular-nums;
}
.rate-count input,
.rate-window input {
  width: 100%;
  min-width: 0;
  margin: 0;
  height: 34px;
  padding: 7px 10px;
  font-size: 12px;
  border-radius: 7px;
}
.rate-count > span:not(.rate-mobile-label) {
  color: #596d65;
  font-size: 13px;
}
.rate-unit {
  flex: 0 0 82px;
}
.rate-unit :deep(.select-trigger) {
  margin: 0;
}
.rate-restore {
  justify-self: end;
  white-space: nowrap;
}
.rate-mobile-label {
  display: none;
}
.rate-footer {
  border-top: 1px solid var(--line);
  padding-top: 14px;
}
.rate-footer .field-help {
  margin: 0 0 16px;
  color: #596d65;
}
.rate-footer .panel-actions {
  border: 0;
  margin: 0;
  padding: 0;
  gap: 10px;
}
@media (max-width: 1200px) {
  .rate-head {
    display: none;
  }
  .rate-row {
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 12px 18px;
  }
  .rate-description {
    grid-column: 1;
  }
  .rate-enabled {
    grid-column: 2;
    grid-row: 1;
  }
  .rate-count,
  .rate-window {
    grid-column: 1 / -1;
    max-width: 420px;
  }
  .rate-mobile-label {
    display: inline;
    white-space: nowrap;
    width: 64px;
    flex-shrink: 0;
    font-size: 12px;
    color: #596d65;
  }
  .rate-restore {
    grid-column: 1 / -1;
  }
}
@media (max-width: 480px) {
  .rate-footer .panel-actions {
    display: flex;
    flex-wrap: wrap;
  }
  .rate-footer .button {
    flex: 1;
    white-space: nowrap;
  }
}
</style>
