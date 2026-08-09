"""The two scheduled jobs: new applications to review, and deadline reminders."""

import asyncio
import logging
from collections import defaultdict

from aiogram import Bot
from aiogram.enums import ParseMode

from rating_bot.config import Config
from rating_bot.dateutil import DT, now
from rating_bot.db import Database
from rating_bot.helpers import (
    DEFAULT_HOST,
    format_applications,
    get_application_form,
    get_batches,
)
from rating_bot.ratingutil import (
    get_applications,
    get_info,
    info_is_bad,
    tourn_info_to_reminders,
)
from rating_bot.subscriptions import review_link

logger = logging.getLogger("rating_bot")


async def send(bot: Bot, chat_id: int, text: str) -> None:
    for batch in get_batches(text):
        try:
            await bot.send_message(chat_id, batch, parse_mode=ParseMode.HTML)
        except Exception as e:
            logger.error(f"exception {type(e)} {e} while messaging {chat_id}")


async def check_applications(
    bot: Bot, db: Database, config: Config, chat_ids_whitelist=None
) -> None:
    logger.debug("running regular job...")
    pending_writes = []
    user_to_message = defaultdict(list)
    user_to_subscriptions = defaultdict(list)
    tournament_applications = {}

    for tournament in db.tournaments():
        for chat_id, subscription in tournament.subscribers.items():
            if subscription.get("r"):
                user_to_subscriptions[chat_id].append(
                    (tournament.id, tournament.name)
                )
        try:
            info = await asyncio.to_thread(get_info, tournament.id)
            if not chat_ids_whitelist and info_is_bad(info):
                logger.debug(f"adding request for removal of tourn_id {tournament.id}")
                pending_writes.append((db.delete_tournament, (tournament.id,)))
                continue
            if DT(info["dateEnd"]) > DT(now()):
                await _collect_new_applications(
                    db,
                    tournament,
                    chat_ids_whitelist,
                    pending_writes,
                    user_to_message,
                    tournament_applications,
                )
        except Exception as e:
            logger.error(
                f"error while processing tourn_id {tournament.id}: {type(e)} {e}"
            )
            continue

    logger.debug(f"total {len(user_to_message)} chats will be messaged")
    for chat_id in user_to_message:
        if chat_ids_whitelist and chat_id not in chat_ids_whitelist:
            continue
        try:
            texts = _messages_for_chat(
                db, chat_id, user_to_message, user_to_subscriptions, tournament_applications
            )
        except Exception as e:
            logger.error(
                f"error while trying to process messages for {chat_id}: {type(e)} {e}"
            )
            continue
        final_text = "\n\n".join(texts)
        if final_text:
            logger.debug(f"sending messages to {chat_id}")
            await send(bot, chat_id, final_text)

    if not chat_ids_whitelist:
        _apply(pending_writes)


async def _collect_new_applications(
    db, tournament, chat_ids_whitelist, pending_writes, user_to_message, tournament_applications
):
    fresh = await asyncio.to_thread(get_applications, tournament.id)
    if fresh is None:
        logger.error(
            f"could not get applications for tourn_id {tournament.id}, skipping"
        )
        return
    added = set(fresh) - set(tournament.applications)
    removed = set(tournament.applications) - set(fresh)
    tournament_applications[(tournament.id, tournament.name)] = fresh
    if (added or removed) and not chat_ids_whitelist:
        logger.debug(
            f"adding request for updating data for tourn_id {tournament.id}:"
            f" was {tournament.applications}, became {fresh}"
        )
        pending_writes.append((db.set_applications, (tournament.id, fresh)))
    if not added:
        return
    logger.debug(
        f"adding requests for sending messages for tourn_id {tournament.id}"
        f" for chat_ids {','.join(str(x) for x in tournament.subscribers)}"
    )
    for chat_id, subscription in tournament.subscribers.items():
        if (
            chat_ids_whitelist and chat_id not in chat_ids_whitelist
        ) or not subscription.get("r"):
            continue
        host = db.prefs(chat_id).get("host") or DEFAULT_HOST
        text = (
            f"Для турнира <b>{tournament.id} {tournament.name}</b> есть {len(fresh)}"
            f" {get_application_form(len(fresh))}. {review_link(host, tournament.id)}\n\n"
            + format_applications(fresh, host)
        )
        user_to_message[chat_id].append((tournament.id, text))
        logger.debug(f"added message for {chat_id} about {tournament.id}")


