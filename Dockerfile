# --- Этап 1: Сборка (Builder) ---
FROM golang:1.23-alpine AS builder

# Устанавливаем зависимости для сборки (CGO нужен для sqlite3)
RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /build

# Копируем файлы зависимостей для кэширования слоев
COPY go.mod go.sum ./
RUN go mod download

# Копируем весь исходный код
COPY . .

# Собираем приложение
# CGO_ENABLED=1 - необходимо для sqlite3
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -o asutp-collector \
    ./cmd/collector

# --- Этап 2: Финальный образ (Final) ---
FROM alpine:3.19

# Устанавливаем runtime зависимости
RUN apk --no-cache add ca-certificates sqlite-libs tzdata

# Создаем непривилегированного пользователя
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /app

# Копируем скомпилированный бинарник
COPY --from=builder /build/asutp-collector .

# Создаем директории для конфигов и буфера
RUN mkdir -p /app/config /var/lib/asutp && \
    chown -R appuser:appuser /app /var/lib/asutp

USER appuser

EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

ENV CONFIG_PATH=/app/config/config.yaml

CMD ["./asutp-collector"]
