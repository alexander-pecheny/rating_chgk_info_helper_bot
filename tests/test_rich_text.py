"""Regression test for the announce that the bot refused on 2026-08-09.

The payload below is trimmed from the real update: a post-editor message whose
whole body lives in rich_message.blocks, with no text and no caption.
"""

from aiogram.types import Message

from rating_bot.rich_text import message_text

CUSTOM_EMOJI = {
    "type": "custom_emoji",
    "custom_emoji_id": "5199623533231101837",
    "alternative_text": "🍁",
}

RICH_UPDATE = {
    "message_id": 53686,
    "date": 1786266432,
    "chat": {"id": 608909090, "type": "private", "first_name": "Тимофей"},
    "rich_message": {
        "blocks": [
            {
                "type": "heading",
                "size": 1,
                "text": [CUSTOM_EMOJI, " АНОНС НОВОГО СЕЗОНА"],
            },
            {"type": "paragraph", "text": ""},
            {
                "type": "paragraph",
                "text": [
                    {"type": "custom_emoji", "custom_emoji_id": "1", "alternative_text": "🗓"},
                    " Фестиваль пройдет ",
                    {"type": "bold", "text": "13-15 ноября"},
                    " 2026 года в отеле «Москва» в Санкт-Петербурге.",
                ],
            },
            {
                "type": "photo",
                "photo": [
                    {
                        "file_id": "AgACAgIAAxUAAWp4Q0",
                        "file_unique_id": "AQADiBZrG1rvwUt4",
                        "file_size": 2027,
                        "width": 90,
                        "height": 83,
                    }
                ],
            },
            {
                "type": "paragraph",
                "text": "В этом году мы расширяемся и готовы принять 80 команд. Ждём и студентов, и молодёжь!",
            },
            {
                "type": "paragraph",
                "text": "Мы приглашаем команды для участия в двух призовых зачётах:",
            },
            {
                "type": "list",
                "items": [
                    {
                        "label": "•",
                        "blocks": [
                            {
                                "type": "paragraph",
                                "text": "Студенческий (все игроки родились не ранее 01.09.2003)",
                            }
                        ],
                    },
                    {
                        "label": "•",
                        "blocks": [
                            {
                                "type": "paragraph",
                                "text": "Молодежный (все игроки родились не ранее 01.09.1996)",
                            }
                        ],
                    },
                ],
            },
        ]
    },
}

PLAIN_UPDATE = {
    "message_id": 1,
    "date": 1786266432,
    "chat": {"id": 1, "type": "private"},
    "text": "обычный текст",
}


def rich_message() -> Message:
    return Message.model_validate(RICH_UPDATE)


def test_reads_text_the_old_code_could_not_see():
    message = rich_message()
    assert message.text is None
    assert message.caption is None
    assert "АНОНС НОВОГО СЕЗОНА" in message_text(message)


def test_keeps_styled_runs_and_emoji_but_not_structure():
    text = message_text(rich_message())
    assert "13-15 ноября" in text
    assert "🍁" in text
    for structural in ("paragraph", "heading", "custom_emoji", "AgACAgIAAxUAAWp4Q0"):
        assert structural not in text


def test_blocks_are_separate_lines_and_empties_dropped():
    lines = message_text(rich_message()).splitlines()
    assert lines[0] == "🍁 АНОНС НОВОГО СЕЗОНА"
    assert "" not in lines


def test_long_enough_to_pass_the_announce_minimum():
    assert len(message_text(rich_message())) >= 200


def test_plain_messages_are_untouched():
    assert message_text(Message.model_validate(PLAIN_UPDATE)) == "обычный текст"
