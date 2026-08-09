"""Subscribing a chat to a tournament, and the messages that reports."""

import logging

from rating_bot.db import Database, Tournament
from rating_bot.helpers import (
    DEFAULT_HOST,
    default_subscription,
    get_application_form,
)
from rating_bot.ratingutil import get_applications, get_info, info_is_bad

logger = logging.getLogger("rating_bot")

JURY_ONLY = {"r": 0, "i": 1, "a": 0}


def review_link(host: str, tournament_id: int) -> str:
    return f"""<a href="https://{host}/tournament/{tournament_id}/requests">Рассмотреть</a>"""


def _subscribed(tournament: Tournament, suffix: str, host: str) -> str:
    count = len(tournament.applications)
    message = (
        f"Вы теперь подписаны на турнир <b>{tournament.id} {tournament.name}</b>"
        f"{suffix}. Там {count} {get_application_form(count)}."
    )
    if count:
        message += f" {review_link(host, tournament.id)}"
    return message


def subscribe(
    db: Database, tournament_id: int, chat_id: int, *, jury_only: bool = False
) -> str:
    subscription = JURY_ONLY if jury_only else default_subscription()
    suffix = " в качестве ИЖ" if jury_only else ""
    host = db.prefs(chat_id).get("host") or DEFAULT_HOST

    tournament = db.tournament(tournament_id)
    if tournament is None:
        info = get_info(tournament_id)
        if info_is_bad(info) or not info.get("name"):
            return f"Турнир {tournament_id} не найден."
        tournament = Tournament(
            id=tournament_id,
            name=info["name"],
            applications=get_applications(tournament_id) or {},
            subscribers={chat_id: subscription},
        )
        logger.debug(
            f"adding tournament {tournament_id} to base, with chat {chat_id} as first subscriber"
        )
        db.add_tournament(tournament)
        return _subscribed(tournament, suffix, host)

    if chat_id in tournament.subscribers:
        return f"Вы уже подписаны на турнир <b>{tournament_id} {tournament.name}</b>."

    tournament.subscribers[chat_id] = subscription
    logger.debug(f"adding chat {chat_id} to subscribers of tournament {tournament_id}")
    db.set_subscribers(tournament_id, tournament.subscribers)
    return _subscribed(tournament, suffix, host)


def unsubscribe(db: Database, tournament_id: int, chat_id: int) -> str:
    tournament = db.tournament(tournament_id)
    if tournament is None or chat_id not in tournament.subscribers:
        return f"Вы и так не подписаны на турнир {tournament_id}."

    tournament.subscribers.pop(chat_id, None)
    if tournament.subscribers:
        logger.debug(
            f"removing chat {chat_id} from subscribers of tournament {tournament_id}"
        )
        db.set_subscribers(tournament_id, tournament.subscribers)
    else:
        logger.debug(f"removing tournament {tournament_id} from base")
        db.delete_tournament(tournament_id)
    return f"Вы теперь отписаны от турнира <b>{tournament_id} {tournament.name}</b>."
