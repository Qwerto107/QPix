<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import {
  ArrowDownToLine,
  ArrowLeft,
  ArrowRight,
  ArrowUpRight,
  Check,
  ChevronRight,
  CircleAlert,
  Copy,
  Database,
  Folder,
  FolderPlus,
  HardDrive,
  Image as ImageIcon,
  Images,
  KeyRound,
  LayoutGrid,
  List,
  LoaderCircle,
  LogOut,
  Menu,
  Plus,
  Search,
  Settings as SettingsIcon,
  ShieldCheck,
  Sparkles,
  Trash2,
  Upload,
  X,
} from "@lucide/vue";
import Modal from "./components/Modal.vue";
import UploadPanel from "./components/UploadPanel.vue";
import ImageDetail from "./components/ImageDetail.vue";
import StorageSettings from "./components/StorageSettings.vue";
import ProcessingSettings from "./components/ProcessingSettings.vue";
import AccessSettings from "./components/AccessSettings.vue";
import UserSettings from "./components/UserSettings.vue";
import AlbumAccess from "./components/AlbumAccess.vue";
import ShareSettings from "./components/ShareSettings.vue";
import PublicGallery from "./components/PublicGallery.vue";
import { api, APIError, onSessionExpired, setCSRF } from "./api";
import type {
  Album,
  APIToken,
  ImageItem,
  ImagePage,
  Settings,
  Stats,
  User,
  StorageProfile,
  UploadOptions,
} from "./types";
import {
  absolute,
  bytes,
  copy,
  date,
  imageLink,
  message,
  type LinkFormat,
} from "./utils";

type View =
  | "library"
  | "upload"
  | "albums"
  | "tokens"
  | "settings"
  | "storages"
  | "processing"
  | "access"
  | "users"
  | "shares";
const publicPage = window.location.pathname.startsWith("/s/");
const shareToken = window.location.pathname.startsWith("/s/")
  ? window.location.pathname.split("/")[2]
  : undefined;
const user = ref<User | null>(null);
const isAdmin = computed(() => user.value?.role === "admin");
const initializing = ref(true);
const initializationError = ref("");
const username = ref("admin");
const password = ref("");
const loginError = ref("");
const loginBusy = ref(false);
const view = ref<View>("library");
const mobileNav = ref(false);
const albums = ref<Album[]>([]);
const tokens = ref<APIToken[]>([]);
const statistics = ref<Stats>({
  images: 0,
  bytes: 0,
  albums: 0,
  unfiled: 0,
  today: 0,
  pending_deletions: 0,
});
const settings = ref<Settings | null>(null);
const storages = ref<StorageProfile[]>([]);
const uploadOptions = ref<UploadOptions | null>(null);
const accountRows = ref<User[]>([]);
const ownerFilter = ref("");
const mineOnly = ref(false);
const albumAccess = ref<Album | null>(null);
const shareTarget = ref<{
  kind: "image" | "album";
  id: string;
  name: string;
} | null>(null);
const uploadStores = computed(
  () =>
    uploadOptions.value?.storages.map((row) => ({ ...row, enabled: true })) ||
    [],
);
const images = ref<ImageItem[]>([]);
const total = ref(0);
const page = ref(1);
const limit = 36;
const search = ref("");
const albumFilter = ref("");
const loading = ref(false);
const loadError = ref("");
const layout = ref<"grid" | "list">("grid");
const selected = ref(new Set<string>());
const brokenImages = ref(new Set<string>());
const detail = ref<ImageItem | null>(null);
const uploader = ref<InstanceType<typeof UploadPanel>>();
const copiedID = ref("");
const notifications = ref<{ id: number; text: string; error?: boolean }[]>([]);
let notificationID = 0;
const timers = new Set<ReturnType<typeof setTimeout>>();
function later(fn: () => void, duration: number) {
  const timer = setTimeout(() => {
    timers.delete(timer);
    fn();
  }, duration);
  timers.add(timer);
  return timer;
}
function notice(text: string, error = false) {
  const id = ++notificationID;
  notifications.value.push({ id, text, error });
  later(() => {
    notifications.value = notifications.value.filter((item) => item.id !== id);
  }, 4500);
}

