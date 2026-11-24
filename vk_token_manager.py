import json
import os
import secrets
import threading
import time
import requests


VK_AUTH_URL = os.environ.get("VK_AUTH_URL", "https://id.vk.ru/oauth2/auth")

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
        url = VK_AUTH_URL
        payload = {
            "grant_type": "refresh_token",
            "refresh_token": self.data["refresh_token"],
            "client_id": self.data["client_id"],
            "device_id": self.data["device_id"],
            "state": secrets.token_urlsafe(16),
        }
        headers = {
            "Content-Type": "application/x-www-form-urlencoded",
            "User-Agent": (
                "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
                " (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
            ),
        }

        response = requests.post(
            url, data=payload, headers=headers, timeout=15, allow_redirects=False
        )

        # Если ВК пытается сделать редирект (301/302) — сразу увидим куда
        if response.is_redirect or response.status_code >= 400:
            raise RuntimeError(
                f"HTTP {response.status_code} от VK ID. Location:"
                f" {response.headers.get('Location')}. Тело:"
                f" {response.text[:300]}"
            )

        try:
            result = response.json()
        except ValueError:
            raise RuntimeError(
                f"VK ID вернул не JSON (HTTP {response.status_code}):"
                f" {response.text[:300]}"
            )

        if "access_token" not in result:
            raise RuntimeError(f"Ошибка обновления токена VK ID: {result}")

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
