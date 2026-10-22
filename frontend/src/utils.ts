import type { ImageItem } from "./types";

export function bytes(size: number): string {
  if (!size) return "0 B";
  const unit = Math.min(Math.floor(Math.log(size) / Math.log(1024)), 3);
  return `${(size / 1024 ** unit).toFixed(unit === 0 ? 0 : 1)} ${["B", "KB", "MB", "GB"][unit]}`;
}
export function date(value: string): string {
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(new Date(value));
}
export function absolute(value: string): string {
  return new URL(value, window.location.origin).href;
}
export type LinkFormat = "url" | "markdown" | "html";
export function imageLink(image: ImageItem, format: LinkFormat): string {
  const url = absolute(image.url);
  if (format === "markdown") {
    const name = image.name
      .replace(/[\r\n]/g, " ")
      .replace(/[\[\]\\]/g, "\\$&");
    return `![${name}](${url.replace(/\(/g, "%28").replace(/\)/g, "%29")})`;
  }
  if (format === "html")
    return `<img src="${escapeHTML(url)}" alt="${escapeHTML(image.name)}" />`;
  return url;
}
function escapeHTML(value: string): string {
  return value.replace(
    /[&<>"']/g,
    (char) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        char
      ]!,
  );
}
export async function copy(value: string) {
  await navigator.clipboard.writeText(value);
}
export function message(error: unknown): string {
  return error instanceof Error ? error.message : "操作失败，请重试";
}

export function timestamp(value?: string | null): string {
  return value
    ? new Date(value).toLocaleString("zh-CN", { hour12: false })
    : "—";
}
export function remaining(value: string | null, now = Date.now()): string {
  if (!value) return "永久有效";
  const ms = new Date(value).getTime() - now;
  if (ms <= 0) return "已到期";
  if (ms >= 86400000) return `剩余 ${Math.ceil(ms / 86400000)} 天`;
  if (ms >= 3600000) return `剩余 ${Math.ceil(ms / 3600000)} 小时`;
  return `剩余 ${Math.max(1, Math.ceil(ms / 60000))} 分钟`;
}
// 偏好存储不可用时仍可正常浏览，且仅接受已知选项。
export function preference(
  key: string,
  fallback: string,
  allowed: string[],
): string {
  try {
    const value = localStorage.getItem(`qpix.${key}`);
    return value && allowed.includes(value) ? value : fallback;
  } catch {
    return fallback;
  }
}
export function savePreference(key: string, value: string) {
  try {
    localStorage.setItem(`qpix.${key}`, value);
  } catch {
    /* 隐私模式不影响当前操作。 */
  }
}
