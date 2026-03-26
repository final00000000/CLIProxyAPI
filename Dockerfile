FROM golang:1.26-alpine AS builder

WORKDIR /app

# Install build dependencies for CGO and SQLite
RUN apk add --no-cache gcc musl-dev sqlite-dev

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN go mod tidy

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

# Enable CGO for SQLite support
RUN set -eux; \
    VERSION_VALUE="${VERSION}"; \
    if [ -z "${VERSION_VALUE}" ] || [ "${VERSION_VALUE}" = "dev" ]; then \
      VERSION_VALUE="$(git describe --tags --always --dirty 2>/dev/null || printf '%s' dev)"; \
    fi; \
    COMMIT_VALUE="${COMMIT}"; \
    if [ -z "${COMMIT_VALUE}" ] || [ "${COMMIT_VALUE}" = "none" ]; then \
      COMMIT_VALUE="$(git rev-parse --short HEAD 2>/dev/null || printf '%s' none)"; \
    fi; \
    BUILD_DATE_VALUE="${BUILD_DATE}"; \
    if [ -z "${BUILD_DATE_VALUE}" ] || [ "${BUILD_DATE_VALUE}" = "unknown" ]; then \
      BUILD_DATE_VALUE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"; \
    fi; \
    CGO_ENABLED=1 GOOS=linux go build \
      -ldflags="-s -w -X 'main.Version=${VERSION_VALUE}' -X 'main.Commit=${COMMIT_VALUE}' -X 'main.BuildDate=${BUILD_DATE_VALUE}'" \
      -o ./CLIProxyAPI \
      ./cmd/server/

FROM alpine:3.22.0

# Install runtime dependencies
RUN apk add --no-cache tzdata sqlite-libs

RUN mkdir /CLIProxyAPI

COPY --from=builder ./app/CLIProxyAPI /CLIProxyAPI/CLIProxyAPI

COPY config.example.yaml /CLIProxyAPI/config.example.yaml

WORKDIR /CLIProxyAPI

EXPOSE 8317

ENV TZ=Asia/Shanghai

RUN cp /usr/share/zoneinfo/${TZ} /etc/localtime && echo "${TZ}" > /etc/timezone

CMD ["./CLIProxyAPI"]
