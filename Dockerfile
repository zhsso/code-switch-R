# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM node:22-alpine AS webui
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY frontend/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS backend
WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . ./
COPY --from=webui /src/frontend/dist ./frontend/dist
ARG VERSION=v2.6.44-codexplus.1
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -tags webui_embed -trimpath -ldflags "-s -w -X main.AppVersion=${VERSION}" -o /out/codeswitch .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 codeswitch \
    && adduser -S -D -H -u 10001 -G codeswitch codeswitch \
    && mkdir -p /data/.code-switch \
    && chown -R 10001:10001 /data
COPY --from=backend --chown=10001:10001 /out/codeswitch /usr/local/bin/codeswitch
ENV HOME=/data \
    GIN_MODE=release \
    CODESWITCH_WEB_PORT=8080 \
    CODESWITCH_RELAY_PORT=18100
USER 10001:10001
VOLUME ["/data"]
EXPOSE 8080 18100
ENTRYPOINT ["/usr/local/bin/codeswitch"]
