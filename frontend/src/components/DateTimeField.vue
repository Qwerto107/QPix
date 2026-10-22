<script setup lang="ts">
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from "vue";
import { CalendarDays, ChevronLeft, ChevronRight } from "@lucide/vue";
import SelectField from "./SelectField.vue";
import { formatLocalDateTime, parseLocalDateTime } from "../dateTime";

const props = defineProps<{
  id: string;
  label: string;
  modelValue: string;
  originalValue?: string;
  disabled?: boolean;
  required?: boolean;
}>();
const emit = defineEmits<{ "update:modelValue": [value: string] }>();
const root = ref<HTMLElement>(),
  panel = ref<HTMLElement>(),
  input = ref<HTMLInputElement>();
const open = ref(false),
  touched = ref(false),
  text = ref(props.modelValue.replace("T", " "));
const viewYear = ref(new Date().getFullYear()),
  viewMonth = ref("1"),
  yearText = ref("");
const day = ref(""),
  hour = ref("00"),
  minute = ref("00"),
  focusDay = ref("");
const position = ref({ top: "0px", left: "0px" });
const months = Array.from({ length: 12 }, (_, i) => ({
  value: String(i + 1),
  label: `${i + 1} 月`,
}));
const weekdays = ["一", "二", "三", "四", "五", "六", "日"];
const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;
const pad = (value: number) => String(value).padStart(2, "0");
const dateKey = (date: Date) => formatLocalDateTime(date).slice(0, 10);
function calendarDate(year: number, month: number, date: number) {
  const result = new Date(0);
  result.setFullYear(year, month, date);
  result.setHours(12, 0, 0, 0);
  return result;
}
const cells = computed(() => {
  const first = calendarDate(viewYear.value, Number(viewMonth.value) - 1, 1);
  const offset = (first.getDay() + 6) % 7;
  const count = calendarDate(
    viewYear.value,
    Number(viewMonth.value),
    0,
  ).getDate();
  return Array.from({ length: Math.ceil((offset + count) / 7) * 7 }, (_, i) => {
    const number = i - offset + 1;
    if (number < 1 || number > count) return null;
    const date = calendarDate(
      viewYear.value,
      Number(viewMonth.value) - 1,
      number,
    );
    const end = new Date(date);
    end.setHours(23, 59, 59, 999);
    return { number, key: dateKey(date), past: end.getTime() < Date.now() };
  });
});
const calendarFocus = computed(() =>
  cells.value.some((cell) => cell?.key === focusDay.value && !cell.past)
    ? focusDay.value
    : cells.value.find((cell) => cell && !cell.past)?.key,
);
const validation = computed(() => {
  if (!props.modelValue) return props.required ? "请选择到期时间" : "";
  let date = parseLocalDateTime(props.modelValue);
  if (!date) return "请输入有效时间，格式为 YYYY-MM-DD HH:mm";
  // 编辑时未改变分钟字段，应按原有秒数校验，避免误判即将到期但仍有效的时间。
  if (props.originalValue) {
    const original = new Date(props.originalValue);
    if (
      !Number.isNaN(original.getTime()) &&
      formatLocalDateTime(original) === formatLocalDateTime(date)
    )
      date = original;
  }
  return date.getTime() <= Date.now() ? "到期时间必须晚于当前时间" : "";
});
const draft = computed(() =>
  parseLocalDateTime(`${day.value}T${hour.value}:${minute.value}`),
);
const draftError = computed(() =>
  !draft.value
    ? "时为 00–23，分为 00–59"
    : draft.value.getTime() <= Date.now()
      ? "请选择晚于当前时间的时刻"
      : "",
);
watch(
  () => props.modelValue,
  (value) => {
    text.value = value.replace("T", " ");
  },
);
watch(validation, (message) => input.value?.setCustomValidity(message), {
  flush: "post",
});
watch([viewYear, viewMonth], () => {
  if (open.value) nextTick(place);
});
watch(draftError, () => {
  if (open.value) nextTick(place);
});
watch(
  () => props.disabled,
  (value) => {
    if (value) close();
  },
);
function edit(value: string) {
  text.value = value;
  emit("update:modelValue", value.replace(" ", "T"));
}
function normalize() {
  touched.value = true;
  const date = parseLocalDateTime(props.modelValue);
  if (date) emit("update:modelValue", formatLocalDateTime(date));
}
function setDraft(date: Date) {
  day.value = dateKey(date);
  focusDay.value = day.value;
  viewYear.value = date.getFullYear();
  yearText.value = String(viewYear.value);
  viewMonth.value = String(date.getMonth() + 1);
  hour.value = pad(date.getHours());
  minute.value = pad(date.getMinutes());
}
function place() {
  if (!panel.value || !root.value || !open.value) return;
  const anchor = root.value.getBoundingClientRect(),
    box = panel.value.getBoundingClientRect();
  const width = document.documentElement.clientWidth,
    height = window.innerHeight;
  const below = anchor.bottom + 6;
  position.value = {
    left: `${Math.max(8, Math.min(anchor.right - box.width, width - box.width - 8))}px`,
    top: `${Math.max(8, below + box.height <= height - 8 ? below : anchor.top - box.height - 6)}px`,
  };
}
async function reveal() {
  if (props.disabled) return;
  if (open.value) {
    close();
    return;
  }
  const fallback = new Date();
  fallback.setDate(fallback.getDate() + 1);
  setDraft(parseLocalDateTime(props.modelValue) || fallback);
  open.value = true;
  // 原生 Popover 仅提供顶层承载与外部点击关闭，日历和输入交互均由组件绘制。
  panel.value?.showPopover();
  await nextTick();
  place();
  panel.value
    ?.querySelector<HTMLButtonElement>(`[data-date="${focusDay.value}"]`)
    ?.focus({ preventScroll: true });
}
function close(restore = false) {
  panel.value?.hidePopover();
  open.value = false;
  if (restore) input.value?.focus({ preventScroll: true });
}
function toggle(event: Event) {
  open.value = (event as ToggleEvent).newState === "open";
}
function moveMonth(offset: number) {
  const date = calendarDate(
    viewYear.value,
    Number(viewMonth.value) - 1 + offset,
    1,
  );
  if (date.getFullYear() < 1 || date.getFullYear() > 9999) return;
  viewYear.value = date.getFullYear();
  yearText.value = String(viewYear.value);
  viewMonth.value = String(date.getMonth() + 1);
}
function changeYear() {
  const value = Number(yearText.value);
  if (/^\d{1,4}$/.test(yearText.value) && value >= 1 && value <= 9999)
    viewYear.value = value;
  yearText.value = String(viewYear.value);
}
async function navigate(event: KeyboardEvent, key: string) {
  const date = parseLocalDateTime(`${key}T12:00`);
  if (
    !date ||
    ![
      "ArrowLeft",
      "ArrowRight",
      "ArrowUp",
      "ArrowDown",
      "Home",
      "End",
      "PageUp",
      "PageDown",
    ].includes(event.key)
  )
    return;
  event.preventDefault();
  if (event.key === "PageUp" || event.key === "PageDown") {
    const offset = event.key === "PageUp" ? -1 : 1;
    const target = calendarDate(
      date.getFullYear(),
      date.getMonth() + offset * (event.shiftKey ? 12 : 1),
      1,
    );
    const last = calendarDate(
      target.getFullYear(),
      target.getMonth() + 1,
      0,
    ).getDate();
    target.setDate(Math.min(date.getDate(), last));
    date.setTime(target.getTime());
  } else {
    const week = (date.getDay() + 6) % 7;
    const offset =
      {
        ArrowLeft: -1,
        ArrowRight: 1,
        ArrowUp: -7,
        ArrowDown: 7,
        Home: -week,
        End: 6 - week,
      }[event.key] || 0;
    date.setDate(date.getDate() + offset);
  }
  if (
    date.getFullYear() < 1 ||
    date.getFullYear() > 9999 ||
    dateKey(date) < dateKey(new Date())
  )
    return;
  viewYear.value = date.getFullYear();
  yearText.value = String(viewYear.value);
  viewMonth.value = String(date.getMonth() + 1);
  focusDay.value = dateKey(date);
  await nextTick();
  panel.value
    ?.querySelector<HTMLButtonElement>(`[data-date="${focusDay.value}"]`)
    ?.focus({ preventScroll: true });
}
function quick(days: number) {
  const date = new Date();
  date.setDate(date.getDate() + days);
  setDraft(date);
}
function apply() {
  if (!draft.value || draftError.value) return;
  emit("update:modelValue", formatLocalDateTime(draft.value));
  touched.value = true;
  close(true);
}
function escape(event: KeyboardEvent) {
  if (!open.value) return;
  event.stopPropagation();
  event.preventDefault();
  close(true);
}
onMounted(() => {
  input.value?.setCustomValidity(validation.value);
  window.addEventListener("resize", place);
  document.addEventListener("scroll", place, true);
});
onBeforeUnmount(() => {
  window.removeEventListener("resize", place);
  document.removeEventListener("scroll", place, true);
});
</script>

