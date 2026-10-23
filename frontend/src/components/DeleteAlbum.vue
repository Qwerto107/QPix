<script setup lang="ts">
import { computed, ref } from "vue";
import Modal from "./Modal.vue";
import SelectField from "./SelectField.vue";
import type { Album } from "../types";
import { api } from "../api";
import { message } from "../utils";
const props = defineProps<{ album: Album; albums: Album[] }>();
const emit = defineEmits<{
  close: [];
  saved: [];
  notice: [text: string, error?: boolean];
}>();
const action = ref("move"),
  target = ref(""),
  busy = ref(false),
  error = ref("");
const choices = computed(() =>
  props.albums
    .filter(
      (v) => v.id !== props.album.id && v.owner_id === props.album.owner_id,
    )
    .map((v) => ({ value: v.id, label: v.name })),
);
const changedPolicy = computed(() => {
  const to = props.albums.find((v) => v.id === target.value);
  const from = props.album;
  return (
    to &&
    (from.guest_visible !== to.guest_visible ||
      from.referer_mode !== to.referer_mode ||
      from.allow_empty_referer !== to.allow_empty_referer ||
      JSON.stringify([...from.referers].sort()) !==
        JSON.stringify([...to.referers].sort()))
  );
});
async function save() {
  busy.value = true;
  error.value = "";
  try {
    await api(`/albums/${props.album.id}`, "DELETE", {
      action: action.value,
      target_id: target.value,
    });
    emit("saved");
    emit(
      "notice",
      action.value === "move"
        ? "相册已删除，图片已转移"
        : "相册已删除，图片已停止访问并开始清理",
    );
    emit("close");
  } catch (e) {
    error.value = message(e);
  } finally {
    busy.value = false;
  }
}
</script>
<template>
  <Modal :title="`删除相册 · ${album.name}`" :busy="busy" @close="emit('close')"
    ><form class="modal-body" @submit.prevent="save">
      <p>相册内有 {{ album.count }} 张图片，请选择处理方式。</p>
      <SelectField
        id="delete-album-action"
        label="图片处理方式"
        v-model="action"
        :options="[
          { value: 'move', label: '保留图片，转入其他相册' },
          { value: 'delete', label: '连同图片一起永久删除' },
        ]"
      /><template v-if="action === 'move'"
        ><label class="field-label">转入相册</label
        ><SelectField
          id="delete-album-target"
          label="转入相册"
          v-model="target"
          :options="choices"
        />
        <p v-if="changedPolicy" class="info-banner">
          目标相册的访问权限或 Referer
          策略不同，转移后图片及分享将按目标规则访问。
        </p></template
      >
      <p v-else class="form-error">
        图片、预览图和缩略图将永久删除，所有直链和分享链接都会失效，无法恢复。
      </p>
      <p v-if="error" class="form-error">{{ error }}</p>
      <div class="modal-actions">
        <button
          type="button"
          class="button"
          :disabled="busy"
          @click="emit('close')"
        >
          取消</button
        ><button
          class="button danger"
          :disabled="busy || (action === 'move' && !target)"
        >
          {{ busy ? "正在处理…" : "确认删除相册" }}
        </button>
      </div>
    </form></Modal
  >
</template>
