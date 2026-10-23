import type { ImageItem } from "./types";

// 会话凭据由 HttpOnly Cookie 保存，CSRF 值只保留在内存中。
let csrf = "";
let expired: () => void = () => {};
export function setCSRF(value: string) {
  csrf = value;
}
export function onSessionExpired(callback: () => void) {
  expired = callback;
}

export class APIError extends Error {
  constructor(
    message: string,
    public status: number,
  ) {
    super(message);
  }
}

export async function api<T>(
  path: string,
  method = "GET",
  body?: unknown,
): Promise<T> {
  let response: Response;
  try {
    response = await fetch(`/api/v1${path}`, {
      method,
      credentials: "same-origin",
      headers: {
        ...(body === undefined ? {} : { "Content-Type": "application/json" }),
        ...(method === "GET" ? {} : { "X-CSRF-Token": csrf }),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new APIError("无法连接服务，请检查网络后重试", 0);
  }
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    if (response.status === 401 && !path.endsWith("/auth/login")) expired();
    throw new APIError(
      data.error || `请求失败（${response.status}）`,
      response.status,
    );
  }
  return data as T;
}

export function uploadImage(
  file: File,
  albumID: string,
  progress: (value: number) => void,
  options?: {
    storage_id: string;
    compression: string;
    quality: number;
    random_name: boolean;
  },
): Promise<ImageItem> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    const form = new FormData();
    form.append("file", file);
    if (albumID) form.append("album_id", albumID);
    if (options?.storage_id) form.append("storage_id", options.storage_id);
    if (options?.compression) form.append("compression", options.compression);
    if (options) form.append("quality", String(options.quality));
    if (options) form.append("random_name", String(options.random_name));
    xhr.open("POST", "/api/v1/images");
    xhr.setRequestHeader("X-CSRF-Token", csrf);
    xhr.timeout = 120_000;
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable)
        progress(Math.round((event.loaded / event.total) * 100));
    };
    xhr.onload = () => {
      let data: ImageItem & { error?: string };
      try {
        data = JSON.parse(xhr.responseText);
      } catch {
        reject(new Error("服务器返回了无效响应"));
        return;
      }
      if (xhr.status >= 200 && xhr.status < 300) resolve(data);
      else {
        if (xhr.status === 401) expired();
        reject(new Error(data.error || "上传失败，请重试"));
      }
    };
    xhr.onerror = () => reject(new Error("网络中断，请重试"));
    xhr.ontimeout = () => reject(new Error("上传超时，请重试"));
    xhr.send(form);
  });
}
