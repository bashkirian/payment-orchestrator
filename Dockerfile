FROM golang:1.26-alpine

RUN apk add --no-cache \
    curl \
    git \
    make \
    unzip \
    docker-cli \
    docker-cli-compose

# Install protoc from official release (apk protobuf only ships the runtime library, not the compiler).
# Pin to the exact version used by developers (libprotoc 34.1 → header "v7.34.1").
# To upgrade: change PROTOC_VERSION here, rebuild+push the builder image, then re-run make proto-gen locally.
RUN PROTOC_VERSION=34.1 && \
    ARCH=$(uname -m) && \
    case "$ARCH" in \
      x86_64)  PROTOC_ARCH="linux-x86_64" ;; \
      aarch64) PROTOC_ARCH="linux-aarch_64" ;; \
      *) echo "Unsupported arch: $ARCH" && exit 1 ;; \
    esac && \
    curl -sSL "https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-${PROTOC_ARCH}.zip" \
      -o /tmp/protoc.zip && \
    unzip -q /tmp/protoc.zip -d /usr/local && \
    rm /tmp/protoc.zip

# Install golangci-lint
RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b /usr/local/bin

# Install protoc Go plugins
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest \
 && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Install goose migration tool
RUN go install github.com/pressly/goose/v3/cmd/goose@latest
