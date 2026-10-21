<script setup lang="ts">
import { onMounted, ref } from "vue";
import Modal from "./Modal.vue";
import { api } from "../api";
import type { Album, User } from "../types";
import { message } from "../utils";
const props = defineProps<{ album: Album }>();
const emit = defineEmits<{
  close: [];
  saved: [];
  notice: [text: string, error?: boolean];
}>();
const users = ref<User[]>([]);
const loading = ref(true);
const loaded = ref(false);
const busy = ref(false);
const error = ref("");
const guest = ref(props.album.guest_visible);
const members = ref([...props.album.member_ids]);
async function load() {
  loading.value = true;
  error.value = "";
  try {
    users.value = (await api<User[]>("/users")).filter(
      (user) => user.role === "user",
    );
    loaded.value = true;
  } catch (reason) {
    error.value = message(reason);
  } finally {
    loading.value = false;
  }
}
async function save() {
  busy.value = true;
  error.value = "";
  try {
    await api(`/albums/${props.album.id}/access`, "PUT", {
      guest_visible: guest.value,
      member_ids: members.value,
    });
    emit("saved");
    emit("notice", "相册权限已保存");
    emit("close");
  } catch (reason) {
    error.value = message(reason);
  } finally {
    busy.value = false;
  }
}
onMounted(load);
</script>
<template>
  <Modal
    :title="`相册权限 · ${album.name}`"
    :busy="busy"
    @close="emit('close')"
  >
    <form class="modal-body" @submit.prevent="save">
      <label class="check-field"
        ><input
          v-model="guest"
          type="checkbox"
        />允许访客通过图片链接访问</label
      >
      <p class="field-help">
        开启后，未登录访客可通过收到的图片链接查看该图片，不提供相册浏览入口。上传与编辑仍需账号授权。有效分享链接具有独立的只读授权。
      </p>
      <h3 class="permission-heading">授权用户</h3>
      <p class="field-help">
        被选中的用户可浏览、上传和管理本人上传的图片；管理员可管理全部图片。
      </p>
      <p v-if="loading" class="muted">正在读取用户…</p>
      <div v-else class="member-list">
        <label v-for="user in users" :key="user.id" class="check-field"
          ><input v-model="members" type="checkbox" :value="user.id" />{{
            user.username
          }}<span v-if="!user.enabled" class="muted">已停用</span></label
        >
        <p v-if="!users.length" class="field-help">
          请先在用户管理中创建普通账号。
        </p>
      </div>
      <p v-if="error" class="form-error" role="alert">
        {{ error }}
        <button
          v-if="!users.length"
          class="text-button"
          type="button"
          @click="load"
        >
          重试
        </button>
      </p>
      <div class="modal-actions">
        <button
          type="button"
          class="button"
          :disabled="busy"
          @click="emit('close')"
        >
          取消</button
        ><button class="button primary" :disabled="busy || loading || !loaded">
          {{ busy ? "正在保存…" : "保存权限" }}
        </button>
      </div>
    </form>
  </Modal>
</template>
