# ==========================================
# Stage 1: Build the Vite/Vanilla JS Frontend
# ==========================================
FROM node:20-alpine AS frontend-builder
WORKDIR /app/frontend

# Install dependencies and build
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# ==========================================
# Stage 2: Build the Go Backend
# ==========================================
FROM golang:1.25-alpine AS backend-builder
WORKDIR /app/backend

# go-sqlite3 requires cgo, which requires a C compiler
RUN apk add --no-cache gcc musl-dev

# Download Go modules
COPY backend/go.mod backend/go.sum ./
RUN go mod download

# Copy source and build the binary
COPY backend/ ./
RUN CGO_ENABLED=1 GOOS=linux go build -o shipper cmd/shipper/main.go

# ==========================================
# Stage 3: The Final Minimal Runtime
# ==========================================
FROM alpine:latest

# Install runtime dependencies: Git (for cloning) and Docker CLI (for buildx)
RUN apk add --no-cache git docker-cli docker-cli-buildx tzdata git-lfs && git lfs install

WORKDIR /app

# Copy the compiled Go binary
COPY --from=backend-builder /app/backend/shipper /app/shipper

# Copy the compiled Vite frontend static files
COPY --from=frontend-builder /app/frontend/dist /app/static

# Set default environment variables
ENV SHIPPER_PORT=8080
ENV SHIPPER_STATIC_DIR=/app/static
ENV SHIPPER_DATA_DIR=/app/data
ENV SHIPPER_DB_PATH=/app/data/shipper.db
ENV SHIPPER_REGISTRY=localhost:5000

LABEL org.opencontainers.image.source https://github.com/Sarin-jacob/Shipper

# Create the data directory so SQLite has a place to write
RUN mkdir -p /app/data

EXPOSE 8080

ENTRYPOINT ["/app/shipper"]