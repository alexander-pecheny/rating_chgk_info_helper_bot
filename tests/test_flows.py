"""Conversation flows and the two access gates."""

import pytest

from tests.conftest import ADMIN_ID, CHAT_ID

pytestmark = pytest.mark.asyncio


async def test_subscribe_without_ids_asks_and_keeps_asking(feed, session):
    await feed(text="/subscribe")
    assert any("укажите id турниров" in text for text in session.sent_texts())

    await feed(text="не число")
    assert sum("укажите id турниров" in text for text in session.sent_texts()) == 2


async def test_cancel_ends_the_flow(feed, session):
    await feed(text="/subscribe")
    await feed(text="/cancel")
    assert "Команда отменена." in session.sent_texts()

    await feed(text="не число")
    assert sum("укажите id турниров" in text for text in session.sent_texts()) == 1


async def test_banned_user_gets_nothing_else(feed, session, db):
    db.ban(CHAT_ID)
    await feed(text="/subscribe")
    assert session.sent_texts() == ["Вы забанены."]


async def test_non_admin_is_told_so(feed, session):
    await feed(text="/ban 123")
    assert "Вы не админ бота." in session.sent_texts()


async def test_admin_can_ban(feed, session, db, dispatcher, config):
    dispatcher["config"] = config.__class__(**{**config.__dict__, "admins": frozenset({CHAT_ID})})
    await feed(text="/ban 4242")
    assert 4242 in db.banned_users()
    assert "Пользователь 4242 забанен" in session.sent_texts()


async def test_dates_flow_walks_three_steps(feed, session):
    await feed(text="/get_dates")
    await feed(text="2026-11-13")
    assert any("Сколько дней от 1 до 7" in text for text in session.sent_texts())

    await feed(text="3")
    assert any("длиться асинхрон" in text for text in session.sent_texts())

    await feed(text="0")
    assert any("13" in text for text in session.sent_texts()[-1:])


async def test_dates_flow_rejects_a_bad_date(feed, session):
    await feed(text="/get_dates")
    await feed(text="не дата")
    assert any("Неверный формат" in text for text in session.sent_texts())


async def test_traffic_is_recorded_both_ways(feed, store):
    await feed(text="/start")
    with store._cursor() as cur:
        rows = cur.execute("select direction, text from traffic order by id").fetchall()
    directions = [row[0] for row in rows]
    assert directions == ["in", "out"]
    assert rows[0][1] == "/start"
    assert "Привет!" in rows[1][1]


async def test_admin_id_fixture_is_not_the_test_chat():
    assert ADMIN_ID != CHAT_ID