const activeAlbum = computed(() =>
  albums.value.find((album) => album.id === albumFilter.value),
);
const title = computed(
  () =>
    ({
      library:
        activeAlbum.value?.name ||
        (albumFilter.value === "unfiled" ? "未分类图片" : "我的图库"),
      upload: "上传图片",
      albums: "我的相册",
      tokens: "上传 Token",
      settings: "偏好设置",
      storages: "存储管理",
      processing: "图片处理",
      access: "访问控制",
      users: "用户管理",
      shares: "分享管理",
    })[view.value],
);
const allSelected = computed(
  () =>
    images.value.length > 0 &&
    images.value.every((image) => selected.value.has(image.id)),
);
const selectedImages = computed(() =>
  images.value.filter((image) => selected.value.has(image.id)),
);
const pages = computed(() => Math.max(1, Math.ceil(total.value / limit)));
const storageLabel = computed(
  () =>
    ({ sqlite: "SQLite", postgres: "PostgreSQL", mysql: "MySQL" })[
      settings.value?.database || "sqlite"
    ],
);
const workspaceNavItems = [
  { key: "library" as View, label: "我的图库", icon: Images },
  { key: "upload" as View, label: "上传图片", icon: Upload },
  { key: "albums" as View, label: "我的相册", icon: Folder },
];
const adminNavItems = computed(() =>
  [
    { key: "users" as View, label: "用户管理", icon: ShieldCheck },
    { key: "shares" as View, label: "分享管理", icon: ArrowUpRight },
    { key: "storages" as View, label: "存储管理", icon: HardDrive },
    { key: "processing" as View, label: "图片处理", icon: Sparkles },
    { key: "access" as View, label: "访问控制", icon: ShieldCheck },
    { key: "tokens" as View, label: "上传 Token", icon: KeyRound },
    { key: "settings" as View, label: "偏好设置", icon: SettingsIcon },
  ].filter(
    (item) => isAdmin.value || item.key === "tokens" || item.key === "settings",
  ),
);

function go(next: View, album = "") {
  view.value = next;
  albumFilter.value = album;
  page.value = 1;
  selected.value = new Set();
  mobileNav.value = false;
  if (next === "library") void loadImages();
  if (next === "tokens") void loadTokens();
  if (next === "shares") shareTarget.value = null;
}
function share(kind: "album" | "image", id: string, name: string) {
  detail.value = null;
  go("shares");
  shareTarget.value = { kind, id, name };
}
async function initialize() {
  initializing.value = true;
  initializationError.value = "";
  try {
    const data = await api<{ user: User; csrf_token: string }>("/auth/me");
    user.value = data.user;
    setCSRF(data.csrf_token);
    await refresh();
  } catch (error) {
    if (!(error instanceof APIError && error.status === 401))
      initializationError.value = message(error);
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
    await refresh();
  } catch (error) {
    loginError.value = message(error);
  } finally {
    loginBusy.value = false;
  }
}
function clearSession() {
  imageRequest++;
  user.value = null;
  setCSRF("");
  images.value = [];
  albums.value = [];
  tokens.value = [];
  storages.value = [];
  uploadOptions.value = null;
  settings.value = null;
  accountRows.value = [];
  albumAccess.value = null;
  shareTarget.value = null;
  mineOnly.value = false;
  ownerFilter.value = "";
  selected.value = new Set();
  detail.value = null;
  modal.value = "";
  newToken.value = "";
  currentPassword.value = "";
  newPassword.value = "";
  confirmNewPassword.value = "";
  view.value = "library";
}
async function logout() {
  try {
    await api("/auth/logout", "POST");
    clearSession();
  } catch (error) {
    notice(message(error), true);
  }
}
onSessionExpired(() => {
  if (user.value) {
    clearSession();
    notice("登录已失效，请重新登录", true);
  }
});