<template>
  <div ref="root" class="date-time-field" @keydown.esc="escape">
    <div
      class="date-time-input"
      :class="{ 'is-invalid': touched && validation, 'is-open': open }"
    >
      <input
        ref="input"
        :id="id"
        :value="text"
        :aria-label="label"
        :disabled="disabled"
        :required="required"
        :aria-invalid="touched && !!validation"
        :aria-describedby="`${id}-hint`"
        placeholder="YYYY-MM-DD HH:mm"
        autocomplete="off"
        maxlength="16"
        @input="edit(($event.target as HTMLInputElement).value)"
        @blur="normalize"
        @invalid="touched = true"
        @keydown.down.prevent="reveal"
        @keydown.enter.prevent="normalize"
      />
      <button
        type="button"
        :aria-label="`选择${label}`"
        :aria-expanded="open"
        :aria-controls="`${id}-calendar`"
        aria-haspopup="dialog"
        :disabled="disabled"
        @click="reveal"
      >
        <CalendarDays :size="16" />
      </button>
    </div>
    <p
      :id="`${id}-hint`"
      class="date-time-hint"
      :class="{ 'date-time-error': touched && validation }"
      :role="touched && validation ? 'alert' : undefined"
    >
      {{ touched && validation ? validation : `本地时间 · ${timeZone}` }}
    </p>
    <div
      ref="panel"
      :id="`${id}-calendar`"
      popover="auto"
      role="dialog"
      :aria-label="`选择${label}`"
      class="date-time-popover"
      :style="position"
      @toggle="toggle"
    >
      <div class="date-calendar-heading">
        <button
          type="button"
          class="date-calendar-arrow"
          aria-label="上个月"
          :disabled="viewYear === 1 && viewMonth === '1'"
          @click="moveMonth(-1)"
        >
          <ChevronLeft :size="17" />
        </button>
        <label class="date-calendar-year"
          ><input
            v-model="yearText"
            inputmode="numeric"
            maxlength="4"
            aria-label="年份"
            @blur="changeYear"
            @keydown.enter.prevent="changeYear"
          /><span>年</span></label
        >
        <SelectField
          :id="`${id}-month`"
          label="月份"
          v-model="viewMonth"
          :options="months"
        />
        <button
          type="button"
          class="date-calendar-arrow"
          aria-label="下个月"
          :disabled="viewYear === 9999 && viewMonth === '12'"
          @click="moveMonth(1)"
        >
          <ChevronRight :size="17" />
        </button>
      </div>
      <div
        class="date-calendar-grid"
        role="group"
        :aria-label="`${viewYear} 年 ${viewMonth} 月`"
      >
        <span
          v-for="weekday in weekdays"
          :key="weekday"
          class="date-calendar-weekday"
          aria-hidden="true"
          >{{ weekday }}</span
        >
        <template v-for="(cell, index) in cells" :key="index"
          ><button
            v-if="cell"
            type="button"
            :data-date="cell.key"
            :aria-label="cell.key"
            :aria-pressed="cell.key === day"
            :aria-current="
              cell.key === dateKey(new Date()) ? 'date' : undefined
            "
            :tabindex="cell.key === calendarFocus ? 0 : -1"
            :disabled="cell.past"
            :class="{ 'is-selected': cell.key === day }"
            @click="
              day = cell.key;
              focusDay = cell.key;
            "
            @keydown="navigate($event, cell.key)"
          >
            {{ cell.number }}</button
          ><span v-else
        /></template>
      </div>
      <div class="date-calendar-quick">
        <button
          v-for="shortcut in [
            { days: 1, label: '明天' },
            { days: 7, label: '7 天后' },
            { days: 30, label: '30 天后' },
          ]"
          :key="shortcut.days"
          type="button"
          @click="quick(shortcut.days)"
        >
          {{ shortcut.label }}
        </button>
      </div>
      <div class="date-calendar-time">
        <span>时间 <small>24 小时制</small></span>
        <div>
          <input
            v-model="hour"
            aria-label="时"
            inputmode="numeric"
            maxlength="2"
            @blur="/^\d{1,2}$/.test(hour) && (hour = pad(Number(hour)))"
            @keydown.enter.prevent="apply"
          /><span>:</span
          ><input
            v-model="minute"
            aria-label="分"
            inputmode="numeric"
            maxlength="2"
            @blur="/^\d{1,2}$/.test(minute) && (minute = pad(Number(minute)))"
            @keydown.enter.prevent="apply"
          />
        </div>
      </div>
      <p v-if="draftError" class="date-time-error" role="alert">
        {{ draftError }}
      </p>
      <div class="date-calendar-footer">
        <span>{{ day.replaceAll("-", "/") }}</span
        ><button
          type="button"
          class="button primary compact"
          :disabled="!!draftError"
          @click="apply"
        >
          确定
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.date-time-field {
  min-width: 0;
}
.date-time-input {
  display: flex;
  align-items: center;
  height: 34px;
  border: 1px solid #dce5df;
  border-radius: 7px;
  background: white;
}
.date-time-input:focus-within,
.date-time-input.is-open {
  border-color: #759980;
}
.date-time-input.is-invalid {
  border-color: #aa4e42;
}
.date-time-input input {
  min-width: 0;
  height: 32px;
  padding: 7px 10px;
  border: 0;
  border-radius: 7px;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}
