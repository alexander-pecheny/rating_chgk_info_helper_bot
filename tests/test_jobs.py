"""The scheduled application check, with the rating API stubbed out."""

import pytest

from rating_bot import jobs
from rating_bot.db import Tournament
from tests.conftest import CHAT_ID

pytestmark = pytest.mark.asyncio

FUTURE = "2030-12-31T00:00:00+03:00"
APPLICATION = {"7": {"status": "N", "rep": "123 Иван Иванов (Москва)"}}


@pytest.fixture
def tournament(db):
    db.add_tournament(
        Tournament(
            id=9002,
            name="Кубок",
            applications={},
            subscribers={CHAT_ID: {"r": 1, "i": 1, "a": 1}},
        )
    )
    return db.tournament(9002)


@pytest.fixture
def stub_api(monkeypatch):
    monkeypatch.setattr(
        jobs, "get_info", lambda tid: {"id": tid, "name": "Кубок", "dateEnd": FUTURE}
    )
    monkeypatch.setattr(jobs, "get_applications", lambda tid: dict(APPLICATION))


async def test_player_links_use_the_real_host(
    bot, db, config, session, tournament, stub_api
):
    await jobs.check_applications(bot, db, config)

    body = "\n".join(session.sent_texts())
    assert "{host}" not in body
    assert 'https://rating.chgk.info/player/123' in body


async def test_player_links_honour_the_chat_host_preference(
    bot, db, config, session, tournament, stub_api
):
    db.set_prefs(CHAT_ID, {"host": "rating.pecheny.me"})

    await jobs.check_applications(bot, db, config)

    body = "\n".join(session.sent_texts())
    assert "https://rating.pecheny.me/player/123" in body
    assert "rating.chgk.info" not in body


async def test_seen_applications_are_not_reported_twice(
    bot, db, config, session, tournament, stub_api
):
    await jobs.check_applications(bot, db, config)
    first = len(session.sent_texts())
    assert first == 1

    await jobs.check_applications(bot, db, config)
    assert len(session.sent_texts()) == first


async def test_a_failed_application_fetch_reports_nothing(
    bot, db, config, session, tournament, monkeypatch
):
    monkeypatch.setattr(
        jobs, "get_info", lambda tid: {"id": tid, "name": "Кубок", "dateEnd": FUTURE}
    )
    monkeypatch.setattr(jobs, "get_applications", lambda tid: None)

    await jobs.check_applications(bot, db, config)

    assert session.sent_texts() == []
    assert db.tournament(9002).applications == {}
