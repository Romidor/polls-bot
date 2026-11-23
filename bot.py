#!/usr/bin/env python3
# bot.py
import json
import logging
import os
from datetime import datetime, timedelta
from zoneinfo import ZoneInfo

import vk_api
from vk_api.bot_longpoll import VkBotLongPoll, VkBotEventType
from apscheduler.schedulers.background import BackgroundScheduler
from apscheduler.triggers.cron import CronTrigger

import config

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
)
logger = logging.getLogger(__name__)

# bot.py (фрагмент в начале, после import config)
BASE_DIR = os.path.dirname(os.path.abspath(__file__))
DATA_DIR = os.environ.get("DATA_DIR", BASE_DIR)

SCHEDULE_FILE = os.path.join(DATA_DIR, "schedule.json")
CANCEL_FILE   = os.path.join(DATA_DIR, "cancellations.json")
SENT_FILE     = os.path.join(DATA_DIR, "sent_polls.json")
CHAT_FILE     = os.path.join(DATA_DIR, "chat.json")

vk_session = vk_api.VkApi(token=config.TOKEN)
vk = vk_session.get_api()
longpoll = VkBotLongPoll(vk_session, config.GROUP_ID)
scheduler = BackgroundScheduler(timezone=ZoneInfo(config.TIMEZONE))

# Сессия пользователя — ТОЛЬКО для polls.create
user_vk_session = vk_api.VkApi(token=config.USER_TOKEN)
user_vk = user_vk_session.get_api()

# ---------- дни недели ----------
RU_DAYS = [
    "понедельник", "вторник", "среда", "четверг",
    "пятница", "суббота", "воскресенье",
]
EN_DAYS = [
    "monday", "tuesday", "wednesday", "thursday",
    "friday", "saturday", "sunday",
]
RU_TO_EN = dict(zip(RU_DAYS, EN_DAYS))
EN_TO_RU = dict(zip(EN_DAYS, RU_DAYS))

def normalize_day(day: str):
    """Возвращает русское название дня недели или None, если день не распознан.
    Принимает русские и английские формы (регистр не важен)."""
    d = (day or "").strip().lower()
    if d in RU_TO_EN:
        return d
    if d in EN_TO_RU:
        return EN_TO_RU[d]
    return None

# ---------- JSON-хелперы ----------
def load_json(path, default):
    if not os.path.exists(path):
        return default
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)

def save_json(path, data):
    parent = os.path.dirname(path)
    if parent:
        os.makedirs(parent, exist_ok=True)
    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)

# ---------- целевой чат ----------
def load_target_peer_id():
    data = load_json(CHAT_FILE, {})
    return data.get("peer_id")

def save_target_peer_id(peer_id: int):
    save_json(CHAT_FILE, {"peer_id": peer_id})

# ---------- расписание ----------
def load_schedule():
    return load_json(SCHEDULE_FILE, [])

def save_schedule(schedule):
    save_json(SCHEDULE_FILE, schedule)

def load_cancellations():
    return set(load_json(CANCEL_FILE, []))

def save_cancellations(cancellations):
    save_json(CANCEL_FILE, list(cancellations))

def load_sent_polls():
    return load_json(SENT_FILE, {})

def save_sent_polls(sent):
    save_json(SENT_FILE, sent)

