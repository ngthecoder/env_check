# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o envcheck ./cmd/main.go

# Runtime stage
FROM alpine:3.19

RUN apk add --no-cache \
    bash \
    python3 \
    nodejs \
    npm \
    git

COPY --from=builder /app/envcheck /usr/local/bin/envcheck

EXPOSE 8484

ENTRYPOINT ["envcheck"]
CMD ["--port", "8484", "--interval", "60s"]
