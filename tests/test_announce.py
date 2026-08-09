"""The announce flow, including the rich message the old bot refused."""

import asyncio

import pytest
from aiogram.methods import CopyMessage, ForwardMessage

from tests.conftest import ANNOUNCE_CHANNEL_ID, CHAT_ID
from tests.test_rich_text import RICH_UPDATE

pytestmark = pytest.mark.asyncio

FORWARD_ORIGIN = {
    "type": "channel",
    "date": 1786266171,
    "message_id": 413,
    "chat": {"id": -1001533750283, "type": "channel", "title": "Полифест"},
}
LONG_TEXT = "Анонс турнира. " * 20


async def test_rich_message_is_announced(feed, session):
    await feed(text="/announce")
    await feed(
        rich_message=RICH_UPDATE["rich_message"], forward_origin=FORWARD_ORIGIN
    )

    assert "forwardMessage" in session.methods()
    forward = next(c for c in session.calls if isinstance(c, ForwardMessage))
    assert forward.chat_id == ANNOUNCE_CHANNEL_ID
    assert forward.from_chat_id == CHAT_ID
    assert "Анонс успешно отправлен!" in session.sent_texts()


async def test_plain_text_announce_is_copied_not_forwarded(feed, session):
    await feed(text="/announce")
    await feed(text=LONG_TEXT)

    assert "copyMessage" in session.methods()
    assert "forwardMessage" not in session.methods()
    assert "Анонс успешно отправлен!" in session.sent_texts()


async def test_falls_back_to_forwarding_when_copy_is_refused(feed, session):
    session.refuse.add(CopyMessage)
    await feed(text="/announce")
    await feed(text=LONG_TEXT)

    assert session.methods().count("copyMessage") == 1
    assert "forwardMessage" in session.methods()
    assert "Анонс успешно отправлен!" in session.sent_texts()


async def test_short_announce_is_rejected_and_flow_continues(feed, session):
    await feed(text="/announce")
    await feed(text="коротко")

    assert "copyMessage" not in session.methods()
    assert any("Слишком короткое" in text for text in session.sent_texts())

    await feed(text=LONG_TEXT)
    assert "copyMessage" in session.methods()


async def test_textless_message_is_refused_not_ignored(feed, session):
    await feed(text="/announce")
    await feed(sticker={
        "file_id": "x",
        "file_unique_id": "y",
        "width": 1,
        "height": 1,
        "type": "regular",
        "is_animated": False,
        "is_video": False,
    })

    assert "copyMessage" not in session.methods()
    assert any("Не получилось распарсить" in text for text in session.sent_texts())


async def test_album_is_announced_whole(feed, session):
    await feed(text="/announce")
    photo = [{"file_id": "a", "file_unique_id": "b", "width": 90, "height": 90}]
    await feed(media_group_id="42", photo=photo, caption=LONG_TEXT)
    await feed(media_group_id="42", photo=photo)
    await asyncio.sleep(1.2)

    assert "copyMessages" in session.methods()
    copy = next(c for c in session.calls if c.__api_method__ == "copyMessages")
    assert len(copy.message_ids) == 2
    assert "Анонс успешно отправлен!" in session.sent_texts()


async def test_command_escapes_the_announce_state(feed, session):
    await feed(text="/announce")
    await feed(text="/start")

    assert "copyMessage" not in session.methods()
    assert any("Привет!" in text for text in session.sent_texts())


async def test_a_later_router_command_also_escapes_the_state(feed, session):
    await feed(text="/announce")
    await feed(text="/get_dates_prefs")

    assert "copyMessage" not in session.methods()
    assert any("настройки дат" in text for text in session.sent_texts())


async def test_slash_prefixed_announcement_is_relayed_not_dropped(feed, session):
    await feed(text="/announce")
    await feed(text="/12.03 — отбор. " + "Подробности турнира. " * 15)

    assert "copyMessage" in session.methods()
    assert "Анонс успешно отправлен!" in session.sent_texts()


async def test_cancel_stops_a_pending_album(feed, session):
    await feed(text="/announce")
    photo = [{"file_id": "a", "file_unique_id": "b", "width": 90, "height": 90}]
    await feed(media_group_id="77", photo=photo, caption=LONG_TEXT)
    await feed(text="/cancel")
    await asyncio.sleep(1.2)

    assert "copyMessages" not in session.methods()
    assert "copyMessage" not in session.methods()
    assert "Команда отменена." in session.sent_texts()


async def test_late_album_item_does_not_start_a_second_post(feed, session):
    await feed(text="/announce")
    photo = [{"file_id": "a", "file_unique_id": "b", "width": 90, "height": 90}]
    await feed(media_group_id="88", photo=photo, caption=LONG_TEXT)
    await asyncio.sleep(1.2)
    assert session.methods().count("copyMessage") == 1

    await feed(media_group_id="88", photo=photo)
    await asyncio.sleep(1.2)
    assert session.methods().count("copyMessage") == 1
    assert "copyMessages" not in session.methods()
