# config.py
import os

def _require_env(name: str) -> str:
    """Возвращает значение переменной окружения или падает с понятной ошибкой."""
    value = os.environ.get(name)
    if not value:
        raise RuntimeError(
            f"Не задана переменная окружения {name}. "
            f"Установите её перед запуском бота "
            f"(например: export {name}=...)."
        )
    return value

# Токен сообщества — только из переменной окружения
TOKEN = _require_env("VK_ACCESS_TOKEN")

# Остальные настройки можно оставить в файле
GROUP_ID = int(os.environ.get("VK_GROUP_ID", "123456789"))  # можно переопределить через env
TIMEZONE = os.environ.get("TZ", "Europe/Moscow")
POLL_CREATION_OFFSET_MINUTES = int(os.environ.get("VK_POLL_OFFSET_MINUTES", "60"))