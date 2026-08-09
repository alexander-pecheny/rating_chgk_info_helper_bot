"""Subscribing to tournaments, and looking up their results."""

from aiogram import Router
from aiogram.filters import Command
from aiogram.types import Message

from rating_bot.db import Database
from rating_bot.handlers.common import register_flow, reply_html, reply_text
from rating_bot.helpers import (
    DEFAULT_HOST,
    ID_TOURN_TEXT,
    ID_TOURNS_TEXT,
    get_list_of_ints,
    tourn_wrap_link,
)
from rating_bot.ratingutil import get_tourn_top3
from rating_bot.states import Flows
from rating_bot.subscriptions import subscribe, unsubscribe

router = Router(name="tournaments")


async def subscribe_step(message: Message, text: str, *, db: Database, **_):
    ids = get_list_of_ints(text)
    if not ids:
        return ID_TOURNS_TEXT, False
    return "\n".join(subscribe(db, id_, message.chat.id) for id_ in ids), True


async def subscribe_izh_step(message: Message, text: str, *, db: Database, **_):
    ids = get_list_of_ints(text)
    if not ids:
        return ID_TOURNS_TEXT, False
    replies = [subscribe(db, id_, message.chat.id, jury_only=True) for id_ in ids]
    return "\n".join(replies), True


async def unsubscribe_step(message: Message, text: str, *, db: Database, **_):
    ids = get_list_of_ints(text)
    if not ids:
        return ID_TOURNS_TEXT, False
    return "\n".join(unsubscribe(db, id_, message.chat.id) for id_ in ids), True


async def top3_step(message: Message, text: str, **_):
    ids = get_list_of_ints(text)
    if len(ids) != 1:
        return ID_TOURN_TEXT, False
    return get_tourn_top3(ids[0]), True


register_flow(router, command="subscribe", waiting=Flows.subscribe, step=subscribe_step)
register_flow(
    router,
    command="subscribe_izh",
    waiting=Flows.subscribe_izh,
    step=subscribe_izh_step,
)
register_flow(
    router, command="unsubscribe", waiting=Flows.unsubscribe, step=unsubscribe_step
)
register_flow(router, command="get_top3", waiting=Flows.top3, step=top3_step)


@router.message(Command("my_subscriptions"))
async def my_subscriptions(message: Message, db: Database) -> None:
    chat_id = message.chat.id
    names = [
        f"{tournament.id} {tournament.name}"
        for tournament in db.tournaments()
        if chat_id in tournament.subscribers
    ]
    if not names:
        await reply_text(message, "Сейчас вы не подписаны ни на один турнир.")
        return
    host = db.prefs(chat_id).get("host") or DEFAULT_HOST
    listed = "\n".join(tourn_wrap_link(name).format(host=host) for name in names)
    await reply_html(message, "Турниры, на которые вы подписаны:\n" + listed)
