export interface User {
  default_album_id: string;
  created_at: string;
  id: string;
  username: string;
  role: "admin" | "user";
  enabled: boolean;
  quota_bytes: number;
  max_file_bytes: number;
  used_bytes?: number;
}
export interface ImageItem {
  owner_name?: string;
  owner_role?: "admin" | "user";
  owner_id?: string;
  can_manage: boolean;
  id: string;
  album_id: string | null;
  name: string;
  format: string;
  size: number;
  original_size: number;
  optimized: boolean;
  compression_status: string;
  storage_id: string;
  width: number;
  height: number;
  created_at: string;
  url: string;
  thumbnail_url: string;
}
export interface Album {
  guest_visible: boolean;
  owner_id: string;
  owner_name: string;
  owner_role: "admin" | "user";
  is_default: boolean;
  referer_mode: "inherit" | "custom" | "unrestricted";
  referers: string[];
  allow_empty_referer: boolean;
  can_upload: boolean;
  id: string;
  name: string;
  count: number;
  created_at: string;
}
export type TokenPermission =
  | "read"
  | "upload"
  | "edit"
  | "delete"
  | "albums"
  | "share";
export interface TokenAuthorization {
  all_albums: boolean;
  permissions: TokenPermission[];
  albums: { album_id: string; permissions: TokenPermission[] }[];
}
export interface APIToken {
  authorization: TokenAuthorization;
  owner_id: string;
  owner_name: string;
  owner_role: "admin" | "user";
  last_used_at: string | null;
  revoked_at: string | null;
  id: string;
  name: string;
  prefix: string;
  created_at: string;
  expires_at: string | null;
}
export interface Stats {
  images: number;
  bytes: number;
  albums: number;
  unfiled: number;
  today: number;
  pending_deletions: number;
}
export interface Settings {
  app_url: string;
  secure_cookie: boolean;
  trusted_proxies: string[];
  public_url: string;
  max_upload_bytes: number;
  max_pixels: number;
  database: string;
  storage: string;
  default_storage: string;
  storage_name: string;
  processing: ProcessingSettings;
  version: string;
  formats: string[];
}
export type ImagePage = RecordPage<ImageItem>;

export interface ProcessingSettings {
  mode: "none" | "preserve" | "webp";
  quality: number;
}
export interface StorageProfile {
  id: string;
  name: string;
  kind: "local" | "s3";
  enabled: boolean;
  root: string;
  endpoint: string;
  region: string;
  bucket: string;
  prefix: string;
  path_style: boolean;
  is_default: boolean;
  has_credentials: boolean;
  images: number;
}
export interface AccessRule {
  scope: "admin" | "upload" | "images";
  ip_enabled: boolean;
  ips: string[];
  referer_enabled: boolean;
  referers: string[];
  allow_empty_referer: boolean;
}

export interface UploadOptions {
  max_upload_bytes: number;
  max_pixels: number;
  default_storage: string;
  processing: ProcessingSettings;
  storages: { id: string; name: string }[];
  used_bytes: number;
  quota_bytes: number;
}

export interface ShareItem {
  id: string;
  kind: "image";
  owner_id: string;
  owner_name: string;
  owner_role: "admin" | "user";
  target_id: string;
  title: string;
  url: string;
  has_password: boolean;
  expires_at: string | null;
  revoked_at: string | null;
  created_at: string;
}

export interface RecordPage<T> {
  items: T[];
  total: number | null;
  page: number;
  limit: number;
  has_more: boolean;
  next_cursor: string;
}