let imageRequest = 0;
async function loadImages() {
  const request = ++imageRequest;
  loading.value = true;
  loadError.value = "";
  selected.value = new Set();
  const params = new URLSearchParams({
    page: `${page.value}`,
    limit: `${limit}`,
    q: search.value,
    album_id: albumFilter.value,
    mine: String(mineOnly.value),
    user_id: isAdmin.value ? ownerFilter.value : "",
  });
  try {
    const data = await api<ImagePage>(`/images?${params}`);
    // 忽略较早的搜索响应，避免慢请求覆盖最新筛选结果。
    if (request !== imageRequest || !user.value) return;
    images.value = data.items;
    total.value = data.total;
    if (page.value > pages.value) {
      page.value = pages.value;
      await loadImages();
    }
  } catch (error) {
    if (request === imageRequest) loadError.value = message(error);
  } finally {
    if (request === imageRequest) loading.value = false;
  }
}
async function refreshMeta() {
  const identity = user.value?.id;
  const [albumRows, stats, options] = await Promise.all([
    api<Album[]>("/albums"),
    api<Stats>("/stats"),
    api<UploadOptions>("/upload-options"),
  ]);
  if (!user.value || user.value.id !== identity) return;
  albums.value = albumRows;
  statistics.value = stats;
  uploadOptions.value = options;
  if (!isAdmin.value) return;
  const [configuration, storageRows, accounts] = await Promise.all([
    api<Settings>("/settings"),
    api<StorageProfile[]>("/storages"),
    api<User[]>("/users"),
  ]);
  if (!user.value || user.value.id !== identity) return;
  const editingDomain =
    settings.value && publicURL.value !== settings.value.public_url;
  const editingUploadLimit =
    settings.value &&
    serverUploadMiB.value !== settings.value.max_upload_bytes / 2 ** 20;
  albums.value = albumRows;
  statistics.value = stats;
  settings.value = configuration;
  storages.value = storageRows;
  accountRows.value = accounts;
  if (!editingDomain) publicURL.value = configuration.public_url;
  if (!editingUploadLimit)
    serverUploadMiB.value = configuration.max_upload_bytes / 2 ** 20;
}
async function refresh() {
  try {
    await Promise.all([refreshMeta(), loadImages()]);
  } catch (error) {
    notice(message(error), true);
  }
}
async function loadTokens() {
  try {
    tokens.value = await api<APIToken[]>("/tokens");
  } catch (error) {
    notice(message(error), true);
  }
}
let searchTimer: ReturnType<typeof setTimeout> | undefined;
watch(search, () => {
  if (searchTimer) clearTimeout(searchTimer);
  searchTimer = later(() => {
    page.value = 1;
    void loadImages();
  }, 300);
});
function changePage(next: number) {
  page.value = next;
  void loadImages();
  window.scrollTo({ top: 0, behavior: "smooth" });
}
function selectImage(id: string) {
  const next = new Set(selected.value);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  selected.value = next;
}
function selectAll() {
  selected.value = allSelected.value
    ? new Set()
    : new Set(images.value.map((image) => image.id));
}
async function copyImages(format: LinkFormat, rows = selectedImages.value) {
  try {
    await copy(rows.map((image) => imageLink(image, format)).join("\n"));
    notice(`已复制 ${rows.length} 张图片的链接`);
    return true;
  } catch {
    notice("复制失败，请检查浏览器剪贴板权限", true);
    return false;
  }
}
async function copyOne(image: ImageItem) {
  if (!(await copyImages("url", [image]))) return;
  copiedID.value = image.id;
  later(() => {
    copiedID.value = "";
  }, 1500);
}
function imageBroken(id: string) {
  brokenImages.value = new Set([...brokenImages.value, id]);
}

const modal = ref<"" | "album" | "confirm" | "token">("");
const modalBusy = ref(false);
const modalError = ref("");
const albumDraft = ref("");
const editingAlbum = ref<Album | null>(null);
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
    notice(editingAlbum.value ? "相册已重命名" : "相册已创建");
  } catch (error) {
    modalError.value = message(error);
  } finally {
    modalBusy.value = false;
  }
}
const confirmTitle = ref("");
const confirmBody = ref("");
let confirmAction: () => Promise<void> = async () => {};
function confirm(title: string, body: string, action: () => Promise<void>) {
  detail.value = null;
  confirmTitle.value = title;
  confirmBody.value = body;
  confirmAction = action;
  modalError.value = "";
  modal.value = "confirm";
}
async function runConfirm() {
  modalBusy.value = true;
  modalError.value = "";
  try {
    await confirmAction();
    modal.value = "";
  } catch (error) {
    modalError.value = message(error);
  } finally {
    modalBusy.value = false;
  }
}
function removeImages(rows: ImageItem[]) {
  const ids = rows.map((image) => image.id);
  confirm(
    `删除 ${ids.length} 张图片？`,
    "图片和缩略图将永久删除，已有外链也会失效。此操作无法撤销。",
    async () => {
      const result = await api<{ deleted: number; failed: string[] }>(
        "/images/batch-delete",
        "POST",
        { ids },
      );
      await refresh();
      if (result.failed.length)
        notice(
          `${result.deleted} 张已删除；${result.failed.length} 张等待后台重试清理`,
          true,
        );
      else notice(`已删除 ${result.deleted} 张图片`);
    },
  );
}
function removeAlbum(album: Album) {
  confirm(
    `删除相册“${album.name}”？`,
    "相册内图片将回到各上传者的私有未分类空间，相册的访客、成员和相册分享将失去访问权限。",
    async () => {
      await api(`/albums/${album.id}`, "DELETE");
      if (albumFilter.value === album.id) albumFilter.value = "";
      await refresh();
      notice("相册已删除，图片已保留");
    },
  );
}
function revokeToken(token: APIToken) {
  confirm(
    `撤销 Token“${token.name}”？`,
    "使用此 Token 的上传客户端将无法继续上传。",
    async () => {
      await api(`/tokens/${token.id}`, "DELETE");
      await loadTokens();
      notice("Token 已撤销");
    },
  );
}

