import type { TokenPermission, TokenAuthorization } from "./types";
export const tokenPermissions: { value: TokenPermission; label: string }[] = [
  { value: "read", label: "读取图库" },
  { value: "upload", label: "上传图片" },
  { value: "edit", label: "修改图片" },
  { value: "delete", label: "删除图片" },
  { value: "albums", label: "管理相册" },
  { value: "share", label: "管理分享" },
];
export function tokenSummary(policy: TokenAuthorization): string {
  if (!policy.all_albums) return `${policy.albums.length} 个指定相册，分别授权`;
  return `全部本人相册 · ${tokenPermissions
    .filter((v) => policy.permissions.includes(v.value))
    .map((v) => v.label)
    .join("、")}`;
}