# ---------- создание опроса ----------
def create_poll(lesson_type, lesson_time, date_str):
    peer_id = load_target_peer_id()
    if peer_id is None:
        logger.warning("Не задан целевой чат для опросов. Отправьте /register в нужном чате.")
        return False

    question = f"{lesson_type} {date_str} {lesson_time} — идёте?"
    answers = [
        {"text": "Иду"},
        {"text": "Не иду"},
    ]
    try:
        response = user_vk.polls.create(
            owner_id=-config.GROUP_ID,
            question=question,
            add_answers=json.dumps(answers, ensure_ascii=False),
            is_anonymous=0,
            is_multiple=0,
        )
        poll_id = response["id"]
        # Вложение: poll-{GROUP_ID}_{poll_id}
        attachment = f"poll-{config.GROUP_ID}_{poll_id}"
        vk.messages.send(          # отправляем уже от имени сообщества
            peer_id=peer_id,
            message=f"📊 Опрос: {question}",
            attachment=attachment,
            random_id=0,
        )
        logger.info(f"Опрос создан для peer_id={peer_id}: {question}")
        return True
    except Exception as e:
        logger.exception(f"Не удалось создать опрос: {e}")
        try:
            vk.messages.send(
                peer_id=peer_id,
                message=(
                    f"⚠️ Не удалось создать опрос. Вопрос: {question}\n"
                    f"Варианты: Иду / Не иду"
                ),
                random_id=0,
            )
        except Exception as send_err:
            logger.exception(f"Не удалось отправить резервное сообщение: {send_err}")
        return False

# ---------- планировщик ----------
def schedule_lesson(lesson):
    day_ru = normalize_day(lesson["day"])
    if day_ru is None:
        logger.warning(f"Неизвестный день недели: {lesson['day']}")
        return
    day_en_short = {
        "понедельник": "mon", "вторник": "tue", "среда": "wed",
        "четверг": "thu", "пятница": "fri", "суббота": "sat", "воскресенье": "sun",
    }[day_ru]

    lesson_hour, lesson_minute = map(int, lesson["time"].split(":"))
    poll_dt = datetime.combine(
        datetime.today(),
        datetime.min.time().replace(hour=lesson_hour, minute=lesson_minute),
    ) - timedelta(minutes=config.POLL_CREATION_OFFSET_MINUTES)
    poll_hour, poll_minute = poll_dt.hour, poll_dt.minute

    job_id = f"poll_{day_ru}_{lesson['time']}_{lesson['type']}"

    def job():
        logger.info(f"▶ Сработала задача {job_id}")
        today = datetime.now(ZoneInfo(config.TIMEZONE)).date().isoformat()
        if today in load_cancellations():
            logger.info(f"Занятие {lesson['type']} на {today} отменено.")
            return
        sent = load_sent_polls()
        if today in sent and lesson["type"] in sent[today]:
            logger.info(f"Опрос для {lesson['type']} на {today} уже создан.")
            return
        success = create_poll(lesson["type"], lesson["time"], today)
        if success:
            sent.setdefault(today, []).append(lesson["type"])
            save_sent_polls(sent)

    scheduler.add_job(
        job,
        trigger=CronTrigger(
            day_of_week=day_en_short,
            hour=poll_hour,
            minute=poll_minute,
            timezone=ZoneInfo(config.TIMEZONE),
        ),
        id=job_id,
        replace_existing=True,
        max_instances=1,
        coalesce=True,
        misfire_grace_time=300,
    )
    logger.info(f"Запланировано: {job_id} (опрос в {poll_hour:02d}:{poll_minute:02d})")

def reschedule_all():
    for job in scheduler.get_jobs():
        job.remove()
    schedule = load_schedule()
    for lesson in schedule:
        schedule_lesson(lesson)
    logger.info(f"Перепланировано {len(schedule)} занятий.")

# ---------- восстановление пропущенных опросов ----------
def recover_missed_polls():
    if load_target_peer_id() is None:
        logger.warning("Нет зарегистрированного чата. Пропускаем восстановление опросов.")
        return

    now = datetime.now(ZoneInfo(config.TIMEZONE))
    today = now.date().isoformat()
    schedule = load_schedule()
    cancellations = load_cancellations()
    sent = load_sent_polls()

    if today in cancellations:
        logger.info(f"Сегодня {today} есть отмены, пропускаем восстановление.")
        return

    # сегодняшний день недели на русском
    today_ru = RU_DAYS[now.weekday()]

    for lesson in schedule:
        day_ru = normalize_day(lesson["day"])
        if day_ru is None or day_ru != today_ru:
            continue
        if today in sent and lesson["type"] in sent[today]:
            continue
        lesson_hour, lesson_minute = map(int, lesson["time"].split(":"))
        lesson_dt = now.replace(hour=lesson_hour, minute=lesson_minute, second=0, microsecond=0)
        poll_dt = lesson_dt - timedelta(minutes=config.POLL_CREATION_OFFSET_MINUTES)
        if now >= poll_dt:
            logger.info(f"Восстанавливаем пропущенный опрос для {lesson['type']} на {today}")
            success = create_poll(lesson["type"], lesson["time"], today)
            if success:
                sent.setdefault(today, []).append(lesson["type"])
                save_sent_polls(sent)

