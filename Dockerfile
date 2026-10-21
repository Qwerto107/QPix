# 前后端在同一次镜像构建中生成，最终只运行一个 Go 进程。
FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS backend
WORKDIR /src
ENV CGO_ENABLED=0
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
ARG TARGETOS
ARG TARGETARCH
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -tags=nodynamic -trimpath -ldflags="-s -w" -o /out/qpix ./cmd/qpix

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && mkdir -p /data && chown 65532:65532 /data
COPY --from=backend /out/qpix /usr/local/bin/qpix
COPY --from=frontend /src/dist /app/web
ENV QPIX_ADDR=0.0.0.0:8080 QPIX_WEB_ROOT=/app/web \
    QPIX_DB_DSN=/data/qpix.db QPIX_STORAGE_ROOT=/data/uploads QPIX_MASTER_KEY_FILE=/data/master.key
USER 65532:65532
WORKDIR /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/api/v1/health || exit 1
ENTRYPOINT ["/usr/local/bin/qpix"]
