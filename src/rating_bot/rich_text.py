"""Plain text extraction from Bot API 10.1 rich messages.

A rich message carries no ``text``; its content lives in ``rich_message.blocks``,
where every text position is either a bare string, a styled run, or a custom
emoji standing in for a character. Flattening gives us something to measure the
length of, and something to log.
"""

from collections.abc import Iterator
from typing import Any

from aiogram.types import Message

# Keys whose string values are read by a human. Everything else in a block is
# structure: block types, file ids, dimensions, emoji ids.
_TEXT_KEYS = frozenset({"text", "caption", "alternative_text", "title"})


def _strings(node: Any, is_text: bool) -> Iterator[str]:
    if isinstance(node, str):
        if is_text:
            yield node
    elif isinstance(node, (list, tuple)):
        for item in node:
            yield from _strings(item, is_text)
    elif isinstance(node, dict):
        for key, value in node.items():
            yield from _strings(value, key in _TEXT_KEYS)


def rich_message_text(message: Message) -> str:
    """Every visible run of a rich message, one line per block."""
    rich = getattr(message, "rich_message", None)
    if rich is None:
        return ""
    lines = []
    for block in rich.blocks or ():
        line = "".join(_strings(block.model_dump(), is_text=False)).strip()
        if line:
            lines.append(line)
    return "\n".join(lines)


def message_text(message: Message) -> str:
    """The prose of a message, wherever Telegram chose to put it."""
    return message.text or message.caption or rich_message_text(message)
