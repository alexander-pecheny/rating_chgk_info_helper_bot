"""A dispatcher wired exactly as production, talking to a fake Telegram."""

import pytest
import pytest_asyncio
from aiogram import Bot
from aiogram.client.session.base import BaseSession
from aiogram.methods import CopyMessage, CopyMessages, ForwardMessages, TelegramMethod
from aiogram.types import Chat, Message, MessageId, Update, User
from apscheduler.schedulers.asyncio import AsyncIOScheduler

from rating_bot.app import build_bot, build_dispatcher
from rating_bot.config import Config
from rating_bot.db import Database
from rating_bot.logstore import LogStore

CHAT_ID = 608909090
ADMIN_ID = 663024
ANNOUNCE_CHANNEL_ID = -1001568906741


class FakeSession(BaseSession):
    """Records outgoing calls and fabricates plausible replies."""

    def __init__(self):
        super().__init__()
        self.calls: list[TelegramMethod] = []
        self.refuse: set[type] = set()
        self._next_id = 1000

    async def close(self) -> None:
        pass

    async def stream_content(self, *args, **kwargs):
        yield b""

    async def make_request(self, bot, method, timeout=None):
        self.calls.append(method)
        if type(method) in self.refuse:
            raise RuntimeError(f"{method.__api_method__} refused")
        self._next_id += 1
        if isinstance(method, (CopyMessage,)):
            return MessageId(message_id=self._next_id)
        if isinstance(method, (CopyMessages, ForwardMessages)):
            return [MessageId(message_id=self._next_id)]
        return Message(
            message_id=self._next_id,
            date=1786266432,
            chat=Chat(id=getattr(method, "chat_id", CHAT_ID), type="private"),
            from_user=User(id=1, is_bot=True, first_name="bot"),
            text=getattr(method, "text", None),
        )

    def sent_texts(self) -> list[str]:
        return [call.text for call in self.calls if getattr(call, "text", None)]

    def methods(self) -> list[str]:
        return [call.__api_method__ for call in self.calls]


@pytest.fixture
def config(tmp_path) -> Config:
    return Config(
        data_dir=tmp_path,
        token="123:fake",
        admins=frozenset({ADMIN_ID}),
        announce_channel_id=ANNOUNCE_CHANNEL_ID,
        debug=True,
    )


@pytest.fixture
def db(config) -> Database:
    return Database(config.bot_db)


@pytest.fixture
def store(config) -> LogStore:
    return LogStore(config.logs_db)


@pytest.fixture
def session() -> FakeSession:
    return FakeSession()


@pytest_asyncio.fixture
async def bot(config, session, store) -> Bot:
    yield build_bot(config, store, session=session)


@pytest.fixture(scope="session")
def _dispatcher(tmp_path_factory):
    # Handler routers are module-level singletons, so only one Dispatcher can
    # ever own them; each test repoints its state instead of rebuilding it.
    seed = tmp_path_factory.mktemp("dispatcher")
    seed_config = Config(
        data_dir=seed,
        token="123:fake",
        admins=frozenset({ADMIN_ID}),
        announce_channel_id=ANNOUNCE_CHANNEL_ID,
        debug=True,
    )
    return build_dispatcher(
        seed_config, Database(seed_config.bot_db), LogStore(seed_config.logs_db),
        AsyncIOScheduler(),
    )


@pytest.fixture
def dispatcher(_dispatcher, config, db, store):
    _dispatcher["config"] = config
    _dispatcher["db"] = db
    _dispatcher["store"] = store
    _dispatcher.storage.path = config.bot_db
    for middleware in (*_dispatcher.update.outer_middleware, ):
        if hasattr(middleware, "store"):
            middleware.store = store
    return _dispatcher


@pytest.fixture
def feed(dispatcher, bot):
    counter = {"update_id": 0, "message_id": 0}

    async def _feed(**message_fields) -> None:
        counter["update_id"] += 1
        counter["message_id"] += 1
        payload = {
            "message_id": counter["message_id"],
            "date": 1786266432,
            "chat": {"id": CHAT_ID, "type": "private"},
            "from": {"id": CHAT_ID, "is_bot": False, "first_name": "Тимофей"},
            **message_fields,
        }
        if isinstance(payload.get("text"), str) and payload["text"].startswith("/"):
            command = payload["text"].split()[0]
            payload["entities"] = [
                {"type": "bot_command", "offset": 0, "length": len(command)}
            ]
        update = Update.model_validate(
            {"update_id": counter["update_id"], "message": payload}
        )
        await dispatcher.feed_update(bot, update)

    return _feed
