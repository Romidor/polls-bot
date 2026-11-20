# ---------- Этап сборки ----------
FROM golang:alpine AS builder

WORKDIR /app

# git нужен для `go mod download`, ca-certificates — для HTTPS-запросов
RUN apk add --no-cache git ca-certificates

# Сначала копируем только файлы модулей — для кэширования слоёв
COPY go.mod go.sum ./
RUN go mod download

# Копируем остальной код и собираем бинарник
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o vk-poll-bot .

# ---------- Финальный (рантайм) этап ----------
FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/vk-poll-bot .

CMD ["./vk-poll-bot"]