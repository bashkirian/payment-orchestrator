FROM golang:1.26-alpine

RUN apk add --no-cache \
    curl \
    git \
    make \
    unzip \
    docker-cli \
    docker-cli-compose

# Install protoc. Version is pinned in .protoc-version at the repo root.
# To upgrade: change .protoc-version, rebuild+push the builder image, re-run make proto-gen locally.
COPY .protoc-version /tmp/.protoc-version
RUN PROTOC_VERSION=$(cat /tmp/protoc-version 2>/dev/null || cat /tmp/.protoc-version) && \
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

COPY .golangci-version /tmp/.golangci-version

# Install golangci-lint. Version is pinned in .golangci-version at the repo root.
# To upgrade: change .golangci-version, rebuild+push the builder image.
RUN GOLANGCI_VERSION=$(cat /tmp/.golangci-version) && \
    curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
      | sh -s -- -b /usr/local/bin "${GOLANGCI_VERSION}"

# Install protoc Go plugins
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest \
 && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Install goose migration tool
RUN go install github.com/pressly/goose/v3/cmd/goose@latest
