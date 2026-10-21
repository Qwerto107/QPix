FROM golang:1.26-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN go build -tags=nodynamic -trimpath -ldflags="-s -w" -o /out/qpix ./cmd/qpix

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && mkdir -p /data && chown 65532:65532 /data
COPY --from=build /out/qpix /usr/local/bin/qpix
USER 65532:65532
WORKDIR /data
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/qpix"]