.date-time-input input::placeholder {
  color: #66796d;
}
.date-time-input button {
  display: grid;
  place-items: center;
  flex-shrink: 0;
  width: 34px;
  height: 32px;
  color: #53745e;
  border-radius: 6px;
}
.date-time-input button:hover {
  background: var(--mint);
}
.date-time-hint {
  margin: 5px 0 0;
  font-size: 11px;
  line-height: 1.6;
  color: #52655b;
  overflow-wrap: anywhere;
}
.date-time-error {
  color: #aa4e42;
  font-size: 11px;
  line-height: 1.6;
}
.date-time-popover {
  position: fixed;
  inset: auto;
  margin: 0;
  width: 312px;
  max-width: calc(100vw - 16px);
  max-height: calc(100dvh - 16px);
  overflow: auto;
  padding: 14px;
  border: 1px solid #dce5df;
  border-radius: 12px;
  background: #fff;
  color: var(--ink);
  box-shadow: 0 12px 36px #18352e24;
  font-size: 12px;
}
.date-calendar-heading {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 12px;
}
.date-calendar-arrow {
  display: grid;
  place-items: center;
  width: 30px;
  height: 32px;
  flex-shrink: 0;
  border-radius: 6px;
}
.date-calendar-arrow:hover:not(:disabled) {
  background: var(--mint);
}
.date-calendar-year {
  display: flex;
  align-items: center;
  flex-shrink: 0;
  gap: 3px;
}
.date-calendar-year input {
  width: 54px;
  height: 32px;
  padding: 5px 4px;
  border-radius: 6px;
  text-align: center;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}