const tokenName = ref("");
const tokenDays = ref("90");
const tokenExpires = ref("");
const newToken = ref("");
function openToken() {
  tokenName.value = "";
  tokenDays.value = "90";
  tokenExpires.value = "";
  newToken.value = "";
  modalError.value = "";
  modal.value = "token";
}
async function createToken() {
  modalBusy.value = true;
  modalError.value = "";
  try {
    const data = await api<{ token: string }>("/tokens", "POST", {
      name: tokenName.value,
      expires_at:
        tokenDays.value === "permanent"
          ? null
          : tokenDays.value === "custom"
            ? new Date(tokenExpires.value).toISOString()
            : new Date(
                Date.now() + Number(tokenDays.value) * 86400000,
              ).toISOString(),
    });
    newToken.value = data.token;
    await loadTokens();
  } catch (error) {
    modalError.value = message(error);
  } finally {
    modalBusy.value = false;
  }
}
async function copyText(value: string) {
  try {
    await copy(value);
    notice("已复制到剪贴板");
  } catch {
    notice("复制失败，请检查浏览器剪贴板权限", true);
  }
}
function closeModal() {
  if (modalBusy.value) return;
  modal.value = "";
  newToken.value = "";
}
const publicURL = ref("");
const settingsBusy = ref(false);
const serverUploadMiB = ref(20);
const uploadLimitBusy = ref(false);
const currentPassword = ref("");
const newPassword = ref("");
const confirmNewPassword = ref("");
const passwordsMismatch = computed(
  () =>
    confirmNewPassword.value !== "" &&
    newPassword.value !== confirmNewPassword.value,
);
const passwordBusy = ref(false);
async function saveSettings() {
  settingsBusy.value = true;
  try {
    await api("/settings", "PATCH", { public_url: publicURL.value });
    await refresh();
    notice("图片访问域名已保存");
  } catch (error) {
    notice(message(error), true);
  } finally {
    settingsBusy.value = false;
  }
}
async function saveUploadLimit() {
  uploadLimitBusy.value = true;
  try {
    await api("/settings", "PATCH", {
      max_upload_bytes: Math.round(serverUploadMiB.value * 2 ** 20),
    });
    await refresh();
    notice("服务器单张上传上限已保存");
  } catch (error) {
    notice(message(error), true);
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
    notice("密码已修改，所有会话和上传 Token 已失效，请重新登录");
  } catch (error) {
    notice(message(error), true);
  } finally {
    passwordBusy.value = false;
  }
}

