ARG GO_IMAGE=golang:alpine
ARG NODE_IMAGE=node:22-alpine
ARG RUNTIME_IMAGE=alpine:latest
ARG GOPROXY=https://proxy.golang.org,direct

FROM --platform=$BUILDPLATFORM ${NODE_IMAGE} AS admin-ui-builder
WORKDIR /src/internal/admin/web
COPY internal/admin/web/package*.json ./
RUN npm ci
COPY internal/admin/web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS builder
ARG GOPROXY
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
WORKDIR /src
COPY . .
COPY --from=admin-ui-builder /src/internal/admin/static ./internal/admin/static
ENV GOPROXY=${GOPROXY}
RUN set -eu; \
	export CGO_ENABLED=0 GOOS="${TARGETOS:-linux}" GOARCH="${TARGETARCH:-amd64}"; \
	if [ -n "${TARGETVARIANT:-}" ]; then export GOARM="${TARGETVARIANT#v}"; fi; \
	sh build.sh --skip-admin-ui

FROM ${RUNTIME_IMAGE}
WORKDIR /app
COPY --from=builder /src/apk-cache /app/apk-cache
COPY entrypoint.sh /app/entrypoint.sh
EXPOSE 3142
CMD ["/app/entrypoint.sh"]