.date-calendar-heading :deep(.select-field) {
  min-width: 0;
}
.date-calendar-heading :deep(.select-trigger) {
  min-height: 32px;
  height: 32px;
  padding: 5px 8px;
}
.date-calendar-grid {
  display: grid;
  grid-template-columns: repeat(7, minmax(0, 1fr));
  gap: 3px;
}
.date-calendar-weekday {
  display: grid;
  place-items: center;
  height: 24px;
  color: #52655b;
  font-size: 11px;
}
.date-calendar-grid button {
  height: 32px;
  border-radius: 6px;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}
.date-calendar-grid button:hover:not(:disabled) {
  background: var(--mint);
}
.date-calendar-grid button[aria-current="date"] {
  box-shadow: inset 0 0 0 1px #759980;
}
.date-calendar-grid button.is-selected,
.date-calendar-grid button.is-selected:hover {
  background: var(--accent);
  color: white;
}
.date-calendar-quick {
  display: flex;
  gap: 6px;
  padding: 12px 0;
}
.date-calendar-quick button {
  border: 1px solid var(--line);
  padding: 5px 9px;
  border-radius: 6px;
  font-size: 11px;
}
.date-calendar-quick button:hover {
  background: var(--mint);
}
.date-calendar-time {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 12px 0;
  border-top: 1px solid var(--line);
}
.date-calendar-time small {
  display: block;
  margin-top: 3px;
  font-size: 10px;
  color: #52655b;
}
.date-calendar-time > div {
  display: flex;
  align-items: center;
  gap: 6px;
}
.date-calendar-time input {
  width: 42px;
  height: 32px;
  padding: 5px;
  text-align: center;
  border-radius: 6px;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}
.date-calendar-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding-top: 12px;
  border-top: 1px solid var(--line);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
}
.date-calendar-footer .button {
  min-height: 30px;
  padding: 5px 13px;
}
</style>
