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