# ---------- обработка команд ----------
def handle_command(event):
    text = event.message.text.strip()
    if not text.startswith("/"):
        return

    parts = text.split(maxsplit=3)  # тип занятия может содержать пробел
    cmd = parts[0][1:].lower()

    peer_id = event.message.peer_id
    is_group_chat = peer_id >= 2_000_000_000

    # ---- /register ----
    if cmd == "register":
        if not is_group_chat:
            vk.messages.send(
                peer_id=peer_id,
                message="⚠️ Регистрировать можно только беседу (групповой чат).",
                random_id=0,
            )
            return
        save_target_peer_id(peer_id)
        logger.info(f"Целевой чат зарегистрирован: peer_id={peer_id}")
        vk.messages.send(
            peer_id=peer_id,
            message=f"✅ Этот чат зарегистрирован как целевой для опросов (peer_id={peer_id}).",
            random_id=0,
        )
        return

    # ---- /list ----
    if cmd == "list":
        schedule = load_schedule()
        if not schedule:
            vk.messages.send(peer_id=peer_id, message="📭 Расписание пусто.", random_id=0)
            return
        lines = ["📅 Расписание занятий на неделю:"]
        for lesson in sorted(
            schedule,
            key=lambda x: (RU_DAYS.index(normalize_day(x["day"]) or "понедельник"), x["time"]),
        ):
            lines.append(f"• {lesson['day'].capitalize()} {lesson['time']} — {lesson['type']}")
        target = load_target_peer_id()
        lines.append("")
        lines.append(
            f"🎯 Целевой чат: {'не задан (/register)' if target is None else target}"
        )
        vk.messages.send(peer_id=peer_id, message="\n".join(lines), random_id=0)
        return

    # ---- остальные команды требуют регистрации ----
    if load_target_peer_id() is None:
        vk.messages.send(
            peer_id=peer_id,
            message="⚠️ Сначала зарегистрируйте чат для опросов: отправьте /register.",
            random_id=0,
        )
        return

    schedule = load_schedule()

    if cmd == "add":
        # /add <день_недели> <время> <тип_занятия>
        if len(parts) < 4:
            vk.messages.send(
                peer_id=peer_id,
                message=(
                    "❌ Использование: /add <день_недели> <время> <тип_занятия>\n"
                    "Пример: /add понедельник 20:00 бассейн"
                ),
                random_id=0,
            )
            return
        day_ru = normalize_day(parts[1])
        if day_ru is None:
            vk.messages.send(
                peer_id=peer_id,
                message=(
                    "❌ Неверный день недели. Допустимые значения: "
                    + ", ".join(RU_DAYS)
                ),
                random_id=0,
            )
            return
        time_str = parts[2]
        try:
            datetime.strptime(time_str, "%H:%M")
        except ValueError:
            vk.messages.send(
                peer_id=peer_id,
                message="❌ Неверный формат времени. Используйте ЧЧ:ММ.",
                random_id=0,
            )
            return
        lesson_type = parts[3].strip()
        if not lesson_type:
            vk.messages.send(
                peer_id=peer_id,
                message="❌ Не указан тип занятия.",
                random_id=0,
            )
            return
        new_lesson = {"day": day_ru, "time": time_str, "type": lesson_type}
        schedule.append(new_lesson)
        save_schedule(schedule)
        schedule_lesson(new_lesson)
        vk.messages.send(
            peer_id=peer_id,
            message=f"✅ Добавлено: {day_ru} {time_str} — {lesson_type}",
            random_id=0,
        )

    elif cmd == "cancel":
        if len(parts) < 2:
            vk.messages.send(
                peer_id=peer_id,
                message="❌ Использование: /cancel <дата>\nПример: /cancel 2025-05-01",
                random_id=0,
            )
            return
        date_str = parts[1]
        try:
            datetime.strptime(date_str, "%Y-%m-%d")
        except ValueError:
            vk.messages.send(
                peer_id=peer_id,
                message="❌ Неверный формат даты. Используйте ГГГГ-ММ-ДД.",
                random_id=0,
            )
            return
        cancellations = load_cancellations()
        cancellations.add(date_str)
        save_cancellations(cancellations)
        vk.messages.send(
            peer_id=peer_id,
            message=f"✅ Занятия на {date_str} отменены.",
            random_id=0,
        )

    elif cmd == "remove":
        # /remove <день_недели> <время>
        if len(parts) < 3:
            vk.messages.send(
                peer_id=peer_id,
                message="❌ Использование: /remove <день_недели> <время>",
                random_id=0,
            )
            return
        day_ru = normalize_day(parts[1])
        if day_ru is None:
            vk.messages.send(
                peer_id=peer_id,
                message="❌ Неверный день недели.",
                random_id=0,
            )
            return
        time_str = parts[2]
        new_schedule = [
            l for l in schedule
            if not (normalize_day(l["day"]) == day_ru and l["time"] == time_str)
        ]
        if len(new_schedule) == len(schedule):
            vk.messages.send(
                peer_id=peer_id,
                message="❌ Занятие с такими параметрами не найдено.",
                random_id=0,
            )
            return
        save_schedule(new_schedule)
        reschedule_all()
        vk.messages.send(
            peer_id=peer_id,
            message=f"✅ Удалено: {day_ru} {time_str}",
            random_id=0,
        )

    elif cmd == "move":
        # /move <день_недели> <время> <новое_время>
        if len(parts) < 4:
            vk.messages.send(
                peer_id=peer_id,
                message="❌ Использование: /move <день_недели> <время> <новое_время>",
                random_id=0,
            )
            return
        day_ru = normalize_day(parts[1])
        if day_ru is None:
            vk.messages.send(
                peer_id=peer_id,
                message="❌ Неверный день недели.",
                random_id=0,
            )
            return
        old_time = parts[2]
        new_time = parts[3]
        try:
            datetime.strptime(new_time, "%H:%M")
        except ValueError:
            vk.messages.send(
                peer_id=peer_id,
                message="❌ Неверный формат нового времени. Используйте ЧЧ:ММ.",
                random_id=0,
            )
            return
        found = False
        for lesson in schedule:
            if normalize_day(lesson["day"]) == day_ru and lesson["time"] == old_time:
                lesson["time"] = new_time
                found = True
                break
        if not found:
            vk.messages.send(
                peer_id=peer_id,
                message="❌ Занятие с такими параметрами не найдено.",
                random_id=0,
            )
            return
        save_schedule(schedule)
        reschedule_all()
        vk.messages.send(
            peer_id=peer_id,
            message=f"✅ Время изменено: {day_ru} {old_time} → {new_time}",
            random_id=0,
        )

    else:
        vk.messages.send(
            peer_id=peer_id,
            message=(
                "❌ Неизвестная команда.\n"
                "Доступные: /register, /add, /cancel, /remove, /move, /list"
            ),
            random_id=0,
        )

# ---------- основной цикл ----------
def main():
    recover_missed_polls()

    schedule = load_schedule()
    for lesson in schedule:
        schedule_lesson(lesson)

    scheduler.start()
    logger.info("Планировщик запущен.")

    logger.info("Бот запущен. Ожидание сообщений…")
    for event in longpoll.listen():
        if event.type == VkBotEventType.MESSAGE_NEW:
            logger.info(f"Получено сообщение: peer_id={event.message.peer_id}, "f"text={event.message.text!r}")
            handle_command(event)

if __name__ == "__main__":
    main()