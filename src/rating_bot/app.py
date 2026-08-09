"""Startup and wiring."""

import argparse
import asyncio
import datetime
import logging
from pathlib import Path

from aiogram import Bot, Dispatcher
from aiogram.client.default import DefaultBotProperties
from aiogram.enums import ParseMode
from apscheduler.schedulers.asyncio import AsyncIOScheduler

from rating_bot import config as config_module
from rating_bot.config import Config
from rating_bot.db import Database
from rating_bot.fsm import SqliteStorage
from rating_bot.handlers import ROUTERS
from rating_bot.handlers.common import BanGate, ResetStateOnCommand
from rating_bot.helpers import get_next_reminder_job_time
from rating_bot.jobs import check_applications, make_reminders
from rating_bot.logstore import LogStore, SqliteLogHandler
from rating_bot.middlewares import IncomingTrafficLog, OutgoingTrafficLog

logger = logging.getLogger("rating_bot")


def setup_logging(store: LogStore) -> None:
    console = logging.StreamHandler()
    console.setFormatter(logging.Formatter("%(asctime)s %(message)s"))
    to_sqlite = SqliteLogHandler(store)
    to_sqlite.setFormatter(logging.Formatter("%(message)s"))

    app_logger = logging.getLogger("rating_bot")
    app_logger.setLevel(logging.DEBUG)
    app_logger.propagate = False
    # Third-party loggers stay at WARNING; their DEBUG output is the firehose
    # that made the old text logs unreadable.
    root = logging.getLogger()
    root.setLevel(logging.WARNING)
    for handler in (console, to_sqlite):
        app_logger.addHandler(handler)
        root.addHandler(handler)


def schedule_jobs(
    scheduler: AsyncIOScheduler, bot: Bot, db: Database, config: Config, store: LogStore
) -> None:
    utcnow = datetime.datetime.now(datetime.UTC)
    delta_hours = 1 if utcnow.hour % 2 else 2
    first = (utcnow + datetime.timedelta(hours=delta_hours)).replace(
        minute=0, second=0, microsecond=0
    )
    scheduler.add_job(
        check_applications,
        "interval",
        hours=2,
        next_run_time=first,
        args=[bot, db, config],
    )
    scheduler.add_job(
        make_reminders,
        "interval",
        hours=24,
        next_run_time=get_next_reminder_job_time(),
        args=[bot, db, config],
    )
    scheduler.add_job(store.prune, "interval", hours=24)


def build_bot(config: Config, store: LogStore, session=None) -> Bot:
    bot = Bot(
        config.token,
        session=session,
        default=DefaultBotProperties(parse_mode=ParseMode.HTML),
    )
    bot.session.middleware(OutgoingTrafficLog(store))
    return bot


def build_dispatcher(
    config: Config, db: Database, store: LogStore, scheduler: AsyncIOScheduler
) -> Dispatcher:
    dispatcher = Dispatcher(storage=SqliteStorage(config.bot_db))
    dispatcher["db"] = db
    dispatcher["config"] = config
    dispatcher["scheduler"] = scheduler
    dispatcher["store"] = store
    dispatcher.update.outer_middleware(IncomingTrafficLog(store))
    dispatcher.message.outer_middleware(ResetStateOnCommand())
    dispatcher.message.middleware(BanGate())
    for router in ROUTERS:
        dispatcher.include_router(router)
    return dispatcher


async def run(config: Config) -> None:
    store = LogStore(config.logs_db)
    setup_logging(store)
    db = Database(config.bot_db)

    bot = build_bot(config, store)
    scheduler = AsyncIOScheduler()
    dispatcher = build_dispatcher(config, db, store, scheduler)

    if not config.debug:
        schedule_jobs(scheduler, bot, db, config, store)
    scheduler.start()

    logger.debug("Starting bot...")
    try:
        await dispatcher.start_polling(bot)
    finally:
        scheduler.shutdown(wait=False)
        await bot.session.close()


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--debug", action="store_true")
    parser.add_argument("--token_path")
    parser.add_argument(
        "--data-dir",
        default=".",
        help="where config.json, token and the databases live",
    )
    args = parser.parse_args()
    config = config_module.load(
        data_dir=Path(args.data_dir).resolve(),
        token_path=Path(args.token_path) if args.token_path else None,
        debug=args.debug,
    )
    asyncio.run(run(config))
