<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import {
  Folder,
  FolderPlus,
  LayoutGrid,
  List,
  Grid2X2,
  Copy,
  Upload,
  ChevronRight,
  Image as ImageIcon,
} from "@lucide/vue";
import SelectField from "./SelectField.vue";
import Modal from "./Modal.vue";
import ImageDetail from "./ImageDetail.vue";
import AlbumAccess from "./AlbumAccess.vue";
import AlbumActions from "./AlbumActions.vue";
import DeleteAlbum from "./DeleteAlbum.vue";
import { api } from "../api";
import type { Album, ImageItem, ImagePage, Stats, User } from "../types";
import {
  bytes,
  copy,
  imageLink,
  message,
  timestamp,
  preference,
  savePreference,
  type LinkFormat,
} from "../utils";
const props = defineProps<{
  all?: boolean;
  albumID: string;
  version: number;
  accounts: User[];
}>();
const emit = defineEmits<{
  notice: [text: string, error?: boolean];
  changed: [];
  folder: [id: string];
  upload: [id: string];
  share: [image: ImageItem];
}>();
const albums = ref<Album[]>([]),
  images = ref<ImageItem[]>([]),
  stats = ref<Stats | null>(null),
  total = ref(0),
  page = ref(1),
  loading = ref(false),
  error = ref("");
const q = ref(""),
  owner = ref(""),
  sort = ref(
    preference("gallery.sort", "created_at:desc", [
      "created_at:desc",
      "created_at:asc",
      "name:asc",
      "name:desc",
      "size:desc",
      "size:asc",
    ]),
  ),
  limit = ref(preference("gallery.limit", "20", ["20", "50", "100"]));
const layout = ref(
  preference(
    props.all ? "admin.layout" : "gallery.layout",
    props.all ? "list" : "grid",
    ["grid", "small", "list"],
  ),
);
const albumMode = ref(!props.all),
  detail = ref<ImageItem | null>(null),
  access = ref<Album | null>(null),
  deleting = ref<Album | null>(null),
  editing = ref<Album | null>(null),
  albumOpen = ref(false),
  albumName = ref(""),
  busy = ref(false),
  formError = ref(""),
  moving = ref(false),
  destination = ref(""),
  confirmDelete = ref(false);
const selected = ref(new Set<string>());
const cursors = ref([""]), nextCursor = ref("");
const deleteTargets = ref<ImageItem[]>([]);
const root = computed(() => !props.albumID && albumMode.value),
  activeAlbum = computed(() =>
    albums.value.find((v) => v.id === props.albumID),
  );
// 相册没有文件大小字段，从图片视图返回时恢复适用的排序。
watch(
  root,
  (value) => {
    if (value && sort.value.startsWith("size:")) sort.value = "created_at:desc";
  },
  { immediate: true },
);
const filteredAlbums = computed(() =>
  albums.value
    .filter(
      (v) =>
        (!owner.value || v.owner_id === owner.value) &&
        v.name.toLocaleLowerCase().includes(q.value.toLocaleLowerCase()),
    )
    .sort((a, b) => {
      const [field, dir] = sort.value.split(":");
      const result =
        field === "name"
          ? a.name.localeCompare(b.name, "zh-CN")
          : new Date(a.created_at).getTime() - new Date(b.created_at).getTime();
      return dir === "asc" ? result : -result;
    }),
);
const count = computed(() =>
    root.value ? filteredAlbums.value.length : total.value,
  ),
  pages = computed(() =>
    Math.max(1, Math.ceil(count.value / Number(limit.value))),
  ),
  albumRows = computed(() =>
    filteredAlbums.value.slice(
      (page.value - 1) * Number(limit.value),
      page.value * Number(limit.value),
    ),
  );
const selectedRows = computed(() =>
    images.value.filter((v) => selected.value.has(v.id)),
  ),
  allSelected = computed(
    () =>
      images.value.length > 0 &&
      images.value.every((v) => selected.value.has(v.id)),
  );
