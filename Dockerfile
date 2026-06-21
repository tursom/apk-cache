ARG GO_IMAGE=golang:alpine
ARG RUNTIME_IMAGE=alpine:latest
ARG GOPROXY=https://proxy.golang.org,direct

FROM ${GO_IMAGE} AS builder
ARG GOPROXY
WORKDIR /src
COPY . .
ENV GOPROXY=${GOPROXY}
RUN sh build.sh

FROM ${RUNTIME_IMAGE}
WORKDIR /app
COPY --from=builder /src/apk-cache /app/apk-cache
COPY entrypoint.sh /app/entrypoint.sh
EXPOSE 3142
CMD ["/app/entrypoint.sh"]
