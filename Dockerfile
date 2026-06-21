ARG GO_IMAGE=golang:alpine
ARG NODE_IMAGE=node:22-alpine
ARG RUNTIME_IMAGE=alpine:latest
ARG GOPROXY=https://proxy.golang.org,direct

FROM ${NODE_IMAGE} AS admin-ui-builder
WORKDIR /src/internal/admin/web
COPY internal/admin/web/package*.json ./
RUN npm ci
COPY internal/admin/web/ ./
RUN npm run build

FROM ${GO_IMAGE} AS builder
ARG GOPROXY
WORKDIR /src
COPY . .
COPY --from=admin-ui-builder /src/internal/admin/static ./internal/admin/static
ENV GOPROXY=${GOPROXY}
RUN sh build.sh --skip-admin-ui

FROM ${RUNTIME_IMAGE}
WORKDIR /app
COPY --from=builder /src/apk-cache /app/apk-cache
COPY entrypoint.sh /app/entrypoint.sh
EXPOSE 3142
CMD ["/app/entrypoint.sh"]
