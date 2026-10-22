<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import {
  ArrowDownToLine,
  Check,
  ChevronRight,
  CircleAlert,
  Database,
  Folder,
  HardDrive,
  Image as ImageIcon,
  Images,
  KeyRound,
  LoaderCircle,
  LogOut,
  Menu,
  Plus,
  Settings as SettingsIcon,
  ShieldCheck,
  Sparkles,
  Upload,
  X,
  ArrowUpRight,
} from "@lucide/vue";
import Modal from "./components/Modal.vue";
import UploadPanel from "./components/UploadPanel.vue";
import LibraryBrowser from "./components/LibraryBrowser.vue";
import ResourceList from "./components/ResourceList.vue";
import StorageSettings from "./components/StorageSettings.vue";
import ProcessingSettings from "./components/ProcessingSettings.vue";
import AccessSettings from "./components/AccessSettings.vue";
import UserSettings from "./components/UserSettings.vue";
import SiteSettings from "./components/SiteSettings.vue";
import PublicGallery from "./components/PublicGallery.vue";
import { api, APIError, onSessionExpired, setCSRF } from "./api";
import type {
  Album,
  ImageItem,
  User,
  Stats,
  Settings,
  StorageProfile,
  UploadOptions,
} from "./types";
import { bytes, message } from "./utils";
type View =
  | "library"
  | "upload"
  | "tokens"
  | "shares"
  | "security"
  | "settings"
  | "storages"
  | "processing"
  | "access"
  | "users"
  | "manageImages"
  | "manageShares"
  | "manageTokens";
const publicPage = window.location.pathname.startsWith("/s/"),
  shareToken = publicPage ? window.location.pathname.split("/")[2] : undefined;
const user = ref<User | null>(null),
  isAdmin = computed(() => user.value?.role === "admin"),
  initializing = ref(true),
  initializationError = ref(""),
  username = ref("admin"),
  password = ref(""),
  loginError = ref(""),
  loginBusy = ref(false),
  view = ref<View>("library"),
  mobileNav = ref(false);
const albums = ref<Album[]>([]),
  statistics = ref<Stats>({
    images: 0,
    bytes: 0,
    albums: 0,
    unfiled: 0,
    today: 0,
    pending_deletions: 0,
  }),
  settings = ref<Settings | null>(null),
  uploadOptions = ref<UploadOptions | null>(null),
  accountRows = ref<User[]>([]),
  storages = ref<StorageProfile[]>([]),
  albumFilter = ref(""),
  uploadAlbum = ref(""),
  version = ref(0),
  shareTarget = ref<{ id: string; name: string } | null>(null),
  uploader = ref<InstanceType<typeof UploadPanel>>();
