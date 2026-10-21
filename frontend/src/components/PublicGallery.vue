<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { LockKeyhole } from "@lucide/vue";
import Modal from "./Modal.vue";
import { api } from "../api";
import type { ImageItem } from "../types";
import { bytes, message } from "../utils";
const props = defineProps<{ token?: string }>();
const items = ref<ImageItem[]>([]);
const loading = ref(true);
const error = ref("");
const locked = ref(false);
const password = ref("");
const unlocking = ref(false);
const title = ref("图片分享");
const expires = ref<string | null>(null);
const page = ref(1);
const total = ref(0);
const limit = 36;
const detail = ref<ImageItem | null>(null);
const tokenPath = computed(
  () => `/public-shares/${encodeURIComponent(props.token || "")}`,
);
async function load() {
  loading.value = true;
  error.value = "";
  detail.value = null;
  items.value = [];
  try {
    const data = await api<{
      locked: boolean;
      title?: string;
      expires_at: string | null;
      items?: ImageItem[];
      total?: number;
    }>(`${tokenPath.value}?page=${page.value}`);
    locked.value = data.locked;
    expires.value = data.expires_at;
    if (!data.locked) {
      title.value = data.title || "图片分享";
      items.value = data.items || [];
      total.value = data.total || 0;
    }
  } catch (reason) {
    error.value = message(reason);
  } finally {
    loading.value = false;
  }
}
async function unlock() {
  unlocking.value = true;
  error.value = "";
  try {
    await api(`${tokenPath.value}/unlock`, "POST", {
      password: password.value,
    });
    password.value = "";
    await load();
  } catch (reason) {
    error.value = message(reason);
  } finally {
    unlocking.value = false;
  }
}
function changePage(value: number) {
  page.value = value;
  void load();
  window.scrollTo({ top: 0, behavior: "smooth" });
}
onMounted(load);
</script>
<template>
  <div class="public-page">
    <header class="public-header">
      <a href="/" class="brand"
        ><img src="/favicon.svg" alt="" /><span
          >QPix<span class="brand-dot">.</span></span
        ></a
      ><a class="button compact" href="/">登录 / 返回空间</a>
    </header>
    <main class="public-content">
      <div class="page-heading">
        <div>
          <span class="eyebrow">A COLLECTION TO SHARE</span>
          <h1>
            {{ locked ? "受密码保护的分享" : title }}
          </h1>
          <p>
            此链接仅授予指定图片或相册的浏览权限。<span v-if="expires">
              · {{ new Date(expires).toLocaleString() }} 到期</span
            >
          </p>
        </div>
      </div>
      <p v-if="loading" class="muted">正在读取…</p>
      <p v-if="error" class="form-error" role="alert">
        {{ error }}
        <button v-if="!locked" class="text-button" @click="load">重试</button>
      </p>
      <form
        v-if="locked && !loading"
        class="panel share-unlock"
        @submit.prevent="unlock"
      >
        <LockKeyhole :size="28" />
        <h2>请输入分享密码</h2>
        <label class="field-label" for="unlock-password">访问密码</label
        ><input
          id="unlock-password"
          v-model="password"
          type="password"
          maxlength="72"
          autocomplete="off"
          required
        /><button class="button primary full" :disabled="unlocking">
          {{ unlocking ? "正在验证…" : "查看分享" }}
        </button>
      </form>
      <template v-else-if="!loading && !error">
        <div class="image-grid">
          <article v-for="image in items" :key="image.id" class="image-card">
            <button
              class="public-image-open"
              :aria-label="`查看 ${image.name}`"
              @click="detail = image"
            >
              <img
                :src="image.thumbnail_url"
                :alt="image.name"
                loading="lazy"
              />
            </button>
            <div class="image-card-caption">
              <strong>{{ image.name }}</strong>
              <p class="field-help">
                {{ image.width }} × {{ image.height }} ·
                {{ bytes(image.size) }}
              </p>
            </div>
          </article>
        </div>
        <div v-if="!items.length" class="panel empty-state">
          <p>暂无可查看的图片。</p>
        </div>
        <div v-if="total > limit" class="pagination">
          <span>共 {{ total }} 张 · 第 {{ page }} 页</span>
          <div>
            <button
              class="button compact"
              :disabled="page <= 1"
              @click="changePage(page - 1)"
            >
              上一页</button
            ><button
              class="button compact"
              :disabled="page * limit >= total"
              @click="changePage(page + 1)"
            >
              下一页
            </button>
          </div>
        </div>
      </template>
    </main>
    <Modal v-if="detail" :title="detail.name" wide @close="detail = null"
      ><div class="public-image-detail">
        <img :src="detail.url" :alt="detail.name" /><a
          class="button"
          :href="detail.url"
          target="_blank"
          rel="noopener noreferrer"
          >打开图片</a
        >
      </div></Modal
    >
  </div>
</template>
