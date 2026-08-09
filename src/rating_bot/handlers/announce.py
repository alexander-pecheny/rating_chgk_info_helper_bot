"""Relaying an organiser's announcement into the announce channel.

Whatever Telegram invents next, an announcement is still just a message we hand
back to Telegram by id, so this stays agnostic about how the content is encoded
-- it only needs enough text to judge the length, and the ids to send.
"""

import asyncio
import logging
from collections import deque

from aiogram import Bot, Router
from aiogram.filters import Command
from aiogram.fsm.context import FSMContext
from aiogram.types import Message

from rating_bot.config import Config
from rating_bot.handlers.common import reply_html
from rating_bot.helpers import udumps
from rating_bot.rich_text import message_text
from rating_bot.states import Flows

logger = logging.getLogger("rating_bot")

router = Router(name="announce")

MIN_LENGTH = 200
# Telegram delivers an album as separate updates; they land within a few ms of
# each other, so a short wait collects the whole set.
ALBUM_DEBOUNCE_SECONDS = 1.0

PROMPT = "Пришлите сообщение для отправки в канал с анонсами:"
TOO_SHORT = "Слишком короткое сообщение. Введите новую версию или нажмите /cancel"
UNPARSEABLE = "Не получилось распарсить сообщение :( Введите новую версию или нажмите /cancel"
SENT = "Анонс успешно отправлен!"
FAILED = "Что-то пошло не так :( Напишите разработчику бота"

_albums: dict[tuple[int, str], list[Message]] = {}
_album_timers: dict[tuple[int, str], asyncio.Task] = {}
# Stragglers of an album we already relayed must not start a second post.
_recently_sent: deque[tuple[int, str]] = deque(maxlen=64)


@router.message(Command("announce"))
async def announce_entry(message: Message, state: FSMContext) -> None:
    # Set the state before the round-trip: updates are handled concurrently, so
    # a fast follow-up message can arrive while the prompt is still in flight.
    await state.set_state(Flows.announce)
    await reply_html(message, PROMPT)


@router.message(Flows.announce)
async def announce(
    message: Message, state: FSMContext, bot: Bot, config: Config, **_
) -> None:
    if message.media_group_id:
        await _collect_album(message, state, bot, config)
        return
    await _deliver(message, [message], state, bot, config)


async def _collect_album(
    message: Message, state: FSMContext, bot: Bot, config: Config
) -> None:
    key = (message.chat.id, message.media_group_id)
    if key in _recently_sent:
        logger.info(f"ignoring album item arriving after {key} was relayed")
        return
    _albums.setdefault(key, []).append(message)
    if timer := _album_timers.get(key):
        timer.cancel()

    async def flush() -> None:
        # Cancellation means a later item of this album superseded us; the
        # buffer belongs to its timer now, so unwind without touching it.
        await asyncio.sleep(ALBUM_DEBOUNCE_SECONDS)
        try:
            group = sorted(_albums.get(key, []), key=lambda m: m.message_id)
            if not group:
                return
            # The organiser may have cancelled while the debounce was pending.
            if await state.get_state() != Flows.announce.state:
                logger.info(f"dropping album {key}, conversation already left")
                return
            _recently_sent.append(key)
            await _deliver(group[0], group, state, bot, config)
        except Exception as e:
            logger.error(f"exception {type(e)} {e} while sending album announce")
        finally:
            _albums.pop(key, None)
            _album_timers.pop(key, None)

    _album_timers[key] = asyncio.create_task(flush())


async def _deliver(
    message: Message,
    group: list[Message],
    state: FSMContext,
    bot: Bot,
    config: Config,
) -> None:
    text = "\n".join(filter(None, (message_text(item) for item in group)))
    if not text:
        await reply_html(message, UNPARSEABLE)
        return
    if len(text) < MIN_LENGTH:
        await reply_html(message, TOO_SHORT)
        return

    chat_id = message.chat.id
    message_ids = [item.message_id for item in group]
    is_forward = group[0].forward_origin is not None

    sent = await _send(bot, config, chat_id, message_ids, forward=is_forward)
    if not sent and not is_forward:
        logger.info(f"copy refused for {message_ids}, forwarding instead")
        sent = await _send(bot, config, chat_id, message_ids, forward=True)

    if sent:
        await reply_html(message, SENT)
        logger.info(f"user {chat_id} sent announce {udumps(text)}")
    else:
        await reply_html(message, FAILED)
        logger.error(f"error while trying to send announce from user {chat_id}")
    await state.clear()


async def _send(
    bot: Bot, config: Config, from_chat_id: int, message_ids: list[int], *, forward: bool
) -> bool:
    target = config.announce_channel_id
    try:
        if len(message_ids) > 1:
            send = bot.forward_messages if forward else bot.copy_messages
            await send(
                chat_id=target, from_chat_id=from_chat_id, message_ids=message_ids
            )
        else:
            send = bot.forward_message if forward else bot.copy_message
            await send(
                chat_id=target, from_chat_id=from_chat_id, message_id=message_ids[0]
            )
        return True
    except Exception as e:
        logger.error(
            f"exception {type(e)} {e} while trying to"
            f" {'forward' if forward else 'copy'} {message_ids} to {target}"
        )
        return False
