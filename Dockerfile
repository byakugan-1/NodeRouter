# Stage 1: Build
FROM golang:1.22-alpine AS builder
RUN apk add --no-cache git
WORKDIR /build
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o noderouter .

# Stage 2: Runtime
FROM alpine:3.19
RUN apk add --no-cache ca-certificates tor
WORKDIR /app
COPY --from=builder /build/noderouter .
COPY --from=builder /build/config.yaml .
COPY --from=builder /build/templates/ ./templates/
COPY --from=builder /build/static/ ./static/
COPY --from=builder /build/entrypoint.sh /entrypoint.sh
# Make config world-writable so any UID can write to it
RUN chmod 666 /app/config.yaml
# Make entrypoint executable
RUN chmod +x /entrypoint.sh
# Create Tor data directory with proper permissions
RUN mkdir -p /var/lib/tor/noderouter && \
    chmod 700 /var/lib/tor/noderouter
# Create torrc directory
RUN mkdir -p /etc/tor && chmod 777 /etc/tor
EXPOSE 5000 9050
# Run as root (needed for Tor hidden service)
ENTRYPOINT ["/entrypoint.sh"]