const moveAlbums = computed(() =>
  albums.value.filter((album) =>
    selectedRows.value.every((image) => image.owner_id === album.owner_id),
  ),
);
const policyChanges = computed(() => {
  const to = albums.value.find((v) => v.id === destination.value);
  if (!to) return false;
  return selectedRows.value.some((v) => {
    const from = albums.value.find((a) => a.id === v.album_id);
    return (
      !from ||
      from.guest_visible !== to.guest_visible ||
      from.referer_mode !== to.referer_mode ||
      from.allow_empty_referer !== to.allow_empty_referer ||
      JSON.stringify([...from.referers].sort()) !==
        JSON.stringify([...to.referers].sort())
    );
  });
});
let requestID = 0,
  timer: ReturnType<typeof setTimeout> | undefined;
async function load() {
  page.value = 1;
  cursors.value = [""];
  nextCursor.value = "";
  await fetchPage(true);
}
async function turnPage(delta: number) {
  const previous = page.value;
  if (!root.value && delta > 0) {
    if (!nextCursor.value) return;
    cursors.value[page.value] = nextCursor.value;
  }
  page.value += delta;
  await fetchPage();
  if (error.value) page.value = previous;
}
async function fetchPage(refreshMeta = false) {
  const request = ++requestID;
  loading.value = true;
  error.value = "";
  try {
    const params = new URLSearchParams({ scope: props.all ? "all" : "own" });
    if (refreshMeta) {
      const [albumData, statData] = await Promise.all([
        api<Album[]>(`/albums?${params}`),
        api<Stats>(`/stats?${params}`),
      ]);
      if (request !== requestID) return;
      albums.value = albumData;
      stats.value = statData;
    }
    if (props.albumID && !albums.value.some((v) => v.id === props.albumID)) {
      emit("folder", "");
      return;
    }
    if (!root.value) {
      const [field, order] = sort.value.split(":");
      params.set("album_id", props.albumID);
      params.set("q", q.value);
      params.set("user_id", props.all ? owner.value : "");
      params.set("pagination", "cursor");
      params.set("cursor", cursors.value[page.value - 1] || "");
      params.set("limit", limit.value);
      params.set("sort", field!);
      params.set("order", order!);
      const data = await api<ImagePage>(`/images?${params}`);
      if (request !== requestID) return;
      images.value = data.items;
      if (data.total !== null) total.value = data.total;
      nextCursor.value = data.next_cursor;
      // 图片可能在翻页期间被删除；空页退回前一页，重新刷新可更新总数。
      if (!data.items.length && page.value > 1) {
        page.value--;
        await fetchPage();
        return;
      }
      selected.value = new Set(
        [...selected.value].filter((id) => data.items.some((v) => v.id === id)),
      );
    }
    if (root.value && page.value > pages.value) {
      page.value = pages.value;
      void fetchPage();
    }
  } catch (e) {
    if (request === requestID) error.value = message(e);
  } finally {
    if (request === requestID) loading.value = false;
  }
}
watch(
  () => [props.albumID, props.version, props.all],
  () => {
    page.value = 1;
    selected.value = new Set();
    void load();
  },
  { immediate: true },
);
watch([q, owner, sort, limit, albumMode], () => {
  page.value = 1;
  selected.value = new Set();
  if (timer) clearTimeout(timer);
  savePreference("gallery.sort", sort.value);
  savePreference("gallery.limit", limit.value);
  timer = setTimeout(load, 250);
});
watch(layout, (value) =>
  savePreference(props.all ? "admin.layout" : "gallery.layout", value),
);
function folder(id: string) {
  q.value = "";
  selected.value = new Set();
  emit("folder", id);
}
function albumAction(album: Album, action: string) {
  if (action === "access") access.value = album;
  else if (action === "rename") editAlbum(album);
  else if (action === "default") setDefault(album);
  else if (action === "delete") deleting.value = album;
}
function select(id: string) {
  const value = new Set(selected.value);
  value.has(id) ? value.delete(id) : value.add(id);
  selected.value = value;
}
function selectAll() {
  selected.value = allSelected.value
    ? new Set()
    : new Set(images.value.map((v) => v.id));
}
async function copyLinks(format: LinkFormat, rows = selectedRows.value) {
  try {
    await copy(rows.map((v) => imageLink(v, format)).join("\n"));
    emit("notice", `已复制 ${rows.length} 张图片的链接`);
  } catch {
    emit("notice", "复制失败，请检查剪贴板权限", true);
  }
}
function editAlbum(album?: Album) {
  editing.value = album || null;
  albumName.value = album?.name || "";
  formError.value = "";
  albumOpen.value = true;
}
async function saveAlbum() {
  busy.value = true;
  formError.value = "";
  try {
    await api(
      editing.value ? `/albums/${editing.value.id}` : "/albums",
      editing.value ? "PATCH" : "POST",
      { name: albumName.value },
    );
    albumOpen.value = false;
    await changed();
    emit("notice", "相册已保存");
  } catch (e) {
    formError.value = message(e);
  } finally {
    busy.value = false;
  }
}
async function changed() {
  detail.value = null;
  await load();
  emit("changed");
}
async function setDefault(album: Album) {
  busy.value = true;
  try {
    await api(`/albums/${album.id}/default`, "PUT", {});
    await changed();
    emit("notice", "默认相册已更新");
  } catch (e) {
    emit("notice", message(e), true);
  } finally {
    busy.value = false;
  }
}
async function move() {
  busy.value = true;
  formError.value = "";
  try {
    await api("/images/batch-move", "POST", {
      ids: selectedRows.value.map((v) => v.id),
      album_id: destination.value,
    });
    moving.value = false;
    selected.value = new Set();
    await changed();
    emit("notice", "图片已转移，上传人和容量归属不变");
  } catch (e) {
    formError.value = message(e);
  } finally {
    busy.value = false;
  }
}
function requestDelete(rows = selectedRows.value) {
  if (!rows.length) return;
  // 单张删除保存独立目标，避免覆盖用户已有的多选项或误删其他图片。
  deleteTargets.value = [...rows];
  formError.value = "";
  confirmDelete.value = true;
}
async function remove() {
  if (!deleteTargets.value.length) return;
  const ids = new Set(deleteTargets.value.map((image) => image.id));
  busy.value = true;
  formError.value = "";
  try {
    const result = await api<{ deleted: number; failed: string[] }>(
      "/images/batch-delete",
      "POST",
      { ids: [...ids] },
    );
    confirmDelete.value = false;
    selected.value = new Set([...selected.value].filter((id) => !ids.has(id)));
    deleteTargets.value = [];
    await changed();
    emit(
      "notice",
      result.failed.length
        ? "部分图片待后台重试清理"
        : `已删除 ${result.deleted} 张图片`,
      result.failed.length > 0,
    );
  } catch (e) {
    formError.value = message(e);
  } finally {
    busy.value = false;
  }
}
onBeforeUnmount(() => {
  requestID++;
  if (timer) clearTimeout(timer);
});
</script>
<template>
  <section class="library-browser" aria-label="图库浏览器">
    <div class="library-summary" v-if="all && stats">
      全站 {{ stats.images }} 张图片 · {{ stats.albums }} 个相册 ·
      {{ bytes(stats.bytes)
      }}<span v-if="stats.pending_deletions">
        · {{ stats.pending_deletions }} 张待清理</span
      >
    </div>
    <div class="folder-path">
      <button class="text-button" @click="folder('')">
        {{ all ? "图片管理" : "我的图库" }}</button
      ><template v-if="activeAlbum"
        ><ChevronRight :size="14" /><strong>{{
          activeAlbum.name
        }}</strong></template
      ><span v-else class="muted">{{ root ? "相册" : "全部图片" }}</span>
      <div v-if="all && !albumID" class="view-toggle">
        <button :class="{ active: !albumMode }" @click="albumMode = false">
          图片</button
        ><button :class="{ active: albumMode }" @click="albumMode = true">
          相册
        </button>
      </div>
    </div>
    <div class="compact-toolbar">
      <input
        v-model="q"
        class="list-search"
        :placeholder="root ? '搜索相册名称' : '搜索图片名称'"
        :aria-label="root ? '搜索相册' : '搜索图片'"
      /><SelectField
        v-if="all"
        id="library-owner"
        label="筛选上传人或相册所有者"
        v-model="owner"
        :options="[
          { value: '', label: '所有用户' },
          ...accounts.map((v) => ({ value: v.id, label: v.username })),
        ]"
      /><SelectField
        id="library-sort"
        label="排序"
        v-model="sort"
        :options="[
          { value: 'created_at:desc', label: '时间 ↓' },
          { value: 'created_at:asc', label: '时间 ↑' },
          { value: 'name:asc', label: '名称 ↑' },
          { value: 'name:desc', label: '名称 ↓' },
          ...(!root
            ? [
                { value: 'size:desc', label: '大小 ↓' },
                { value: 'size:asc', label: '大小 ↑' },
              ]
            : []),
        ]"
      />
      <div class="view-toggle">
        <button
          :class="{ active: layout === 'grid' }"
          aria-label="大图标"
          :aria-pressed="layout === 'grid'"
          @click="layout = 'grid'"
        >
          <LayoutGrid :size="17" /></button
        ><button
          :class="{ active: layout === 'small' }"
          aria-label="小图标"
          :aria-pressed="layout === 'small'"
          @click="layout = 'small'"
        >
          <Grid2X2 :size="17" /></button
        ><button
          :class="{ active: layout === 'list' }"
          aria-label="详细信息"
          :aria-pressed="layout === 'list'"
          @click="layout = 'list'"
        >
          <List :size="17" />
        </button>
      </div>
      <button v-if="!all" class="button compact" @click="editAlbum()">
        <FolderPlus :size="15" />新建相册</button
      ><button
        v-if="!all"
        class="button primary compact"
        @click="emit('upload', albumID)"
      >
        <Upload :size="15" />上传图片
      </button>
    </div>
    <p v-if="error" class="form-error" role="alert">
      {{ error }} <button class="text-button" @click="load">重试</button>
    </p>
    <p v-if="loading" class="muted" role="status">正在加载…</p>
    <template v-else-if="root">
      <div
        v-if="layout === 'list'"
        class="management-list"
        role="region"
        aria-label="图库表格，可横向滚动"
        tabindex="0"
      >
        <table
          class="management-table album-table"
          :class="{ 'album-table-all': all }"
        >
          <colgroup>
            <col />
            <col class="album-count-column" />
            <col v-if="all" class="album-owner-column" />
            <col v-if="all" class="album-role-column" />
            <col class="album-access-column" />
            <col class="album-referer-column" />
            <col class="album-created-column" />
            <col class="album-actions-column" />
          </colgroup>
          <thead>
            <tr>
              <th>相册名称</th>
              <th>图片数</th>
              <th v-if="all">所有者</th>
              <th v-if="all">身份</th>
              <th>图片直链</th>
              <th>Referer</th>
              <th>创建时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="album in albumRows" :key="album.id">
              <td>
                <div class="album-name-cell">
                  <button
                    class="text-button album-row-name"
                    :title="album.name"
                    @click="folder(album.id)"
                  >
                    <Folder :size="17" /><span>{{ album.name }}</span></button
                  ><span v-if="album.is_default" class="tag">默认</span>
                </div>
              </td>
              <td>{{ album.count }}</td>
              <td
                v-if="all"
                class="management-truncate"
                :title="album.owner_name"
              >
                {{ album.owner_name }}
              </td>
              <td v-if="all">
                {{ album.owner_role === "admin" ? "管理员" : "用户" }}
              </td>
              <td>{{ album.guest_visible ? "允许访客" : "仅用户访问" }}</td>
              <td>
                {{
                  album.referer_mode === "custom"
                    ? "自定义"
                    : album.referer_mode === "unrestricted"
                      ? "不限制"
                      : "继承全局"
                }}
              </td>
              <td>{{ timestamp(album.created_at) }}</td>
              <td>
                <AlbumActions
                  :album="album"
                  :busy="busy"
                  @select="albumAction(album, $event)"
                />
              </td>
            </tr>
            <tr v-if="!albumRows.length">
              <td :colspan="all ? 8 : 6" class="table-empty">
                暂无符合条件的相册
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div
        v-else
        class="folder-grid"
        :class="{ 'small-icons': layout === 'small' }"
      >
        <article v-for="album in albumRows" :key="album.id" class="folder-item">
          <button class="folder-open" @click="folder(album.id)">
            <Folder
              :size="layout === 'small' ? 30 : 46"
              :stroke-width="1.5"
            /><strong :title="album.name">{{ album.name }}</strong
            ><span
              >{{ album.count }} 张图片<span v-if="album.is_default">
                · 默认</span
              ></span
            ><span v-if="all"
              >{{ album.owner_name }} ·
              {{ album.owner_role === "admin" ? "管理员" : "用户" }}</span
            >
          </button>
          <AlbumActions
            class="folder-more"
            :album="album"
            :busy="busy"
            @select="albumAction(album, $event)"
          />
        </article>
        <p v-if="!albumRows.length" class="muted">暂无符合条件的相册</p>
      </div>
    </template>
    <template v-else>
      <div class="selection-toolbar compact-selection">
        <label class="selection-label"
          ><input
            type="checkbox"
            :checked="allSelected"
            :disabled="!images.length"
            @change="selectAll"
          />{{ selected.size ? `已选 ${selected.size} 张` : "全选本页" }}</label
        ><template v-if="selected.size"
          ><button class="text-button" @click="copyLinks('url')">
            复制直链</button
          ><button class="text-button" @click="copyLinks('markdown')">
            Markdown</button
          ><button class="text-button" @click="copyLinks('html')">HTML</button
          ><button
            class="text-button"
            @click="
              moving = true;
              destination = '';
              formError = '';
            "
          >
            移动到相册</button
          ><button class="text-button danger-text" @click="requestDelete()">
            删除
          </button></template
        >
      </div>
      <div
        v-if="layout === 'list'"
        class="management-list"
        role="region"
        aria-label="图库表格，可横向滚动"
        tabindex="0"
      >
        <table class="management-table image-details-table">
          <thead>
            <tr>
              <th class="check-column">选择</th>
              <th>图片名称</th>
              <th>格式</th>
              <th>尺寸</th>
              <th>大小</th>
              <th v-if="all">上传人</th>
              <th v-if="all">身份</th>
              <th>上传时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="image in images" :key="image.id">
              <td>
                <input
                  type="checkbox"
                  :checked="selected.has(image.id)"
                  :aria-label="`选择 ${image.name}`"
                  @change="select(image.id)"
                />
              </td>
              <td>
                <button
                  class="image-row-name"
                  :title="image.name"
                  @click="detail = image"
                >
                  <img
                    v-if="image.thumbnail_url"
                    :src="image.thumbnail_url"
                    alt=""
                    loading="lazy"
                  /><ImageIcon v-else :size="20" /><span>{{ image.name }}</span>
                </button>
              </td>
              <td>{{ image.format.toUpperCase() }}</td>
              <td>{{ image.width }} × {{ image.height }}</td>
              <td>{{ bytes(image.size) }}</td>
              <td v-if="all">{{ image.owner_name }}</td>
              <td v-if="all">
                {{ image.owner_role === "admin" ? "管理员" : "用户" }}
              </td>
              <td class="management-number">
                {{ timestamp(image.created_at) }}
              </td>
              <td>
                <div class="management-actions">
                  <button
                    class="text-button"
                    @click="copyLinks('url', [image])"
                  >
                    复制直链</button
                  ><button class="text-button" @click="emit('share', image)">
                    分享
                  </button>
                  <button
                    v-if="image.can_manage"
                    class="text-button danger-text"
                    :aria-label="`删除 ${image.name}`"
                    :disabled="busy"
                    @click="requestDelete([image])"
                  >
                    删除
                  </button>
                </div>
              </td>
            </tr>
            <tr v-if="!images.length">
              <td :colspan="all ? 9 : 7" class="table-empty">
                相册中暂无符合条件的图片
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div
        v-else
        class="image-grid"
        :class="{ 'small-image-grid': layout === 'small' }"
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
                v-if="image.thumbnail_url"
                :src="image.thumbnail_url"
                :alt="image.name"
                loading="lazy"
              /><span v-else class="image-placeholder">
                <ImageIcon :size="32" />暂不支持预览
              </span></button
            ><label class="image-checkbox"
              ><input
                type="checkbox"
                :checked="selected.has(image.id)"
                :aria-label="`选择 ${image.name}`"
                @change="select(image.id)"
            /></label>
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
              <span>{{ bytes(image.size) }}</span
              ><span>{{ image.width }} × {{ image.height }}</span>
            </div>
            <div v-if="all" class="management-secondary">
              {{ image.owner_name }} ·
              {{ image.owner_role === "admin" ? "管理员" : "用户" }} ·
              {{ timestamp(image.created_at) }}
            </div>
            <div class="management-actions">
              <button class="text-button" @click="copyLinks('url', [image])">
                <Copy :size="14" />直链</button
              ><button class="text-button" @click="emit('share', image)">
                分享
              </button>
            </div>
          </div>
        </article>
        <p v-if="!images.length" class="muted">相册中暂无符合条件的图片。</p>
      </div>
    </template>
    <div class="compact-pagination">
      <span>共 {{ count }} {{ root ? "个相册" : "张图片" }}</span
      ><SelectField
        id="library-limit"
        label="每页数量"
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
      ><span>{{ root ? `${page} / ${pages}` : `第 ${page} 页` }}</span
      ><button
        class="button compact"
        :disabled="(root ? page >= pages : !nextCursor) || loading"
        @click="turnPage(1)"
      >
        下一页
      </button>
    </div>
    <ImageDetail
      v-if="detail"
      :key="detail.id"
      :image="detail"
      :albums="albums"
      can-share
      @close="detail = null"
      @updated="changed"
      @remove="
        (image) => {
          detail = null;
          requestDelete([image]);
        }
      "
      @share="
        (image) => {
          detail = null;
          emit('share', image);
        }
      "
      @notice="(text, bad) => emit('notice', text, bad)"
    />
    <AlbumAccess
      v-if="access"
      :album="access"
      @close="access = null"
      @saved="changed"
      @notice="(text, bad) => emit('notice', text, bad)"
    />
    <DeleteAlbum
      v-if="deleting"
      :album="deleting"
      :albums="albums"
      @close="deleting = null"
      @saved="changed"
      @notice="(text, bad) => emit('notice', text, bad)"
    />
    <Modal
      v-if="albumOpen"
      :title="editing ? '重命名相册' : '新建相册'"
      :busy="busy"
      @close="albumOpen = false"
      ><form class="modal-body" @submit.prevent="saveAlbum">
        <label class="field-label" for="browser-album-name">相册名称</label
        ><input
          id="browser-album-name"
          v-model="albumName"
          required
          maxlength="100"
        />
        <p v-if="formError" class="form-error">{{ formError }}</p>
        <div class="modal-actions">
          <button
            type="button"
            class="button"
            :disabled="busy"
            @click="albumOpen = false"
          >
            取消</button
          ><button class="button primary" :disabled="busy">保存</button>
        </div>
      </form></Modal
    >
    <Modal
      v-if="moving"
      title="移动图片到相册"
      :busy="busy"
      @close="moving = false"
      ><form class="modal-body" @submit.prevent="move">
        <SelectField
          id="move-image-album"
          label="目标相册"
          v-model="destination"
          :options="
            moveAlbums.map((v) => ({
              value: v.id,
              label: all ? `${v.owner_name} / ${v.name}` : v.name,
            }))
          "
        />
        <p v-if="policyChanges" class="info-banner">
          目标相册的访问权限或 Referer
          策略不同，转移后图片及分享将按目标规则访问。
        </p>
        <p class="field-help">移动不会改变上传人和容量归属。</p>
        <p v-if="formError" class="form-error">{{ formError }}</p>
        <div class="modal-actions">
          <button
            type="button"
            class="button"
            :disabled="busy"
            @click="moving = false"
          >
            取消</button
          ><button class="button primary" :disabled="busy || !destination">
            确认移动
          </button>
        </div>
      </form></Modal
    >
    <Modal
      v-if="confirmDelete"
      title="永久删除图片"
      :busy="busy"
      @close="confirmDelete = false"
      ><div class="modal-body">
        <p>
          永久删除<template v-if="deleteTargets.length === 1"
            >图片“{{ deleteTargets[0]?.name }}”</template
          ><template v-else>选中的 {{ deleteTargets.length }} 张图片</template
          >及其预览图和缩略图？所有直链及分享链接将失效，无法恢复。
        </p>
        <p v-if="formError" class="form-error">{{ formError }}</p>
        <div class="modal-actions">
          <button
            class="button"
            :disabled="busy"
            @click="confirmDelete = false"
          >
            取消</button
          ><button class="button danger" :disabled="busy" @click="remove">
            确认删除
          </button>
        </div>
      </div></Modal
    >
  </section>
</template>
