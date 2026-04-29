# ============================================================
# Stage 1: Build Web Frontend
# ============================================================
FROM node:22-alpine AS web-builder

WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./
RUN npm run build

# ============================================================
# Stage 2: Build Go Binary
# ============================================================
FROM golang:1.26-alpine AS go-builder

RUN apk add --no-cache git

WORKDIR /app

# Copy Go module files
COPY go.mod go.sum ./
RUN go mod download

# Copy web dist from frontend build stage (required for go:embed)
COPY --from=web-builder /app/web/dist ./web/dist

# Copy source code
COPY . ./

# Build with embedassets tag to include web frontend
RUN CGO_ENABLED=0 GOOS=linux go build \
    -tags embedassets \
    -ldflags="-s -w -extldflags '-static'" \
    -o /kanflux .

# ============================================================
# Stage 3: Runtime Image
# ============================================================
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=go-builder /kanflux /app/kanflux

EXPOSE 8765

ENTRYPOINT ["/app/kanflux"]
CMD ["gateway", "start", "--config", "/app/config.yaml"]
