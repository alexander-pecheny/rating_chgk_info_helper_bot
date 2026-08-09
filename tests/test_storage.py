"""The two custom storages: FSM state expiry, and the log size cap."""

import pytest
from aiogram.fsm.storage.base import StorageKey

from rating_bot.db import Database
from rating_bot.fsm import SqliteStorage
from rating_bot.logstore import LogStore

pytestmark = pytest.mark.asyncio

KEY = StorageKey(bot_id=1, chat_id=2, user_id=2)


@pytest.fixture
def storage(tmp_path):
    path = tmp_path / "bot.db"
    Database(path)
    return SqliteStorage(path, ttl=100.0)


async def test_state_and_data_survive_a_new_storage_instance(storage, tmp_path):
    await storage.set_state(KEY, "Flows:announce")
    await storage.update_data(KEY, {"sync_days": 3})

    reopened = SqliteStorage(tmp_path / "bot.db", ttl=100.0)
    assert await reopened.get_state(KEY) == "Flows:announce"
    assert await reopened.get_data(KEY) == {"sync_days": 3}


async def test_state_older_than_the_ttl_reads_as_absent(tmp_path):
    path = tmp_path / "bot.db"
    Database(path)
    storage = SqliteStorage(path, ttl=-1.0)
    await storage.set_state(KEY, "Flows:announce")
    assert await storage.get_state(KEY) is None


async def test_clearing_state_without_data_removes_the_row(storage):
    await storage.set_state(KEY, "Flows:announce")
    await storage.set_state(KEY, None)
    with storage._cursor() as cur:
        assert cur.execute("select count(*) from fsm_state").fetchone()[0] == 0


async def test_prune_drops_oldest_rows_until_under_cap(tmp_path):
    store = LogStore(tmp_path / "logs.db", max_bytes=120 * 1024)
    payload = "x" * 2000
    for i in range(400):
        store.record_traffic("in", payload, chat_id=i, text="t")
    assert store.size() > store.max_bytes

    store.prune()

    assert store.size() <= store.max_bytes
    with store._cursor() as cur:
        remaining = cur.execute("select count(*) from traffic").fetchone()[0]
        oldest = cur.execute("select min(chat_id) from traffic").fetchone()[0]
    assert 0 < remaining < 400
    assert oldest > 0


async def test_prune_is_a_no_op_when_small(tmp_path):
    store = LogStore(tmp_path / "logs.db")
    store.record_traffic("in", "{}", chat_id=1, text="t")
    assert store.prune() == 0
    with store._cursor() as cur:
        assert cur.execute("select count(*) from traffic").fetchone()[0] == 1


async def test_prune_stops_rather_than_emptying_a_file_it_cannot_shrink(
    tmp_path, monkeypatch
):
    store = LogStore(tmp_path / "logs.db", max_bytes=120 * 1024)
    for i in range(400):
        store.record_traffic("in", "x" * 2000, chat_id=i, text="t")
    monkeypatch.setattr(store, "_reclaim", lambda: None)

    store.prune()

    with store._cursor() as cur:
        remaining = cur.execute("select count(*) from traffic").fetchone()[0]
    assert remaining > 0
