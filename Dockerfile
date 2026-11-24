# syntax=docker/dockerfile:1

# ---------- stage 1: сборка зависимостей ----------
FROM python:3.11-slim AS builder

ENV PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

WORKDIR /build

# Компиляторы нужны только на этапе сборки wheel-пакетов
RUN apt-get update \
 && apt-get install -y --no-install-recommends build-essential \
 && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .

# Ставим зависимости в отдельную папку, чтобы перенести в финальный образ
RUN pip install --prefix=/install -r requirements.txt

# ---------- stage 2: рантайм ----------
FROM python:3.11-slim

# Метаданные
LABEL maintainer="you@example.com" \
      description="VK poll bot for training attendance"

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    TZ=Europe/Samara

# --- системные зависимости ---
# tzdata        — база часовых поясов для zoneinfo (Europe/Moscow и др.)
# ca-certificates — корректная проверка TLS при запросах к VK API
RUN apt-get update \
 && apt-get install -y --no-install-recommends tzdata ca-certificates \
 && rm -rf /var/lib/apt/lists/*

# Копируем установленные pip-пакеты из builder-стадии
COPY --from=builder /install /usr/local

# Непривилегированный пользователь
RUN useradd --create-home --shell /bin/bash bot \
 && mkdir -p /app/data \
 && chown -R bot:bot /app

WORKDIR /app

# Код приложения
COPY --chown=bot:bot bot.py config.py /app/

USER bot

# Data-каталог будет смонтирован volume-ом:
#   schedule.json, cancellations.json, sent_polls.json, chat.json
#VOLUME ["/app/data"]

# Переопределяем пути к файлам данных на /app/data, чтобы не писать в слой образа
#ENV DATA_DIR=/app/data

CMD ["python", "bot.py"]