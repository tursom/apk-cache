ARG GO_IMAGE=golang:alpine
ARG RUNTIME_IMAGE=alpine:latest

# 构建时：Alpine apk 源（用于 apk add）
# 例: https://mirrors.aliyun.com/alpine
ARG ALPINE_APK_MIRROR=https://dl-cdn.alpinelinux.org/alpine

# 构建时：Go 模块代理（用于 go build 拉取依赖）
# 例: https://goproxy.cn,direct
ARG GOPROXY=https://proxy.golang.org,direct

FROM ${GO_IMAGE} AS builder

# ARG defined before FROM must be re-declared per-stage
ARG ALPINE_APK_MIRROR
ARG GOPROXY

RUN sed -i "s|https\\?://dl-cdn\\.alpinelinux\\.org/alpine|${ALPINE_APK_MIRROR}|g" /etc/apk/repositories && \
    apk add --no-cache upx gzip

WORKDIR /app
COPY . entrypoint.sh /app/

ENV GOPROXY=${GOPROXY}

RUN sh build.sh && \
    upx --best --lzma ./apk-cache

FROM ${RUNTIME_IMAGE}

WORKDIR /app
COPY --from=builder /app/apk-cache /app/entrypoint.sh /app/

CMD ["/app/entrypoint.sh"]
