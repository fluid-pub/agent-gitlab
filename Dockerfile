# syntax=docker/dockerfile:1
# Fluid GitLab execution agent — git and chown required for gitlab.repo.checkout_mr.

FROM golang:1.26-bookworm AS build

ARG BINARY_NAME=fluid-agent-gitlab
ARG VERSION=0.0.0
ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src

COPY go.mod go.sum ./
COPY core ./core
COPY cmd ./cmd
COPY internal ./internal

RUN go mod download

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags "-s -w -X main.Version=${VERSION}" \
        -o /out/workload ./cmd

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/workload /usr/local/bin/workload

# checkout_mr may use chown; typical deployment runs this agent with host workspace mounts.
USER root

ENTRYPOINT ["/usr/local/bin/workload"]
CMD ["-config", "/etc/fluid/config/config.yaml"]