const uploadStores = computed(
  () =>
    uploadOptions.value?.storages.map((v) => ({ ...v, enabled: true })) || [],
);
const title = computed(
  () =>
    ({
      library: "我的图库",
      upload: "上传图片",
      tokens: "我的 Token",
      shares: "我的分享",
      security: "账号安全",
      settings: "系统设置",
      storages: "存储管理",
      processing: "图片处理",
      access: "访问控制",
      users: "用户管理",
      manageImages: "图片管理",
      manageShares: "分享管理",
      manageTokens: "Token 管理",
    })[view.value],
);
const workspaceNavItems = computed(() => [
  { key: "library" as View, label: "我的图库", icon: Images },
  { key: "upload" as View, label: "上传图片", icon: Upload },
  { key: "shares" as View, label: "我的分享", icon: ArrowUpRight },
  { key: "tokens" as View, label: "我的 Token", icon: KeyRound },
]);
const adminNavItems = [
  { key: "manageImages" as View, label: "图片管理", icon: Images },
  { key: "manageShares" as View, label: "分享管理", icon: ArrowUpRight },
  { key: "manageTokens" as View, label: "Token 管理", icon: KeyRound },
  { key: "users" as View, label: "用户管理", icon: ShieldCheck },
  { key: "storages" as View, label: "存储管理", icon: HardDrive },
  { key: "processing" as View, label: "图片处理", icon: Sparkles },
  { key: "access" as View, label: "访问控制", icon: ShieldCheck },
  { key: "settings" as View, label: "系统设置", icon: SettingsIcon },
];
const storageLabel = computed(
  () =>
    ({ sqlite: "SQLite", postgres: "PostgreSQL", mysql: "MySQL" })[
      settings.value?.database || "sqlite"
    ],
);
const notifications = ref<{ id: number; text: string; error?: boolean }[]>([]);
let notificationID = 0;
const timers = new Set<ReturnType<typeof setTimeout>>();
function later(fn: () => void, ms: number) {
  const timer = setTimeout(() => {
    timers.delete(timer);
    fn();
  }, ms);
  timers.add(timer);
  return timer;
}
function notice(text: string, error = false) {
  const id = ++notificationID;
  notifications.value.push({ id, text, error });
  later(
    () =>
      (notifications.value = notifications.value.filter((v) => v.id !== id)),
    4500,
  );
}
function go(next: View, album = "") {
  if (next === "upload")
    uploadAlbum.value =
      (view.value === "library" ? albumFilter.value : "") ||
      albums.value.find((v) => v.is_default)?.id ||
      "";
  view.value = next;
  albumFilter.value = album;
  mobileNav.value = false;
  shareTarget.value = null;
}
function uploadTo(id: string) {
  go("upload");
  uploadAlbum.value = id || albums.value.find((v) => v.is_default)?.id || "";
}
function share(image: ImageItem) {
  go(view.value === "manageImages" ? "manageShares" : "shares");
  shareTarget.value = { id: image.id, name: image.name };
}
async function initialize() {
  initializing.value = true;
  initializationError.value = "";
  try {
    const data = await api<{ user: User; csrf_token: string }>("/auth/me");
    user.value = data.user;
    setCSRF(data.csrf_token);
    await refreshMeta();
  } catch (e) {
    if (!(e instanceof APIError && e.status === 401))
      initializationError.value = message(e);
  } finally {
    initializing.value = false;
  }
}
async function login() {
  loginBusy.value = true;
  loginError.value = "";
  try {
    const data = await api<{ user: User; csrf_token: string }>(
      "/auth/login",
      "POST",
      { username: username.value, password: password.value },
    );
    user.value = data.user;
    setCSRF(data.csrf_token);
    password.value = "";
    await refreshMeta();
  } catch (e) {
    loginError.value = message(e);
  } finally {
    loginBusy.value = false;
  }
}
function clearSession() {
  user.value = null;
  setCSRF("");
  albums.value = [];
  settings.value = null;
  uploadOptions.value = null;
  accountRows.value = [];
  storages.value = [];
  shareTarget.value = null;
  albumFilter.value = "";
  uploadAlbum.value = "";
  currentPassword.value = "";
  newPassword.value = "";
  confirmNewPassword.value = "";
  modal.value = "";
  view.value = "library";
}
async function logout() {
  try {
    await api("/auth/logout", "POST");
    clearSession();
  } catch (e) {
    notice(message(e), true);
  }
}
onSessionExpired(() => {
  if (user.value) {
    clearSession();
    notice("登录已失效，请重新登录", true);
  }
});
async function refreshMeta() {
  const identity = user.value?.id;
  try {
    const [albumRows, stats, options] = await Promise.all([
      api<Album[]>("/albums"),
      api<Stats>("/stats"),
      api<UploadOptions>("/upload-options"),
    ]);
    if (!user.value || user.value.id !== identity) return;
    albums.value = albumRows;
    statistics.value = stats;
    uploadOptions.value = options;
    version.value++;
    if (
      !uploadAlbum.value ||
      !albumRows.some((v) => v.id === uploadAlbum.value)
    )
      uploadAlbum.value = albumRows.find((v) => v.is_default)?.id || "";
    if (!isAdmin.value) return;
    const [config, storageRows, accounts] = await Promise.all([
      api<Settings>("/settings"),
      api<StorageProfile[]>("/storages"),
      api<User[]>("/users"),
    ]);
    if (!user.value || user.value.id !== identity) return;
    const editingDomain =
      settings.value && publicURL.value !== settings.value.public_url;
    const editingLimit =
      settings.value &&
      serverUploadMiB.value !== settings.value.max_upload_bytes / 2 ** 20;
    const editingPixels =
      settings.value &&
      serverPixels.value !== settings.value.max_pixels / 1_000_000;
    settings.value = config;
    storages.value = storageRows;
    accountRows.value = accounts;
    if (!editingDomain) publicURL.value = config.public_url;
    if (!editingLimit)
      serverUploadMiB.value = config.max_upload_bytes / 2 ** 20;
    if (!editingPixels) serverPixels.value = config.max_pixels / 1_000_000;
  } catch (e) {
    notice(message(e), true);
  }
}
const modal = ref<"" | "album">(""),
  editingAlbum = ref<Album | null>(null),
  albumDraft = ref(""),
  modalBusy = ref(false),
  modalError = ref("");
