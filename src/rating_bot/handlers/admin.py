"""Admin-only commands."""

import logging
from typing import Any

from aiogram import BaseMiddleware, Bot, Router
from aiogram.enums import ParseMode
from aiogram.filters import Command
from aiogram.types import Message
from apscheduler.schedulers.asyncio import AsyncIOScheduler

from rating_bot.config import Config
from rating_bot.dateutil import tryint
from rating_bot.db import Database
from rating_bot.handlers.common import command_argument, reply_text
from rating_bot.jobs import (
    check_applications,
    check_applications_debug,
    make_reminders,
    test_job,
)

logger = logging.getLogger("rating_bot")

router = Router(name="admin")


class AdminGate(BaseMiddleware):
    async def __call__(self, handler, event: Message, data: dict[str, Any]):
        if event.chat.id not in data["config"].admins:
            await event.answer("Вы не админ бота.", parse_mode=None)
            return None
        return await handler(event, data)


router.message.middleware(AdminGate())


def _run_soon(scheduler: AsyncIOScheduler, func, bot, db, config) -> None:
    scheduler.add_job(func, args=[bot, db, config])


@router.message(Command("debug_info"))
async def debug_info(message: Message, scheduler: AsyncIOScheduler) -> None:
    times = sorted(job.next_run_time for job in scheduler.get_jobs() if job.next_run_time)
    if not times:
        await reply_text(message, "no jobs scheduled")
        return
    await reply_text(message, f"next regular job will be run at {times[0]}")


@router.message(Command("ban"))
async def ban(message: Message, db: Database) -> None:
    chat_id = tryint(command_argument(message, "ban").strip())
    if not chat_id:
        await reply_text(message, "ID чата не распознан")
        return
    if db.ban(chat_id):
        await reply_text(message, f"Пользователь {chat_id} забанен")
    else:
        await reply_text(message, f"Пользователь {chat_id} уже забанен")


@router.message(Command("unban"))
async def unban(message: Message, db: Database) -> None:
    chat_id = tryint(command_argument(message, "unban").strip())
    if not chat_id:
        await reply_text(message, "ID чата не распознан")
        return
    if db.unban(chat_id):
        await reply_text(message, f"Пользователь {chat_id} разбанен")
    else:
        await reply_text(message, f"Пользователь {chat_id} и так не забанен")


@router.message(Command("get_subscribers"))
async def get_subscribers(message: Message, db: Database) -> None:
    tournaments = db.tournaments()
    per_chat = {}
    for tournament in tournaments:
        for chat_id in tournament.subscribers:
            per_chat.setdefault(chat_id, []).append(
                f"{tournament.id} {tournament.name}"
            )
    lines = [
        f"{i + 1}. {chat_id} - {len(per_chat[chat_id])} tournaments"
        f" ({', '.join(per_chat[chat_id])})"
        for i, chat_id in enumerate(sorted(per_chat))
    ]
    lines.append(f"{len(lines)} chats subscribed to {len(tournaments)} unique tournaments")
    await reply_text(message, "\n".join(lines))


@router.message(Command("run_check_requests"))
async def run_check_applications(
    message: Message, scheduler: AsyncIOScheduler, bot: Bot, db: Database, config: Config
) -> None:
    _run_soon(scheduler, check_applications, bot, db, config)
    await reply_text(message, "regular job scheduled")


@router.message(Command("run_make_reminders"))
async def run_make_reminders(
    message: Message, scheduler: AsyncIOScheduler, bot: Bot, db: Database, config: Config
) -> None:
    _run_soon(scheduler, make_reminders, bot, db, config)
    await reply_text(message, "regular job scheduled")


@router.message(Command("run_check_requests_debug"))
async def run_check_applications_debug(
    message: Message, scheduler: AsyncIOScheduler, bot: Bot, db: Database, config: Config
) -> None:
    _run_soon(scheduler, check_applications_debug, bot, db, config)
    await reply_text(message, "regular job scheduled in debug mode")


@router.message(Command("run_test_job"))
async def run_test_job(
    message: Message, scheduler: AsyncIOScheduler, bot: Bot, db: Database, config: Config
) -> None:
    _run_soon(scheduler, test_job, bot, db, config)
    await reply_text(message, "test job scheduled")


@router.message(Command("echo_md"))
async def echo_md(message: Message) -> None:
    await message.answer(
        command_argument(message, "echo_md"), parse_mode=ParseMode.MARKDOWN
    )


@router.message(Command("echo_html"))
async def echo_html(message: Message) -> None:
    await message.answer(
        command_argument(message, "echo_html"), parse_mode=ParseMode.HTML
    )
