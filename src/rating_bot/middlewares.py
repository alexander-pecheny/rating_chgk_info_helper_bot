"""Recording what came in and what went out."""

import logging
from typing import Any

from aiogram import BaseMiddleware, Bot
from aiogram.client.session.middlewares.base import BaseRequestMiddleware
from aiogram.methods import TelegramMethod
from aiogram.types import Update

from rating_bot.logstore import LogStore, dumps
from rating_bot.rich_text import message_text

logger = logging.getLogger("rating_bot")

# The long poll fires every second and says nothing about the conversation.
SKIPPED_METHODS = frozenset({"getUpdates"})


def _incoming_summary(update: Update) -> tuple[int | None, str | None]:
    message = (
        update.message
        or update.edited_message
        or update.channel_post
        or update.edited_channel_post
    )
    if message:
        return message.chat.id, message_text(message)
    if update.callback_query:
        chat = getattr(update.callback_query.message, "chat", None)
        return (chat.id if chat else None), update.callback_query.data
    return None, None


class IncomingTrafficLog(BaseMiddleware):
    def __init__(self, store: LogStore):
        self.store = store

    async def __call__(self, handler, event: Update, data: dict[str, Any]):
        try:
            chat_id, text = _incoming_summary(event)
            self.store.record_traffic("in", dumps(event), chat_id=chat_id, text=text)
        except Exception as e:
            logger.error(f"exception {type(e)} {e} while logging incoming update")
        return await handler(event, data)


class OutgoingTrafficLog(BaseRequestMiddleware):
    def __init__(self, store: LogStore):
        self.store = store

    async def __call__(self, make_request, bot: Bot, method: TelegramMethod):
        name = method.__api_method__
        if name not in SKIPPED_METHODS:
            try:
                self.store.record_traffic(
                    "out",
                    dumps(method),
                    method=name,
                    chat_id=getattr(method, "chat_id", None),
                    text=getattr(method, "text", None)
                    or getattr(method, "caption", None),
                )
            except Exception as e:
                logger.error(f"exception {type(e)} {e} while logging {name}")
        return await make_request(bot, method)
