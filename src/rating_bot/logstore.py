"""Queryable log storage: message traffic and the application log, in sqlite.

Replaces the rotating text logs. Long-polling calls are skipped, so the traffic
table holds only what a person actually sent or the bot actually said.
"""

import json
import logging
import sqlite3
import time
from contextlib import contextmanager
from pathlib import Path
from typing import Any

from aiogram.client.default import Default

MAX_BYTES = 100 * 1024 * 1024
_PRUNE_BATCH = 5000

SCHEMA = [
    """CREATE TABLE IF NOT EXISTS traffic (
    id integer PRIMARY KEY AUTOINCREMENT,
    ts real NOT NULL,
    direction text NOT NULL,
    method text,
    chat_id integer,
    text text,
    payload text NOT NULL
)""",
    "CREATE INDEX IF NOT EXISTS traffic_ts ON traffic(ts)",
    "CREATE INDEX IF NOT EXISTS traffic_chat_id ON traffic(chat_id)",
    """CREATE TABLE IF NOT EXISTS app_log (
    id integer PRIMARY KEY AUTOINCREMENT,
    ts real NOT NULL,
    level text NOT NULL,
    logger text NOT NULL,
    message text NOT NULL
)""",
    "CREATE INDEX IF NOT EXISTS app_log_ts ON app_log(ts)",
]


class LogStore:
    def __init__(self, path: Path, max_bytes: int = MAX_BYTES):
        self.path = path
        self.max_bytes = max_bytes
        with self._cursor() as cur:
            # Must precede table creation to take effect on a fresh database.
            cur.execute("PRAGMA auto_vacuum = INCREMENTAL")
            cur.execute("PRAGMA journal_mode = WAL")
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

    def record_traffic(
        self,
        direction: str,
        payload: Any,
        method: str | None = None,
        chat_id: int | None = None,
        text: str | None = None,
    ) -> None:
        with self._cursor() as cur:
            cur.execute(
                "insert into traffic(ts, direction, method, chat_id, text, payload)"
                " values (?,?,?,?,?,?)",
                (time.time(), direction, method, chat_id, text, payload),
            )

    def record_log(self, ts: float, level: str, logger: str, message: str) -> None:
        with self._cursor() as cur:
            cur.execute(
                "insert into app_log(ts, level, logger, message) values (?,?,?,?)",
                (ts, level, logger, message),
            )

    def size(self) -> int:
        with self._cursor() as cur:
            pages = cur.execute("PRAGMA page_count").fetchone()[0]
            page_size = cur.execute("PRAGMA page_size").fetchone()[0]
        return pages * page_size

    def prune(self) -> int:
        """Drop the oldest rows until the file fits under the cap."""
        removed = 0
        while self.size() > self.max_bytes:
            batch = 0
            with self._cursor() as cur:
                for table in ("traffic", "app_log"):
                    count = cur.execute(f"select count(*) from {table}").fetchone()[0]
                    if not count:
                        continue
                    cur.execute(
                        f"delete from {table} where id in"
                        f" (select id from {table} order by ts limit ?)",
                        (min(_PRUNE_BATCH, max(1, count // 10)),),
                    )
                    batch += max(cur.rowcount, 0)
            self._reclaim()
            if batch == 0:
                break
            removed += batch
        return removed

    def _reclaim(self) -> None:
        # incremental_vacuum yields rows and refuses to share a transaction.
        conn = sqlite3.connect(self.path, isolation_level=None)
        try:
            conn.execute("PRAGMA incremental_vacuum").fetchall()
        finally:
            conn.close()


class SqliteLogHandler(logging.Handler):
    def __init__(self, store: LogStore):
        super().__init__()
        self.store = store

    def emit(self, record: logging.LogRecord) -> None:
        try:
            self.store.record_log(
                record.created, record.levelname, record.name, self.format(record)
            )
        except Exception:  # a broken log sink must not take the bot down
            self.handleError(record)


def _encode(value: Any) -> Any:
    # Unset fields carrying bot-wide defaults say nothing about this call.
    return None if isinstance(value, Default) else str(value)


def dumps(model: Any) -> str:
    payload = model.model_dump(exclude_none=True) if hasattr(model, "model_dump") else model
    return json.dumps(payload, ensure_ascii=False, default=_encode)
