"""Subscriber state: which chats watch which tournaments, and their preferences."""

import json
import sqlite3
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path

from rating_bot.helpers import ChatPrefsType, parse_chat_ids, serialize_chat_ids

SCHEMA = [
    """CREATE TABLE IF NOT EXISTS data (
    id integer PRIMARY KEY,
    name text,
    state text,
    chat_ids text,
    prefs text
);""",
    """CREATE TABLE IF NOT EXISTS chat_prefs (
    chat_id integer PRIMARY KEY,
    prefs text
)""",
    """CREATE TABLE IF NOT EXISTS banned_users (
    chat_id integer PRIMARY KEY
)""",
    """CREATE TABLE IF NOT EXISTS fsm_state (
    bot_id integer NOT NULL,
    chat_id integer NOT NULL,
    user_id integer NOT NULL,
    thread_id integer NOT NULL,
    destiny text NOT NULL,
    state text,
    data text NOT NULL,
    updated_at real NOT NULL,
    PRIMARY KEY (bot_id, chat_id, user_id, thread_id, destiny)
)""",
]


@dataclass
class Tournament:
    id: int
    name: str
    applications: dict
    subscribers: ChatPrefsType


class Database:
    def __init__(self, path: Path):
        self.path = path
        with self._cursor() as cur:
            for statement in SCHEMA:
                cur.execute(statement)

    @contextmanager
    def _cursor(self):
        conn = sqlite3.connect(self.path)
        try:
            yield conn.cursor()
            conn.commit()
        finally:
            conn.close()

    def _row_to_tournament(self, row) -> Tournament:
        return Tournament(
            id=row[0],
            name=row[1],
            applications=json.loads(row[2]),
            subscribers=parse_chat_ids(row[3]),
        )

    def tournaments(self) -> list[Tournament]:
        with self._cursor() as cur:
            rows = cur.execute("select id, name, state, chat_ids from data").fetchall()
        return [self._row_to_tournament(row) for row in rows]

    def tournament(self, tournament_id: int) -> Tournament | None:
        with self._cursor() as cur:
            rows = cur.execute(
                "select id, name, state, chat_ids from data where id = ?",
                (tournament_id,),
            ).fetchall()
        return self._row_to_tournament(rows[0]) if rows else None

    def add_tournament(self, tournament: Tournament) -> None:
        with self._cursor() as cur:
            cur.execute(
                "insert into data(id,name,state,chat_ids) values (?,?,?,?)",
                (
                    tournament.id,
                    tournament.name,
                    json.dumps(tournament.applications),
                    serialize_chat_ids(tournament.subscribers),
                ),
            )

    def set_subscribers(self, tournament_id: int, subscribers: ChatPrefsType) -> None:
        with self._cursor() as cur:
            cur.execute(
                "update data set chat_ids = ? where id = ?",
                (serialize_chat_ids(subscribers), tournament_id),
            )

    def set_applications(self, tournament_id: int, applications: dict) -> None:
        with self._cursor() as cur:
            cur.execute(
                "update data set state = ? where id = ?",
                (json.dumps(applications), tournament_id),
            )

    def delete_tournament(self, tournament_id: int) -> None:
        with self._cursor() as cur:
            cur.execute("delete from data where id = ?", (tournament_id,))

    def prefs(self, chat_id: int) -> dict:
        with self._cursor() as cur:
            rows = cur.execute(
                "select prefs from chat_prefs where chat_id = ?", (chat_id,)
            ).fetchall()
        return json.loads(rows[0][0]) if rows else {}

    def set_prefs(self, chat_id: int, prefs: dict) -> None:
        with self._cursor() as cur:
            cur.execute(
                """insert into chat_prefs(chat_id, prefs) values (?, ?)
                   on conflict(chat_id) do update set prefs = excluded.prefs""",
                (chat_id, json.dumps(prefs)),
            )

    def banned_users(self) -> set[int]:
        with self._cursor() as cur:
            rows = cur.execute("select chat_id from banned_users").fetchall()
        return {row[0] for row in rows}

    def ban(self, chat_id: int) -> bool:
        if chat_id in self.banned_users():
            return False
        with self._cursor() as cur:
            cur.execute("insert into banned_users(chat_id) values (?)", (chat_id,))
        return True

    def unban(self, chat_id: int) -> bool:
        if chat_id not in self.banned_users():
            return False
        with self._cursor() as cur:
            cur.execute("delete from banned_users where chat_id = ?", (chat_id,))
        return True
