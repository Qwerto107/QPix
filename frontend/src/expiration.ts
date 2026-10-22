import { formatLocalDateTime, parseLocalDateTime } from "./dateTime";

const hour = 60 * 60 * 1000;
const day = 24 * hour;
const presets = [
  { value: "1h", label: "1 小时", duration: hour },
  { value: "6h", label: "6 小时", duration: 6 * hour },
  { value: "12h", label: "12 小时", duration: 12 * hour },
  ...[1, 3, 7, 14, 30, 90, 180, 365].map((days) => ({
    value: String(days),
    label: `${days} 天`,
    duration: days * day,
  })),
];

export const expirationOptions = [
  { value: "permanent", label: "永久有效" },
  ...presets.map(({ value, label }) => ({ value, label })),
  { value: "custom", label: "自定义到期时间" },
];

export function resolveExpiration(
  period: string,
  custom: string,
  original?: string | null,
  now = Date.now(),
): string | null {
  if (period === "permanent") return null;
  let date: Date | null;
  if (period === "custom") {
    date = parseLocalDateTime(custom);
    const previous = original ? new Date(original) : null;
    // 只编辑其他信息时保留原时刻的秒数，与日期控件的分钟精度保持一致。
    if (
      date &&
      previous &&
      Number.isFinite(previous.getTime()) &&
      formatLocalDateTime(date) === formatLocalDateTime(previous)
    )
      date = previous;
  } else {
    const preset = presets.find((item) => item.value === period);
    date = preset ? new Date(now + preset.duration) : null;
  }
  if (!date || !Number.isFinite(date.getTime()) || date.getTime() <= now) {
    throw new Error("请选择有效且晚于当前时间的到期时间");
  }
  return date.toISOString();
}
