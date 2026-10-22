# 本地源码构建使用前端与后端阶段；CI 镜像直接复用已打包的二进制。
FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build
WORKDIR /src
COPY scripts/ ./scripts/
RUN node scripts/prepare-webui.mjs

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS backend
WORKDIR /src
ENV CGO_ENABLED=0
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=frontend /src/backend/internal/webui/dist ./internal/webui/dist
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
RUN case "${TARGETARCH}/${TARGETVARIANT}" in \
      arm/v5) export GOARM=5 ;; \
      arm/v6) export GOARM=6 ;; \
      arm/*) echo "不支持的 ARM 变体" >&2; exit 1 ;; \
    esac \
    && GOOS=$TARGETOS GOARCH=$TARGETARCH go build -tags=nodynamic,with_frontend -trimpath -ldflags="-s -w" -o /out/qpix ./cmd/qpix

# 从构建主机获取证书和时区文件，运行层无需目标架构基础镜像或模拟器。
FROM --platform=$BUILDPLATFORM alpine:3.23 AS runtime-files
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 65532 qpix && adduser -D -H -u 65532 -G qpix qpix \
    && mkdir /data /runtime-tmp && chown 65532:65532 /data && chmod 1777 /runtime-tmp

FROM scratch AS runtime
COPY --from=runtime-files /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=runtime-files /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=runtime-files /etc/passwd /etc/passwd
COPY --from=runtime-files /etc/group /etc/group
COPY --from=runtime-files --chown=65532:65532 /data /data
COPY --from=runtime-files --chmod=1777 /runtime-tmp /tmp
ENV QPIX_ADDR=0.0.0.0:8080 \
    QPIX_DB_DSN=/data/qpix.db QPIX_STORAGE_ROOT=/data/uploads QPIX_MASTER_KEY_FILE=/data/master.key
USER 65532:65532
WORKDIR /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD ["/qpix", "healthcheck"]
ENTRYPOINT ["/qpix"]

FROM --platform=$BUILDPLATFORM alpine:3.23 AS release-binary
ARG TARGETARCH
ARG TARGETVARIANT
COPY .release/binaries/ /binaries/
RUN platform="${TARGETARCH}" \
    && if [ "$TARGETARCH" = arm ]; then platform="arm${TARGETVARIANT}"; fi \
    && cp "/binaries/qpix-linux-${platform}/qpix" /qpix && chmod 0755 /qpix

FROM runtime AS release
COPY --from=release-binary /qpix /qpix

# 默认目标保留 docker compose --build 的源码构建方式。
FROM runtime AS standalone
COPY --from=backend /out/qpix /qpix
