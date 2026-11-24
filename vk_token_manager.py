import json
import os
import secrets
import threading
import time
import requests


VK_AUTH_URL = os.environ.get("VK_AUTH_URL", "https://id.vk.ru/oauth2/auth")

class VKTokenManager:

    def __init__(self, tokens_file="/app/data/vk_tokens.json"):
        self.tokens_file = tokens_file
        self.lock = threading.Lock()
        self.data = self._load_or_init_tokens()

    def _save_tokens(self):
        os.makedirs(os.path.dirname(self.tokens_file), exist_ok=True)
        tmp_file = f"{self.tokens_file}.tmp"
        with open(tmp_file, "w", encoding="utf-8") as f:
            json.dump(self.data, f, ensure_ascii=False, indent=4)
        os.replace(tmp_file, self.tokens_file)

    def _load_or_init_tokens(self):
        # 1. Если файл с токенами уже создан и не пуст — работаем с ним
        if (
            os.path.exists(self.tokens_file)
            and os.path.getsize(self.tokens_file) > 0
        ):
            with open(self.tokens_file, "r", encoding="utf-8") as f:
                data = json.load(f)
            if "expires_at" not in data:
                data["expires_at"] = 0
            return data

        # 2. Если файла ещё нет — пробуем обменять одноразовый code из ENV прямо с IP сервера
        auth_code = os.getenv("VK_AUTH_CODE", "").strip()
        verifier = os.getenv("VK_CODE_VERIFIER", "").strip()
        device_id = os.getenv("VK_DEVICE_ID", "").strip()
        client_id = os.getenv("VK_CLIENT_ID", "").strip()

        if not all([auth_code, verifier, device_id, client_id]):
            raise RuntimeError(
                f"Файл {self.tokens_file} не найден, и не заданы ENV для"
                " первичного обмена (VK_AUTH_CODE, VK_CODE_VERIFIER,"
                " VK_DEVICE_ID, VK_CLIENT_ID)."
            )

        print(
            "[VKTokenManager] Обмениваю одноразовый VK_AUTH_CODE на токены с IP"
            " сервера..."
        )
        resp = requests.post(
            VK_AUTH_URL,
            data={
                "grant_type": "authorization_code",
                "code": auth_code,
                "code_verifier": verifier,
                "client_id": client_id,
                "device_id": device_id,
                "redirect_uri": "https://vk.ru",
                "state": secrets.token_urlsafe(32),
            },
            headers={"Content-Type": "application/x-www-form-urlencoded"},
            timeout=15,
        )
        tokens = resp.json()
        if "access_token" not in tokens:
            raise RuntimeError(f"Ошибка обмена VK_AUTH_CODE: {tokens}")

        tokens["device_id"] = device_id
        tokens["client_id"] = client_id
        tokens["expires_at"] = time.time() + int(tokens.get("expires_in", 3600))

        self.data = tokens
        self._save_tokens()
        print("[VKTokenManager] Токены успешно привязаны к IP сервера и сохранены!")
        return self.data

    def refresh(self):
        url = VK_AUTH_URL
        payload = {
            "grant_type": "refresh_token",
            "refresh_token": self.data["refresh_token"],
            "client_id": self.data["client_id"],
            "device_id": self.data["device_id"],
            "state": secrets.token_urlsafe(32),
        }
        response = requests.post(
            url,
            data=payload,
            headers={"Content-Type": "application/x-www-form-urlencoded"},
            timeout=15,
        )
        result = response.json()
        if "access_token" not in result:
            raise RuntimeError(f"Ошибка обновления токена VK ID: {result}")

        expires_in = int(result.get("expires_in", 3600))
        self.data["access_token"] = result["access_token"]
        self.data["refresh_token"] = result["refresh_token"]
        self.data["expires_at"] = time.time() + expires_in
        self._save_tokens()
        print("[VKTokenManager] Токен успешно обновлён.")

    def get_token(self) -> str:
        with self.lock:
            if time.time() >= (self.data.get("expires_at", 0) - 300):
                self.refresh()
            return self.data["access_token"]