def _messages_for_chat(
    db, chat_id, user_to_message, user_to_subscriptions, tournament_applications
):
    reported = user_to_message[chat_id]
    texts = [text for _, text in reported]
    reported_ids = {tournament_id for tournament_id, _ in reported}
    host = db.prefs(chat_id).get("host") or DEFAULT_HOST
    others = [t for t in user_to_subscriptions[chat_id] if t[0] not in reported_ids]
    for tournament_id, name in sorted(others):
        applications = tournament_applications.get((tournament_id, name))
        if applications:
            texts.append(
                f"Ранее нерассмотренные заявки на турнир <b>{tournament_id} {name}</b>."
                f" {review_link(host, tournament_id)}\n\n"
                + format_applications(applications, host)
            )
    return texts


async def make_reminders(bot: Bot, db: Database, config: Config) -> None:
    logger.debug("running regular job...")
    pending_writes = []
    user_to_message = defaultdict(list)

    for tournament in db.tournaments():
        logger.info(f"processing tournament {tournament.id}...")
        info = await asyncio.to_thread(get_info, tournament.id)
        if info_is_bad(info):
            logger.debug(f"adding request for removal of tourn_id {tournament.id}")
            pending_writes.append((db.delete_tournament, (tournament.id,)))
            continue
        if (DT(now()).dt - DT(info["dateEnd"]).dt).days >= 30:
            logger.debug(f"adding request for removal of tourn_id {tournament.id}")
            pending_writes.append((db.delete_tournament, (tournament.id,)))
            continue
        logger.debug(f"getting reminders for tournament {tournament.id}")
        try:
            reminders = await asyncio.to_thread(tourn_info_to_reminders, info, DT(now()))
        except Exception as e:
            logger.debug(
                f"exception {type(e)} {e} while trying to get reminders for {tournament.id}"
            )
            continue
        if not reminders:
            continue
        logger.debug(f"got reminders for tournament {tournament.id}")
        for chat_id, subscription in tournament.subscribers.items():
            wanted = {key for key in subscription if subscription[key]} & set(reminders)
            if not wanted:
                continue
            seen = []
            for key in wanted:
                if reminders[key] not in seen:
                    seen.append(reminders[key])
            user_to_message[chat_id].append("\n\n".join(seen))

    logger.info(f"got messages for {len(user_to_message)} chats")
    for chat_id, messages in user_to_message.items():
        logger.debug(f"processing messages to {chat_id}")
        text = "\n\n".join(messages)
        host = db.prefs(chat_id).get("host") or DEFAULT_HOST
        if host != DEFAULT_HOST:
            text = text.replace(DEFAULT_HOST, host)
        await send(bot, chat_id, text)

    _apply(pending_writes)


async def check_applications_debug(bot: Bot, db: Database, config: Config) -> None:
    await check_applications(bot, db, config, chat_ids_whitelist=config.admins)


async def test_job(bot: Bot, db: Database, config: Config) -> None:
    for chat_id in config.admins:
        await send(bot, chat_id, "<b>test</b>")


def _apply(pending_writes) -> None:
    if not pending_writes:
        return
    logger.debug("committing requests")
    for write, args in pending_writes:
        try:
            write(*args)
        except Exception as e:
            logger.debug(f"exception while trying to commit {args}: {type(e)} {e}")
    logger.debug("end of committing requests")
