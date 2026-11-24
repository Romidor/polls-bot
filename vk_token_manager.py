import json
import os
import secrets
import threading
import time
import requests


class VKTokenManager:

    def __init__(self, tokens_file="vk_tokens.json"):
        self.tokens_file = tokens_file
        self.lock = threading.Lock()
        self.data = self._load_tokens()

    def _load_tokens(self):
        if not os.path.exists(self.tokens_file):
            raise FileNotFoundError(
                f"Файл {self.tokens_file} не найден! Сначала запустите"
                " auth_vk.py."
            )
        with open(self.tokens_file, "r", encoding="utf-8") as f:
            data = json.load(f)

        # Если скрипт первичной авторизации не записал время истечения,
        # ставим 0, чтобы токен сразу обновился при первом обращении
        if "expires_at" not in data:
            data["expires_at"] = 0
        return data

    def _save_tokens(self):
        with open(self.tokens_file, "w", encoding="utf-8") as f:
            json.dump(self.data, f, ensure_ascii=False, indent=4)

    def refresh(self):
        """Принудительно обновляет access_token и refresh_token."""
        url = "https://vk.ru"
        payload = {
            "grant_type": "refresh_token",
            "refresh_token": self.data["refresh_token"],
            "client_id": self.data["client_id"],
            "device_id": self.data["device_id"],
            "state": secrets.token_urlsafe(16),
        }

        response = requests.post(url, data=payload, timeout=15)
        result = response.json()

        if "access_token" not in result:
            raise RuntimeError(f"Ошибка обновления токена VK ID: {result}")

        # Сохраняем новую пару токенов и рассчитываем время истечения (обычно 3600 сек)
        expires_in = int(result.get("expires_in", 3600))
        self.data["access_token"] = result["access_token"]
        self.data["refresh_token"] = result["refresh_token"]
        self.data["expires_at"] = time.time() + expires_in

        self._save_tokens()
        print("[VKTokenManager] Токен успешно обновлён и сохранён.")

    def get_token(self) -> str:
        """Возвращает актуальный access_token, обновляя его при необходимости."""
        with self.lock:
            # Обновляем заранее — за 5 минут (300 секунд) до истечения срока
            if time.time() >= (self.data.get("expires_at", 0) - 300):
                self.refresh()
            return self.data["access_token"]
