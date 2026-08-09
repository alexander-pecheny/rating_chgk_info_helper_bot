"""Conversation state that survives a restart but not an absent user.

aiogram's bundled storages are memory (lost on deploy) or Redis (not here), so
this is a small sqlite one. State older than ``ttl`` reads as absent, which is
what stops a forgotten /announce from swallowing an unrelated message next week.
"""

import json
import sqlite3
import time
from collections.abc import Mapping
from contextlib import contextmanager
from pathlib import Path
from typing import Any

from aiogram.fsm.state import State
from aiogram.fsm.storage.base import BaseStorage, StorageKey

IDLE_TTL_SECONDS = 300.0


class SqliteStorage(BaseStorage):
    def __init__(self, path: Path, ttl: float = IDLE_TTL_SECONDS):
        self.path = path
        self.ttl = ttl

    @contextmanager
    def _cursor(self):
        conn = sqlite3.connect(self.path)
        try:
            yield conn.cursor()
            conn.commit()
        finally:
            conn.close()

    @staticmethod
    def _key(key: StorageKey) -> tuple:
        return (
            key.bot_id,
            key.chat_id,
            key.user_id,
            key.thread_id or 0,
            key.destiny,
        )

    def _read(self, key: StorageKey) -> tuple[str | None, dict]:
        with self._cursor() as cur:
            rows = cur.execute(
                """select state, data, updated_at from fsm_state
                   where bot_id=? and chat_id=? and user_id=? and thread_id=? and destiny=?""",
                self._key(key),
            ).fetchall()
            if not rows:
                return None, {}
            state, data, updated_at = rows[0]
            if time.time() - updated_at > self.ttl:
                cur.execute(
                    """delete from fsm_state
                       where bot_id=? and chat_id=? and user_id=? and thread_id=? and destiny=?""",
                    self._key(key),
                )
                return None, {}
        return state, json.loads(data)

    def _write(self, key: StorageKey, state: str | None, data: dict) -> None:
        with self._cursor() as cur:
            cur.execute(
                """insert into fsm_state
                   (bot_id, chat_id, user_id, thread_id, destiny, state, data, updated_at)
                   values (?,?,?,?,?,?,?,?)
                   on conflict(bot_id, chat_id, user_id, thread_id, destiny) do update set
                   state = excluded.state, data = excluded.data, updated_at = excluded.updated_at""",
                (*self._key(key), state, json.dumps(data), time.time()),
            )

    async def set_state(self, key: StorageKey, state: State | str | None = None) -> None:
        resolved = state.state if isinstance(state, State) else state
        _, data = self._read(key)
        if resolved is None and not data:
            with self._cursor() as cur:
                cur.execute(
                    """delete from fsm_state
                       where bot_id=? and chat_id=? and user_id=? and thread_id=? and destiny=?""",
                    self._key(key),
                )
            return
        self._write(key, resolved, data)

    async def get_state(self, key: StorageKey) -> str | None:
        return self._read(key)[0]

    async def set_data(self, key: StorageKey, data: Mapping[str, Any]) -> None:
        state, _ = self._read(key)
        self._write(key, state, dict(data))

    async def get_data(self, key: StorageKey) -> dict[str, Any]:
        return self._read(key)[1]

    async def update_data(
        self, key: StorageKey, data: Mapping[str, Any]
    ) -> dict[str, Any]:
        state, current = self._read(key)
        current.update(data)
        self._write(key, state, current)
        return current

    async def get_value(
        self, storage_key: StorageKey, dict_key: str, default: Any | None = None
    ) -> Any | None:
        return self._read(storage_key)[1].get(dict_key, default)

    async def close(self) -> None:
        pass