async function receiveFiles(files: FileList | File[]) {
  if (!user.value) return;
  view.value = "upload";
  mobileNav.value = false;
  await nextTick();
  uploader.value?.addFiles(files);
}
function paste(event: ClipboardEvent) {
  const files = event.clipboardData?.files;
  if (files?.length && user.value) {
    event.preventDefault();
    void receiveFiles(files);
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
  uploadRefresh = later(() => void refresh(), 250);
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
          v-if="isAdmin"
          class="icon-button"
          aria-label="新建相册"
          @click="openAlbum()"
        >
          <Plus :size="15" />
        </button>
      </div>
      <div class="album-nav">
        <button
          class="nav-item small"
          :class="{ active: view === 'library' && albumFilter === 'unfiled' }"
          @click="go('library', 'unfiled')"
        >
          <span class="album-dot neutral" /><span>未分类</span
          ><span class="nav-count">{{ statistics.unfiled }}</span></button
        ><button
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
          @click="go('albums')"
        >
          查看全部相册<ChevronRight :size="14" />
        </button>
      </div>
      <div class="nav-label admin-nav-heading">
        {{ isAdmin ? "系统管理" : "账号设置" }}
      </div>
      <nav aria-label="系统管理导航">
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
            >{{
              bytes(
                isAdmin ? statistics.bytes : uploadOptions?.used_bytes || 0,
              )
            }}<span>图片用量</span></strong
          >
          <p v-if="!isAdmin">
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
          <span class="private-badge"><ShieldCheck :size="14" />私人图床</span
          ><span class="topbar-divider" /><span class="topbar-version"
            >v{{ settings?.version || "0.3.0" }}</span
          >
        </div>
      </header>
      <main class="main-content">
        <div class="page-heading">
          <div>
            <span class="eyebrow">{{
              view === "library"
                ? "YOUR PERSONAL COLLECTION"
                : view === "upload"
                  ? "MAKE ROOM FOR INSPIRATION"
                  : view === "albums"
                    ? "A PLACE FOR EVERYTHING"
                    : view === "tokens"
                      ? "CONNECT YOUR WORKFLOW"
                      : "MAKE IT YOURS"
            }}</span>
            <h1>
              {{ title
              }}<span v-if="view === 'library'" class="title-count">{{
                total
              }}</span>
            </h1>
            <p>
              {{
                view === "library"
                  ? "收好每一张图片，让分享更简单。"
                  : view === "upload"
                    ? "从一次拖拽开始，把灵感留在这里。"
                    : view === "albums"
                      ? "用相册整理图片，也整理你的灵感。"
                      : view === "tokens"
                        ? "让脚本和上传工具安全地连接到 QPix。"
                        : "让这个小小的图片空间，更合你的习惯。"
              }}
            </p>
          </div>
          <button
            v-if="view === 'library'"
            class="button primary"
            @click="go('upload')"
          >
            <Plus :size="17" />上传图片</button
          ><button
            v-if="view === 'albums' && isAdmin"
            class="button primary"
            @click="openAlbum()"
          >
            <FolderPlus :size="17" />新建相册</button
          ><button
            v-if="view === 'tokens'"
            class="button primary"
            @click="openToken"
          >
            <Plus :size="17" />创建 Token
          </button>
        </div>

        <section v-if="view === 'library'" aria-label="图片库">
          <div class="stats-grid">
            <div class="stat-card">
              <span class="stat-icon mint"><Images :size="20" /></span>
              <div>
                <span>收藏图片</span
                ><strong>{{ statistics.images }}<small>张</small></strong>
              </div>
              <span class="stat-decoration">01</span>
            </div>
            <div class="stat-card">
              <span class="stat-icon sand"><Folder :size="20" /></span>
              <div>
                <span>我的相册</span
                ><strong>{{ statistics.albums }}<small>个</small></strong>
              </div>
              <span class="stat-decoration">02</span>
            </div>
            <div class="stat-card">
              <span class="stat-icon lilac"><HardDrive :size="20" /></span>
              <div>
                <span>图片存储</span
                ><strong class="storage-stat">{{
                  bytes(statistics.bytes)
                }}</strong>
              </div>
              <span class="stat-decoration">03</span>
            </div>
          </div>
          <div v-if="statistics.pending_deletions" class="info-banner">
            <CircleAlert :size="17" /><span
              >{{
                statistics.pending_deletions
              }}
              张图片等待后台清理，外链已停用。</span
            >
          </div>
          <div class="gallery-toolbar">
            <div class="search-field">
              <Search :size="17" /><input
                v-model="search"
                aria-label="搜索图片名称"
                placeholder="搜索图片名称…"
              /><button
                v-if="search"
                class="icon-button"
                aria-label="清空搜索"
                @click="search = ''"
              >
                <X :size="15" />
              </button>
            </div>
            <div class="gallery-filters">
              <select
                v-if="isAdmin"
                v-model="ownerFilter"
                aria-label="筛选上传者"
                @change="
                  page = 1;
                  loadImages();
                "
              >
                <option value="">所有上传者</option>
                <option
                  v-for="account in accountRows"
                  :key="account.id"
                  :value="account.id"
                >
                  {{ account.username }}
                </option>
              </select>
              <label v-else class="check-field mine-filter"
                ><input
                  v-model="mineOnly"
                  type="checkbox"
                  @change="
                    page = 1;
                    loadImages();
                  "
                />仅看我上传的图片</label
              >
              <div class="select-wrap">
                <Folder :size="15" /><select
                  :value="albumFilter"
                  aria-label="筛选相册"
                  @change="
                    go('library', ($event.target as HTMLSelectElement).value)
                  "
                >
                  <option value="">全部相册</option>
                  <option value="unfiled">未分类</option>
                  <option
                    v-for="album in albums"
                    :key="album.id"
                    :value="album.id"
                  >
                    {{ album.name }}
                  </option>
                </select>
              </div>
              <div class="view-toggle">
                <button
                  :class="{ active: layout === 'grid' }"
                  aria-label="网格视图"
                  :aria-pressed="layout === 'grid'"
                  @click="layout = 'grid'"
                >
                  <LayoutGrid :size="17" /></button
                ><button
                  :class="{ active: layout === 'list' }"
                  aria-label="列表视图"
                  :aria-pressed="layout === 'list'"
                  @click="layout = 'list'"
                >
                  <List :size="18" />
                </button>
              </div>
            </div>
          </div>
          <div class="collection-heading">
            <label class="selection-label"
              ><input
                type="checkbox"
                :checked="allSelected"
                :disabled="!images.length"
                @change="selectAll"
              /><span>{{
                selected.size ? `已选择 ${selected.size} 张` : "全部选择"
              }}</span></label
            ><span class="muted sort-label">按上传时间排序 ↓</span>
          </div>
          <div v-if="selected.size" class="selection-toolbar">
            <span>{{ selected.size }} 张图片</span
            ><button class="text-button" @click="copyImages('url')">
              <Copy :size="15" />复制直链</button
            ><button class="text-button" @click="copyImages('markdown')">
              Markdown</button
            ><button class="text-button" @click="copyImages('html')">
              HTML</button
            ><button
              v-if="selectedImages.every((image) => image.can_manage)"
              class="text-button danger-text"
              @click="removeImages(selectedImages)"
            >
              <Trash2 :size="15" />删除</button
            ><button
              class="icon-button selection-close"
              aria-label="取消选择"
              @click="selected = new Set()"
            >
              <X :size="17" />
            </button>
          </div>
          <div v-if="loadError" class="empty-state panel" role="alert">
            <CircleAlert :size="32" />
            <h3>暂时无法加载图库</h3>
            <p>{{ loadError }}</p>
            <button class="button" @click="loadImages">重新加载</button>
          </div>
          <div v-else-if="loading" class="image-grid">
            <div v-for="n in 8" :key="n" class="skeleton-card">
              <div />
              <span />
            </div>
          </div>
          <div v-else-if="!images.length" class="gallery-empty">
            <div class="empty-illustration">
              <ImageIcon :size="42" :stroke-width="1.2" /><span
                class="empty-plus"
                ><Plus :size="16"
              /></span>
            </div>
            <span class="eyebrow">{{
              search || albumFilter
                ? "A LITTLE QUIET HERE"
                : "YOUR COLLECTION STARTS HERE"
            }}</span>
            <h2>
              {{
                search
                  ? "没有找到这张图片"
                  : albumFilter
                    ? "这个相册还很安静"
                    : "第一张图片，从这里开始"
              }}
            </h2>
            <p>
              {{
                search
                  ? "换个关键词试试，或查看全部图片。"
                  : "拖拽、粘贴，或点击上传，把值得收藏的画面放进来。"
              }}
            </p>
            <button
              class="button primary"
              @click="search ? (search = '') : go('upload')"
            >
              <component :is="search ? Search : Plus" :size="17" />{{
                search ? "清空搜索" : "上传第一张图片"
              }}
            </button>
            <div class="empty-formats">
              JPG <span>·</span> PNG <span>·</span> GIF <span>·</span> WebP
            </div>
          </div>
          <div
            v-else
            class="image-grid"
            :class="{ 'list-view': layout === 'list' }"
          >
            <article
              v-for="image in images"
              :key="image.id"
              class="image-card"
              :class="{ selected: selected.has(image.id) }"
            >
              <div class="image-card-visual">
                <button
                  class="image-open"
                  :aria-label="`查看 ${image.name}`"
                  @click="detail = image"
                >
                  <img
                    v-if="!brokenImages.has(image.id)"
                    :src="image.thumbnail_url"
                    :alt="image.name"
                    loading="lazy"
                    @error="imageBroken(image.id)"
                  /><ImageIcon v-else :size="32" class="muted" /></button
                ><label class="image-checkbox"
                  ><input
                    type="checkbox"
                    :checked="selected.has(image.id)"
                    :aria-label="`选择 ${image.name}`"
                    @change="selectImage(image.id)" /></label
                ><span class="image-format">{{
                  image.format.toUpperCase()
                }}</span
                ><button
                  class="image-preview-link"
                  :aria-label="`打开 ${image.name} 详情`"
                  @click="detail = image"
                >
                  <ArrowUpRight :size="17" />
                </button>
              </div>
              <div class="image-card-caption">
                <button
                  class="image-title"
                  :title="image.name"
                  @click="detail = image"
                >
                  {{ image.name }}
                </button>
                <div class="image-card-meta">
                  <span>{{ image.width }} × {{ image.height }}</span
                  ><span>{{ bytes(image.size) }}</span>
                </div>
                <div class="image-card-bottom">
                  <span>{{ date(image.created_at) }}</span
                  ><button
                    class="icon-button"
                    :aria-label="`复制 ${image.name} 链接`"
                    @click="copyOne(image)"
                  >
                    <Check
                      v-if="copiedID === image.id"
                      :size="15"
                      class="success-text"
                    /><Copy v-else :size="15" />
                  </button>
                </div>
              </div>
            </article>
          </div>
          <div v-if="images.length && !loading" class="pagination">
            <span>共 {{ total }} 张图片 · 第 {{ page }} / {{ pages }} 页</span>
            <div>
              <button
                class="button compact"
                :disabled="page <= 1"
                @click="changePage(page - 1)"
              >
                <ArrowLeft :size="16" />上一页</button
              ><button
                class="button compact"
                :disabled="page >= pages"
                @click="changePage(page + 1)"
              >
                下一页<ArrowRight :size="16" />
              </button>
            </div>
          </div>
        </section>

        <UploadPanel
          v-show="view === 'upload'"
          ref="uploader"
          :albums="albums.filter((album) => album.can_upload)"
          :storages="uploadStores"
          :default-storage="uploadOptions?.default_storage || 'local'"
          :default-compression="uploadOptions?.processing.mode || 'none'"
          :default-quality="uploadOptions?.processing.quality ?? 82"
          :max-bytes="uploadOptions?.max_upload_bytes || 20 * 1024 * 1024"
          @notice="notice"
          @uploaded="uploaded"
        />

        <section v-if="view === 'albums'" aria-label="相册管理">
          <div v-if="!albums.length" class="gallery-empty">
            <div class="empty-illustration">
              <FolderPlus :size="40" :stroke-width="1.2" />
            </div>
            <span class="eyebrow">A PLACE FOR YOUR INSPIRATION</span>
            <h2>给图片一个小小的归处</h2>
            <p>
              {{
                isAdmin
                  ? "按主题、项目或心情创建相册，整理起来更轻松。"
                  : "暂无可访问相册，请联系管理员授权。"
              }}
            </p>
            <button v-if="isAdmin" class="button primary" @click="openAlbum()">
              <Plus :size="17" />创建第一个相册
            </button>
          </div>
          <div v-else class="album-grid">
            <article
              v-for="(album, index) in albums"
              :key="album.id"
              class="album-card"
            >
              <button class="album-card-open" @click="go('library', album.id)">
                <span class="album-cover" :class="`album-cover-${index % 4}`"
                  ><Folder :size="46" :stroke-width="1.2" /><span>{{
                    String(index + 1).padStart(2, "0")
                  }}</span></span
                ><strong>{{ album.name }}</strong
                ><span
                  >{{ album.count }} 张图片 · {{ date(album.created_at) }}</span
                >
              </button>
              <p class="album-permission-tag">
                {{ album.guest_visible ? "访客可见" : "私有相册" }} ·
                {{ album.can_upload ? "可上传" : "只读" }}
              </p>
              <div v-if="isAdmin" class="album-card-actions">
                <button class="text-button" @click="albumAccess = album">
                  权限</button
                ><button
                  class="text-button"
                  @click="share('album', album.id, album.name)"
                >
                  分享
                </button>
                <button class="text-button" @click="openAlbum(album)">
                  重命名</button
                ><button
                  class="icon-button"
                  :aria-label="`删除相册 ${album.name}`"
                  @click="removeAlbum(album)"
                >
                  <Trash2 :size="16" />
                </button>
              </div>
            </article>
            <button v-if="isAdmin" class="new-album-card" @click="openAlbum()">
              <Plus :size="28" :stroke-width="1.3" /><span>新建相册</span>
            </button>
          </div>
        </section>

        <section v-if="view === 'tokens'" class="settings-layout">
          <div class="panel token-panel">
            <div class="section-heading">
              <h3>你的上传凭据</h3>
              <span class="tag">仅上传权限</span>
            </div>
            <div v-if="!tokens.length" class="empty-state">
              <KeyRound :size="35" :stroke-width="1.3" />
              <h3>连接你的上传工具</h3>
              <p>创建一个 Token，就能从脚本或客户端上传图片。</p>
              <button class="button" @click="openToken">
                创建 Token<ArrowRight :size="15" />
              </button>
            </div>
            <div v-for="token in tokens" :key="token.id" class="token-row">
              <span class="token-icon"><KeyRound :size="19" /></span>
              <div>
                <strong>{{ token.name }}</strong
                ><code>{{ token.prefix }}…</code
                ><span>{{
                  !token.expires_at
                    ? "永久有效"
                    : new Date(token.expires_at).getTime() <= Date.now()
                      ? "已过期"
                      : `有效至 ${new Date(token.expires_at).toLocaleString()}`
                }}</span>
              </div>
              <button
                class="button compact danger-text"
                @click="revokeToken(token)"
              >
                撤销
              </button>
            </div>
          </div>
          <aside class="panel guidance">
            <ShieldCheck :size="23" />
            <h3>把权限留在刚好够用的地方</h3>
            <p>
              上传 Token 只能调用图片上传接口，不能浏览、修改或删除你的图片。
            </p>
            <p>
              完整 Token
              只在创建时显示一次，请妥善保存。不要放入公开仓库或网页代码。
            </p>
            <label class="field-label">上传接口</label
            ><code class="endpoint">POST {{ absolute("/api/v1/images") }}</code>
            <p>请求头使用 Bearer Token，表单字段为 <code>file</code>。</p>
            <button
              class="text-button"
              @click="copyText(absolute('/api/v1/images'))"
            >
              <Copy :size="14" />复制接口地址
            </button>
          </aside>
        </section>

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
          @changed="refresh"
          @notice="notice"
        />
        <ShareSettings
          v-if="view === 'shares' && isAdmin"
          :albums="albums"
          :target="shareTarget"
          @notice="notice"
        />

        <section v-if="view === 'settings'" class="settings-layout">
          <div class="settings-main">
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
              <div class="panel-actions">
                <button class="button primary" :disabled="uploadLimitBusy">
                  {{ uploadLimitBusy ? "正在保存…" : "保存上传限制" }}
                </button>
              </div>
            </form>
            <form class="panel settings-panel" @submit.prevent="changePassword">
              <h3>账号安全</h3>
              <p class="muted">
                修改密码后，全部登录会话和上传 Token 都会失效。
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
          </div>
          <aside v-if="isAdmin">
            <div class="panel system-panel">
              <span class="eyebrow">SMALL & SELF-HOSTED</span>
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
        <footer class="page-footer">
          <span>QPix · YOUR IMAGES, YOUR SPACE.</span
          ><span>简简单单，收好灵感。<span class="footer-dot" /></span>
        </footer>
      </main>
    </div>
  </div>

  <ImageDetail
    v-if="detail"
    :key="detail.id"
    :image="detail"
    :albums="albums"
    :can-share="isAdmin"
    @share="(image) => share('image', image.id, image.name)"
    @close="detail = null"
    @updated="refresh"
    @remove="(image) => removeImages([image])"
    @notice="notice"
  />
  <AlbumAccess
    v-if="albumAccess && isAdmin"
    :album="albumAccess"
    @close="albumAccess = null"
    @saved="refresh"
    @notice="notice"
  />
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
  <Modal
    v-if="modal === 'confirm'"
    :title="confirmTitle"
    :busy="modalBusy"
    @close="closeModal"
    ><div class="modal-body">
      <div class="confirm-icon"><Trash2 :size="25" /></div>
      <p class="confirm-body">{{ confirmBody }}</p>
      <p v-if="modalError" class="form-error" role="alert">{{ modalError }}</p>
      <div class="modal-actions">
        <button class="button" :disabled="modalBusy" @click="closeModal">
          取消</button
        ><button
          class="button danger"
          :disabled="modalBusy"
          @click="runConfirm"
        >
          {{ modalBusy ? "正在处理…" : "确认操作" }}
        </button>
      </div>
    </div></Modal
  >
  <Modal
    v-if="modal === 'token'"
    title="创建上传 Token"
    :busy="modalBusy"
    @close="closeModal"
    ><div v-if="newToken" class="modal-body">
      <div class="info-banner">
        <ShieldCheck :size="20" /><span
          >完整 Token 只会显示这一次，请立即保存。</span
        >
      </div>
      <label class="field-label" for="new-token">你的 Token</label
      ><textarea id="new-token" :value="newToken" rows="3" readonly /><button
        class="button primary full"
        @click="copyText(newToken)"
      >
        <Copy :size="16" />复制 Token
      </button>
      <div class="modal-actions">
        <button class="button" @click="closeModal">已妥善保存</button>
      </div>
    </div>
    <form v-else class="modal-body" @submit.prevent="createToken">
      <label class="field-label" for="token-name">名称</label
      ><input
        id="token-name"
        v-model="tokenName"
        required
        maxlength="64"
        placeholder="例如：笔记上传、博客脚本"
      /><label class="field-label" for="token-days">有效期</label
      ><select id="token-days" v-model="tokenDays">
        <option value="30">30 天</option>
        <option value="90">90 天</option>
        <option value="180">180 天</option>
        <option value="365">365 天</option>
        <option value="custom">自定义到期时间</option>
        <option value="permanent">永久有效</option>
      </select>
      <template v-if="tokenDays === 'custom'"
        ><label class="field-label" for="token-expires">到期时间</label
        ><input
          id="token-expires"
          v-model="tokenExpires"
          type="datetime-local"
          max="9999-12-31T23:59"
          required
      /></template>
      <p class="field-help">
        时间按当前设备时区填写。仅可上传图片，包括永久 Token
        在内都可以随时撤销。
      </p>
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
          {{ modalBusy ? "正在创建…" : "创建 Token" }}
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
