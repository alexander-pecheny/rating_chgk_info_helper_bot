"""Per-chat preferences: which rating host to link to, and synchronous-tournament dates."""

from aiogram import Router
from aiogram.filters import Command
from aiogram.fsm.context import FSMContext
from aiogram.types import Message

from rating_bot.dateutil import DatesPrefs, generate_dates, parse_dt_prefs, tryint
from rating_bot.db import Database
from rating_bot.handlers.common import register_flow, reply_html
from rating_bot.helpers import strip_host, validate_host
from rating_bot.states import Flows

router = Router(name="prefs")

DATE_PROMPT = (
    "Введите дату либо дату и время (UTC+3) начала синхрона в формате:"
    " <pre>2023-01-31</pre> либо <pre>2023-01-31 11:00</pre>"
)
HOST_PROMPT = (
    "Введите новый хост (например, <pre>rating.chgk.info</pre>"
    " или <pre>rating.pecheny.me</pre>)"
)
SYNC_DAYS_PROMPT = "Сколько дней от 1 до 7 будет длиться синхрон?"
ASYNC_DAYS_PROMPT = "Сколько дней будет длиться асинхрон? Если не хотите асинхрон, напишите 0"
DAYS_ERROR = "Неверный формат. Введите количество дней от 1 до 7"


def stored_dates_prefs(db: Database, chat_id: int) -> DatesPrefs:
    prefs = DatesPrefs()
    prefs.update_from_dict(db.prefs(chat_id).get("dates") or {})
    return prefs


async def host_step(message: Message, text: str, *, db: Database, **_):
    host = strip_host(text)
    if not host:
        return HOST_PROMPT, False
    if not validate_host(host):
        return "Не удалось распарсить хост, попробуйте ещё раз", False
    prefs = db.prefs(message.chat.id)
    prefs["host"] = host
    db.set_prefs(message.chat.id, prefs)
    return f"Ваш хост теперь <pre>{host}</pre>.", True


async def dates_prefs_step(message: Message, text: str, *, db: Database, **_):
    chat_id = message.chat.id
    current = stored_dates_prefs(db, chat_id)
    if not text.strip():
        return f"Ваши настройки дат: {current.hr()}.\n\nВведите новые настройки:", False
    current.update_from_str(text)
    prefs = db.prefs(chat_id)
    prefs["dates"] = current.json()
    db.set_prefs(chat_id, prefs)
    return f"Ваши настройки дат теперь: {current.hr()}.", True


register_flow(router, command="set_host", waiting=Flows.host, step=host_step)
register_flow(
    router,
    command="set_dates_prefs",
    waiting=Flows.dates_prefs,
    step=dates_prefs_step,
)


@router.message(Command("get_dates_prefs"))
async def get_dates_prefs(message: Message, db: Database) -> None:
    prefs = stored_dates_prefs(db, message.chat.id)
    await reply_html(message, f"Ваши настройки дат: {prefs.hr()}.")


@router.message(Command("get_dates"))
async def get_dates(message: Message, state: FSMContext) -> None:
    await reply_html(message, DATE_PROMPT)
    await state.set_state(Flows.dates_start)


@router.message(Flows.dates_start)
async def dates_start(message: Message, state: FSMContext) -> None:
    text = (message.text or "").strip()
    try:
        parse_dt_prefs(text)
    except Exception:
        await reply_html(message, f"Неверный формат. {DATE_PROMPT}")
        return
    await state.update_data(sync_start=text)
    await reply_html(message, SYNC_DAYS_PROMPT)
    await state.set_state(Flows.dates_sync_days)


@router.message(Flows.dates_sync_days)
async def dates_sync_days(message: Message, state: FSMContext) -> None:
    days = tryint((message.text or "").strip())
    if not days:
        await reply_html(message, DAYS_ERROR)
        return
    await state.update_data(sync_days=days)
    await reply_html(message, ASYNC_DAYS_PROMPT)
    await state.set_state(Flows.dates_async_days)


@router.message(Flows.dates_async_days)
async def dates_async_days(message: Message, state: FSMContext, db: Database) -> None:
    async_days = tryint((message.text or "").strip()) or 0
    data = await state.get_data()
    dates = generate_dates(
        data["sync_start"],
        data["sync_days"],
        async_days,
        stored_dates_prefs(db, message.chat.id),
    )
    await reply_html(message, dates)
    await state.clear()
