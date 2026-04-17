FROM golang:1.26-alpine

RUN apk add --no-cache \
    curl \
    git \
    make

# Install golangci-lint
RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b /usr/local/bin
