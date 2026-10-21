#!/usr/bin/env bash
set -euo pipefail

image="ghcr.io/${GITHUB_REPOSITORY,,}"
tags=("$image:sha-${GITHUB_SHA:0:12}")

if [[ "$GITHUB_EVENT_NAME" == "push" && "$GITHUB_REF" == "refs/heads/main" ]]; then
  tags+=("$image:dev")
elif [[ "$GITHUB_EVENT_NAME" == "push" && "$GITHUB_REF" == refs/tags/* ]]; then
  version="${GITHUB_REF#refs/tags/v}"
  # 仅正式语义版本更新 latest，预发布版本只更新自身标签。
  if [[ ${#version} -gt 128 || ! "$GITHUB_REF" =~ ^refs/tags/v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$ ]]; then
    printf '版本标签必须为 vX.Y.Z 或 vX.Y.Z-预发布标识\n' >&2
    exit 1
  fi
  tags+=("$image:$version")
  if [[ "$version" != *-* ]]; then tags+=("$image:latest"); fi
elif [[ "$GITHUB_EVENT_NAME" == "workflow_dispatch" ]]; then
  if [[ "$GITHUB_REF" != "refs/heads/main" ]]; then
    printf '手动发布请选择 main 分支\n' >&2
    exit 1
  fi
  version="${CUSTOM_VERSION:-}"
  if [[ -n "$version" ]]; then
    # 输入通过环境变量传入并严格校验，不能覆盖保留标签或作为 Shell 代码执行。
    if [[ ! "$version" =~ ^[a-z0-9][a-z0-9_.-]{0,127}$ || "$version" == "latest" || "$version" == "dev" || "$version" == sha-* ]]; then
      printf '自定义标签必须为小写 Docker 标签，且不能使用 latest、dev 或 sha-*\n' >&2
      exit 1
    fi
    tags+=("$image:$version")
  fi
else
  printf '当前事件不允许发布镜像\n' >&2
  exit 1
fi

{
  printf 'tags<<QPIX_IMAGE_TAGS\n'
  printf '%s\n' "${tags[@]}"
  printf 'QPIX_IMAGE_TAGS\n'
} >> "${GITHUB_OUTPUT:?缺少 GITHUB_OUTPUT}"
