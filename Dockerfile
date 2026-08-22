# --- STAGE 1: Build environment ---
FROM golang:alpine AS builder

WORKDIR /app

# 1. Copy go.mod & go.sum and cache Go modules
COPY go.mod go.sum ./
RUN go mod download

# 2. Copy the remaining source code
COPY . .

# 3. Build static Go binary for Linux
# -ldflags="-s -w" strips debug symbols and reduces binary size
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o ceres .

# --- STAGE 2: Lightweight runtime environment ---
FROM alpine:latest

# IMPORTANT for OpenAI & Discord: Install SSL certificates and timezone data
RUN apk --no-cache add ca-certificates tzdata ncurses-terminfo python3
COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /usr/local/bin/


ENV TERM=xterm-256color
ENV COLORTERM=truecolor

WORKDIR /app

# Copy the compiled binary from the build stage
COPY --from=builder /app/ceres /usr/local/bin/ceres

# Command to run the application
CMD ["ceres"]
