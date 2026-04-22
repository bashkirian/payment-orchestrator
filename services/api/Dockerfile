FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/api ./services/api/cmd/server

FROM alpine:3.21

RUN adduser -D -H -u 10001 appuser

WORKDIR /app
COPY --from=builder /bin/api /usr/local/bin/api

EXPOSE 8080

USER appuser

ENTRYPOINT ["/usr/local/bin/api"]