function openAlbum(album?: Album) {
  editingAlbum.value = album || null;
  albumDraft.value = album?.name || "";
  modalError.value = "";
  modal.value = "album";
}
async function saveAlbum() {
  modalBusy.value = true;
  modalError.value = "";
  try {
    await api(
      editingAlbum.value ? `/albums/${editingAlbum.value.id}` : "/albums",
      editingAlbum.value ? "PATCH" : "POST",
      { name: albumDraft.value },
    );
    modal.value = "";
    await refreshMeta();
    notice("相册已保存");
  } catch (e) {
    modalError.value = message(e);
  } finally {
    modalBusy.value = false;
  }
}
function closeModal() {
  if (!modalBusy.value) modal.value = "";
}
const publicURL = ref(""),
  settingsBusy = ref(false),
  serverUploadMiB = ref(20),
  serverPixels = ref(25),
  uploadLimitBusy = ref(false),
  currentPassword = ref(""),
  newPassword = ref(""),
  confirmNewPassword = ref(""),
  passwordBusy = ref(false);
const passwordsMismatch = computed(
  () =>
    confirmNewPassword.value !== "" &&
    newPassword.value !== confirmNewPassword.value,
);
async function saveSettings() {
  settingsBusy.value = true;
  try {
    await api("/settings", "PATCH", { public_url: publicURL.value });
    await refreshMeta();
    notice("图片访问域名已保存");
  } catch (e) {
    notice(message(e), true);
  } finally {
    settingsBusy.value = false;
  }
}
async function saveUploadLimit() {
  uploadLimitBusy.value = true;
  try {
    await api("/settings", "PATCH", {
      max_upload_bytes: Math.round(serverUploadMiB.value * 2 ** 20),
      max_pixels: Math.round(serverPixels.value * 1_000_000),
    });
    await refreshMeta();
    notice("服务器单张上传上限已保存");
  } catch (e) {
    notice(message(e), true);
  } finally {
    uploadLimitBusy.value = false;
  }
}
async function changePassword() {
  if (newPassword.value !== confirmNewPassword.value) {
    notice("两次输入的新密码不一致", true);
    return;
  }
  passwordBusy.value = true;
  try {
    await api("/auth/password", "PUT", {
      current_password: currentPassword.value,
      new_password: newPassword.value,
    });
    clearSession();
    notice("密码已修改，请重新登录。上传 Token 保持有效。");
  } catch (e) {
    notice(message(e), true);
  } finally {
    passwordBusy.value = false;
  }
}
async function receiveFiles(files: FileList | File[]) {
  if (!user.value) return;
  go("upload");
  await nextTick();
  uploader.value?.addFiles(files);
}
function paste(event: ClipboardEvent) {
  if (event.clipboardData?.files.length && user.value) {
    event.preventDefault();
    void receiveFiles(event.clipboardData.files);
  }
}
function drop(event: DragEvent) {
  if (!event.dataTransfer?.types.includes("Files")) return;
  event.preventDefault();
  if (event.dataTransfer.files.length)
    void receiveFiles(event.dataTransfer.files);
}
function dragover(event: DragEvent) {
  if (event.dataTransfer?.types.includes("Files")) event.preventDefault();
}
let uploadRefresh: ReturnType<typeof setTimeout> | undefined;
function uploaded() {
  if (uploadRefresh) clearTimeout(uploadRefresh);
  uploadRefresh = later(() => void refreshMeta(), 250);
}
onMounted(() => {
  if (!publicPage) void initialize();
  document.addEventListener("paste", paste);
  window.addEventListener("drop", drop);
  window.addEventListener("dragover", dragover);
});
onBeforeUnmount(() => {
  timers.forEach(clearTimeout);
  document.removeEventListener("paste", paste);
  window.removeEventListener("drop", drop);
  window.removeEventListener("dragover", dragover);
});
</script>
<template>
  <PublicGallery v-if="publicPage" :token="shareToken" />
  <div v-else-if="initializing" class="initializing">
    <img src="/favicon.svg" alt="QPix" width="48" height="48" /><LoaderCircle
      class="spin"
      :size="22"
    /><span>正在打开你的图片空间…</span>
  </div>
  <div v-else-if="!user" class="login-page">
    <section class="login-story">
      <a class="brand" href="/"
        ><img src="/favicon.svg" alt="" /><span
          >QPix<span class="brand-dot">.</span></span
        ></a
      >
      <div class="login-story-content">
        <span class="eyebrow">A LITTLE SPACE FOR YOUR PIXELS</span>
        <h1>每一张图片，<br />都值得好好收藏。</h1>
        <p>上传、整理、分享。<br />你的图片空间，简单一点。</p>
        <div class="login-art" aria-hidden="true">
          <div class="art-card art-card-back">
            <div class="art-sun" />
            <div class="art-mountain one" />
            <div class="art-mountain two" />
          </div>
          <div class="art-card art-card-front">
            <div class="art-sun" />
            <div class="art-mountain one" />
            <div class="art-mountain two" />
            <div class="art-caption">a moment worth keeping.</div>
          </div>
          <div class="art-label"><Sparkles :size="15" />留住每一份灵感</div>
        </div>
      </div>
      <div class="login-story-footer">
        <span>轻巧 · 私有 · 自由</span><span>PERSONAL IMAGE HOSTING</span>
      </div>
    </section>
    <section class="login-form-area">
      <div class="login-form">
        <span class="tag"><ShieldCheck :size="14" />私人图片空间</span>
        <h2>欢迎回来</h2>
        <p class="muted">登录 QPix，继续整理你的灵感。</p>
        <div v-if="initializationError" class="form-error" role="alert">
          {{ initializationError }}
          <button class="text-button" @click="initialize">重新连接</button>
        </div>
        <form @submit.prevent="login">
          <label class="field-label" for="username">用户名</label
          ><input
            id="username"
            v-model="username"
            name="username"
            autocomplete="username"
            required
            maxlength="64"
          /><label class="field-label" for="password">密码</label
          ><input
            id="password"
            v-model="password"
            name="password"
            type="password"
            autocomplete="current-password"
            required
            maxlength="72"
            placeholder="输入账号密码"
          />
          <p v-if="loginError" class="form-error" role="alert">
            {{ loginError }}
          </p>
          <button
            class="button primary full login-submit"
            :disabled="loginBusy"
          >
            <LoaderCircle v-if="loginBusy" class="spin" :size="17" />{{
              loginBusy ? "正在登录…" : "进入我的图库"
            }}<ArrowRight v-if="!loginBusy" :size="17" />
          </button>
        </form>
        <p class="login-footnote">账号由管理员创建 · 未开放注册</p>
      </div>
      <span class="login-version">QPix / YOUR IMAGES, YOUR SPACE.</span>
    </section>
  </div>
  <div v-else class="app-shell">
    <div v-if="mobileNav" class="nav-scrim" @click="mobileNav = false" />
    <aside class="sidebar" :class="{ open: mobileNav }">
      <a href="#" class="brand" @click.prevent="go('library')"
        ><img src="/favicon.svg" alt="" /><span
          >QPix<span class="brand-dot">.</span></span
        ><span class="personal-label">PERSONAL</span></a
      >
      <button class="button primary sidebar-upload" @click="go('upload')">
        <Plus :size="18" />上传图片<span>↑</span>
      </button>
      <div class="nav-label">图片空间</div>
      <nav aria-label="图片空间导航">
        <button
          v-for="item in workspaceNavItems"
          :key="item.key"
          class="nav-item"
          :class="{
            active:
              view === item.key && (item.key !== 'library' || !albumFilter),
          }"
          @click="go(item.key)"
        >
          <component :is="item.icon" :size="19" :stroke-width="1.8" /><span>{{
            item.label
          }}</span
          ><span v-if="item.key === 'library'" class="nav-count">{{
            statistics.images
          }}</span>
        </button>
      </nav>
      <div class="nav-label album-nav-heading">
        我的相册<button
          class="icon-button"
          aria-label="新建相册"
          @click="openAlbum()"
        >
          <Plus :size="15" />
        </button>
      </div>
      <div class="album-nav">
        <button
          v-for="(album, index) in albums.slice(0, 6)"
          :key="album.id"
          class="nav-item small"
          :class="{ active: view === 'library' && albumFilter === album.id }"
          @click="go('library', album.id)"
        >
          <span class="album-dot" :class="`color-${index % 4}`" /><span
            class="truncate"
            >{{ album.name }}</span
          ><span class="nav-count">{{ album.count }}</span></button
        ><button
          v-if="albums.length > 6"
          class="text-button see-albums"
          @click="go('library')"
        >
          查看全部相册<ChevronRight :size="14" />
        </button>
      </div>
      <div v-if="isAdmin" class="nav-label admin-nav-heading">系统管理</div>
      <nav v-if="isAdmin" aria-label="系统管理导航">
        <button
          v-for="item in adminNavItems"
          :key="item.key"
          class="nav-item"
          :class="{ active: view === item.key }"
          @click="go(item.key)"
        >
          <component :is="item.icon" :size="19" :stroke-width="1.8" />
          <span>{{ item.label }}</span>
        </button>
      </nav>
      <div class="sidebar-bottom">
        <div class="storage-card">
          <div>
            <HardDrive :size="16" /><span>{{
              settings?.storage_name || "图片存储"
            }}</span
            ><span class="status-dot" />
          </div>
          <strong
            >{{ bytes(uploadOptions?.used_bytes || 0)
            }}<span>图片用量</span></strong
          >
          <p>
            容量：{{
              uploadOptions?.quota_bytes
                ? bytes(uploadOptions.quota_bytes)
                : "不限"
            }}
          </p>
          <p>{{ statistics.images }} 张图片，妥善收藏。</p>
        </div>
        <div class="user-card">
          <span class="avatar">{{
            user.username.slice(0, 1).toUpperCase()
          }}</span>
          <div>
            <strong>{{ user.username }}</strong
            ><span>{{ isAdmin ? "管理员" : "普通用户" }}</span>
          </div>
          <button
            class="icon-button"
            aria-label="退出登录"
            title="退出登录"
            @click="logout"
          >
            <LogOut :size="17" />
          </button>
        </div>
      </div>
    </aside>
    <div class="main-area">
      <header class="topbar">
        <div class="breadcrumb">
          <button
            class="icon-button mobile-menu"
            aria-label="打开导航"
            @click="mobileNav = true"
          >
            <Menu :size="21" /></button
          ><span>工作空间</span><ChevronRight :size="14" /><strong>{{
            title
          }}</strong>
        </div>
        <div class="topbar-right">
          <details class="account-menu">
            <summary>
              <span class="avatar">{{
                user.username.slice(0, 1).toUpperCase()
              }}</span
              >{{ user.username }}
            </summary>
            <div>
              <button
                class="nav-item"
                @click="
                  go('security');
                  ($event.currentTarget as HTMLElement)
                    .closest('details')
                    ?.removeAttribute('open');
                "
              >
                <KeyRound :size="16" />账号安全</button
              ><button class="nav-item" @click="logout">
                <LogOut :size="16" />退出登录
              </button>
            </div>
          </details>
        </div>
      </header>
      <main class="main-content">
        <div class="compact-page-heading">
          <h1>{{ title }}</h1>
          <span v-if="view === 'library'" class="muted"
            >{{ statistics.images }} 张图片 · 已用
            {{ bytes(uploadOptions?.used_bytes || 0) }} /
            {{
              uploadOptions?.quota_bytes
                ? bytes(uploadOptions.quota_bytes)
                : "不限容量"
            }}
            · 单张上限 {{ bytes(uploadOptions?.max_upload_bytes || 0) }}</span
          >
        </div>
        <LibraryBrowser
          v-if="view === 'library' || view === 'manageImages'"
          :key="view"
          :all="view === 'manageImages'"
          :album-i-d="albumFilter"
          :version="version"
          :accounts="accountRows"
          @folder="(id) => (albumFilter = id)"
          @upload="uploadTo"
          @changed="refreshMeta"
          @notice="notice"
          @share="share"
        />
        <UploadPanel
          v-show="view === 'upload'"
          ref="uploader"
          :albums="albums"
          :initial-album="uploadAlbum"
          :max-bytes="uploadOptions?.max_upload_bytes || 20 * 1024 * 1024"
          :storages="uploadStores"
          :default-storage="uploadOptions?.default_storage || 'local'"
          :default-compression="uploadOptions?.processing.mode || 'none'"
          :default-quality="uploadOptions?.processing.quality ?? 82"
          @uploaded="uploaded"
          @notice="notice"
        />
        <ResourceList
          v-if="
            ['tokens', 'manageTokens', 'shares', 'manageShares'].includes(view)
          "
          :key="view"
          :kind="
            view === 'tokens' || view === 'manageTokens' ? 'tokens' : 'shares'
          "
          :all="view === 'manageTokens' || view === 'manageShares'"
          :accounts="accountRows"
          :can-create-token="view === 'tokens'"
          :target="shareTarget"
          @notice="notice"
        />
        <StorageSettings
          v-if="view === 'storages' && isAdmin"
          @changed="refreshMeta"
          @notice="notice"
        />
        <ProcessingSettings
          v-if="view === 'processing' && isAdmin"
          @changed="refreshMeta"
          @notice="notice"
        />
        <AccessSettings v-if="view === 'access' && isAdmin" @notice="notice" />
        <UserSettings
          v-if="view === 'users' && isAdmin"
          :server-limit="settings?.max_upload_bytes || 20 * 1024 * 1024"
          :current-user-id="user?.id || ''"
          @changed="refreshMeta"
          @notice="notice"
        />
        <section v-if="view === 'settings' && isAdmin" class="settings-layout">
          <div class="settings-main">
            <SiteSettings
              v-if="settings"
              :settings="settings"
              @changed="refreshMeta"
              @reauthenticate="clearSession"
              @notice="notice"
            />
            <form
              v-if="isAdmin"
              class="panel settings-panel"
              @submit.prevent="saveSettings"
            >
              <h3>图片访问</h3>
              <p class="muted">为你的图片设置一个独立的访问域名。</p>
              <label class="field-label" for="public-url">图片访问域名</label
              ><input
                id="public-url"
                v-model="publicURL"
                type="url"
                placeholder="留空使用当前站点，例如 https://img.example.com"
              />
              <p class="field-help">
                填写后，新复制的外链将使用此域名。请先将域名反向代理到 QPix 的
                /i/ 路径。
              </p>
              <div class="panel-actions">
                <button class="button primary" :disabled="settingsBusy">
                  {{ settingsBusy ? "正在保存…" : "保存设置" }}
                </button>
              </div>
            </form>
            <form
              v-if="isAdmin"
              class="panel settings-panel"
              @submit.prevent="saveUploadLimit"
            >
              <h3>上传限制</h3>
              <p class="muted">
                设置全站单张图片的上传上限，保存后对新上传生效。
              </p>
              <label class="field-label" for="server-upload-limit"
                >服务器单张上限（MiB）</label
              >
              <input
                id="server-upload-limit"
                v-model.number="serverUploadMiB"
                type="number"
                min="1"
                max="100"
                step="1"
                required
                :disabled="uploadLimitBusy"
              />
              <p class="field-help">
                可设置 1–100 MiB。用户单张上限为 0
                时跟随这里的设置；设置了独立上限时，取两者中较小的值。降低上限不会删除已有图片。
              </p>
              <label class="field-label" for="server-pixels"
                >像素上限（百万像素）</label
              >
              <input
                id="server-pixels"
                v-model.number="serverPixels"
                type="number"
                min="1"
                max="100"
                step="1"
                required
                :disabled="uploadLimitBusy"
              />
              <p class="field-help">
                允许 1–100 百万像素，用于限制解码占用，对新上传生效。
              </p>
              <div class="panel-actions">
                <button class="button primary" :disabled="uploadLimitBusy">
                  {{ uploadLimitBusy ? "正在保存…" : "保存上传限制" }}
                </button>
              </div>
            </form>
          </div>
          <aside v-if="isAdmin">
            <div class="panel system-panel">
              <h3>轻一点，刚刚好。</h3>
              <dl>
                <div>
                  <dt><Database :size="15" />数据库</dt>
                  <dd>{{ storageLabel }}</dd>
                </div>
                <div>
                  <dt><HardDrive :size="15" />存储驱动</dt>
                  <dd>
                    {{
                      settings?.storage === "s3" ? "S3 兼容存储" : "本地磁盘"
                    }}
                  </dd>
                </div>
                <div>
                  <dt><ArrowDownToLine :size="15" />单张上限</dt>
                  <dd>{{ bytes(settings?.max_upload_bytes || 0) }}</dd>
                </div>
                <div>
                  <dt><ImageIcon :size="15" />像素上限</dt>
                  <dd>{{ (settings?.max_pixels || 0) / 10000 }} 万</dd>
                </div>
              </dl>
              <p class="field-help">
                数据库通过服务端配置管理。可在存储管理中配置本地目录和 S3 存储。
              </p>
            </div>
            <div class="backup-note">
              <ShieldCheck :size="20" />
              <div>
                <strong>让收藏更安心</strong>
                <p>定期备份数据库、图片、配置和主密钥，并验证可以恢复。</p>
              </div>
            </div>
          </aside>
        </section>
        <section v-if="view === 'security'" class="account-security">
          <form class="panel settings-panel" @submit.prevent="changePassword">
            <h3>账号安全</h3>
            <p class="muted">
              修改密码后，全部登录会话会失效，需要重新登录；上传 Token
              保持有效。
            </p>
            <label class="field-label" for="current-password">当前密码</label
            ><input
              id="current-password"
              v-model="currentPassword"
              type="password"
              autocomplete="current-password"
              required
            /><label class="field-label" for="new-password">新密码</label
            ><input
              id="new-password"
              v-model="newPassword"
              type="password"
              autocomplete="new-password"
              maxlength="72"
              required
              placeholder="输入新密码，最多 72 字节"
            />
            <label class="field-label" for="confirm-new-password"
              >确认新密码</label
            >
            <input
              id="confirm-new-password"
              v-model="confirmNewPassword"
              type="password"
              autocomplete="new-password"
              maxlength="72"
              required
              placeholder="请再次输入新密码"
              :aria-invalid="passwordsMismatch"
              :aria-describedby="
                passwordsMismatch ? 'password-mismatch' : undefined
              "
            />
            <p
              v-if="passwordsMismatch"
              id="password-mismatch"
              class="field-help error-text"
              role="alert"
            >
              两次输入的新密码不一致
            </p>
            <div class="panel-actions">
              <button
                class="button"
                :disabled="passwordBusy || passwordsMismatch"
              >
                {{ passwordBusy ? "正在修改…" : "修改密码" }}
              </button>
            </div>
          </form>
        </section>
        <footer class="page-footer">
          <span>QPix · YOUR IMAGES, YOUR SPACE.</span
          ><span>简简单单，收好灵感。<span class="footer-dot" /></span>
        </footer>
      </main>
    </div>
  </div>

  <Modal
    v-if="modal === 'album'"
    :title="editingAlbum ? '重命名相册' : '新建相册'"
    :busy="modalBusy"
    @close="closeModal"
    ><form class="modal-body" @submit.prevent="saveAlbum">
      <p class="muted">给相近的图片一个共同的归处。</p>
      <label class="field-label" for="album-name">相册名称</label
      ><input
        id="album-name"
        v-model="albumDraft"
        required
        maxlength="100"
        placeholder="例如：生活碎片、设计灵感"
      />
      <p v-if="modalError" class="form-error" role="alert">{{ modalError }}</p>
      <div class="modal-actions">
        <button
          type="button"
          class="button"
          :disabled="modalBusy"
          @click="closeModal"
        >
          取消</button
        ><button class="button primary" :disabled="modalBusy">
          {{ modalBusy ? "正在保存…" : "保存相册" }}
        </button>
      </div>
    </form></Modal
  >
  <div class="toast-stack" aria-live="polite">
    <div
      v-for="item in notifications"
      :key="item.id"
      class="toast"
      :class="{ error: item.error }"
    >
      <CircleAlert v-if="item.error" :size="18" /><Check
        v-else
        :size="18"
      /><span>{{ item.text }}</span
      ><button
        class="icon-button"
        aria-label="关闭通知"
        @click="
          notifications = notifications.filter((row) => row.id !== item.id)
        "
      >
        <X :size="14" />
      </button>
    </div>
  </div>
</template>
