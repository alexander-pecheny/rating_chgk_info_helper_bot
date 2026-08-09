"""Shared handler plumbing: replies, the ban gate, and the ask-one-more-thing flow."""

import logging
from collections.abc import Awaitable, Callable
from typing import Any

from aiogram import BaseMiddleware, Router
from aiogram.enums import ParseMode
from aiogram.filters import Command, StateFilter
from aiogram.fsm.context import FSMContext
from aiogram.fsm.state import State
from aiogram.types import Message

from rating_bot.db import Database
from rating_bot.helpers import START, get_batches

logger = logging.getLogger("rating_bot")

router = Router(name="common")

# A step reports what to say and whether the conversation is over.
Step = Callable[..., Awaitable[tuple[str, bool]]]


async def reply_html(message: Message, text: str) -> None:
    for batch in get_batches(text):
        await message.answer(batch)


async def reply_text(message: Message, text: str) -> None:
    for batch in get_batches(text):
        await message.answer(batch, parse_mode=None)


def command_name(message: Message) -> str | None:
    text = message.text or ""
    if not text.startswith("/"):
        return None
    return text.split(maxsplit=1)[0][1:].split("@")[0]


class ResetStateOnCommand(BaseMiddleware):
    """A real command always means what it says, even mid-conversation.

    Only commands the bot actually has: an announcement that happens to start
    with a slash is conversation input, not an escape.
    """

    def __init__(self, commands: frozenset[str]):
        self.commands = commands

    async def __call__(self, handler, event: Message, data: dict[str, Any]):
        if command_name(event) in self.commands:
            await data["state"].clear()
            # StateFilter reads the raw_state resolved once per update, so
            # clearing the storage alone would not stop a state handler.
            data["raw_state"] = None
        return await handler(event, data)


class BanGate(BaseMiddleware):
    async def __call__(self, handler, event: Message, data: dict[str, Any]):
        db: Database = data["db"]
        if event.chat.id in db.banned_users():
            await event.answer("Вы забанены.", parse_mode=None)
            return None
        return await handler(event, data)


def command_argument(message: Message, command: str) -> str:
    return (message.text or "")[len(command) + 1 :]


def register_flow(
    router: Router, *, command: str, waiting: State, step: Step
) -> None:
    """A command that either completes from its arguments or asks for one more message.

    ``step`` returns the reply and whether that finished the conversation.
    """

    async def advance(message: Message, text: str, state: FSMContext, data: dict):
        reply, finished = await step(message, text, **data)
        # Settle the state before the round-trip; updates run concurrently.
        if finished:
            await state.clear()
        else:
            await state.set_state(waiting)
        if reply:
            await reply_html(message, reply)

    @router.message(Command(command))
    async def entry(message: Message, state: FSMContext, **data):
        await advance(message, command_argument(message, command), state, data)

    @router.message(StateFilter(waiting))
    async def continued(message: Message, state: FSMContext, **data):
        await advance(message, message.text or "", state, data)


@router.message(Command("start"))
async def start(message: Message) -> None:
    await message.answer(START, parse_mode=ParseMode.HTML)


@router.message(Command("cancel"))
async def cancel(message: Message, state: FSMContext) -> None:
    await message.answer("Команда отменена.", parse_mode=None)
    logger.info(f"chat {message.chat.id} canceled the conversation.")
    await state.clear()
