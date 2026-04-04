# Stage 1: Build
FROM golang:1.22-alpine AS builder
RUN apk add --no-cache git
WORKDIR /build
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o noderouter .

# Stage 2: Runtime
FROM alpine:3.19
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /build/noderouter .
COPY --from=builder /build/config.yaml .
COPY --from=builder /build/templates/ ./templates/
COPY --from=builder /build/static/ ./static/
EXPOSE 5000
USER 1000:1000
CMD ["./noderouter"]
