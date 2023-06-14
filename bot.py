#!/usr/bin/env python
# -*- coding: utf-8 -*-
import os
import argparse
import datetime
import json
import logging
import logging.handlers
import time
import sqlite3
import subprocess
import sys
from collections import defaultdict

import httpx
from telegram import Update
from telegram.constants import ParseMode
from telegram.ext import (
    ApplicationBuilder,
    CommandHandler,
    ContextTypes,
    CallbackContext,
)

API = "https://api.rating.chgk.net"
DIR = os.path.dirname(os.path.abspath(__file__))
DB_LOC = os.path.join(DIR, "bot.db")
DB_INIT = """\
CREATE TABLE IF NOT EXISTS data (
    id integer PRIMARY KEY,
    name text,
    state text,
    chat_ids text
);
"""
START = """\
Привет! Это бот-помощник для турнирного сайта.

Он умеет оповещать о новых заявках на турниры.

Чтобы подписаться на обновления, напиши `/subscribe` и id турниров через запятую, вот так:

`/subscribe 7000, 9002, 9015`

Чтобы отписаться, то же самое, но с командой `/unsubscribe`.
"""
UTC_PLUS_3 = datetime.timezone(datetime.timedelta(seconds=10800))
ADMINS = []


class Formatter(logging.Formatter):
    def converter(self, timestamp):
        dt = datetime.datetime.fromtimestamp(timestamp)
        return dt.astimezone(UTC_PLUS_3)

    def formatTime(self, record, datefmt=None):
        dt = self.converter(record.created)
        if datefmt:
            s = dt.strftime(datefmt)
        else:
            return dt.strftime("%Y-%m-%d %H:%M:%S%z")
        return s


formatter = Formatter("%(asctime)s %(message)s")
fileHandler = logging.handlers.RotatingFileHandler(
    os.path.join(DIR, "rating_bot.log"), maxBytes=1024 * 1024 * 16
)
fileHandler.setFormatter(formatter)
consoleHandler = logging.StreamHandler()
consoleHandler.setFormatter(formatter)
logger = logging.getLogger("rating_bot")
logger.setLevel(logging.DEBUG)
logger.addHandler(consoleHandler)
logger.addHandler(fileHandler)
default_logger = logging.getLogger()
default_fileHandler = logging.handlers.RotatingFileHandler(
    os.path.join(DIR, "rating_bot_ext.log"), maxBytes=1024 * 1024 * 16
)
default_fileHandler.setFormatter(formatter)
default_logger.setLevel(logging.DEBUG)
default_logger.addHandler(default_fileHandler)


def now():
    return datetime.datetime.now(tz=UTC_PLUS_3)


def parse_dt(dt):
    return datetime.datetime.strptime(dt, "%Y-%m-%dT%H:%M:%S%z")


def db_init():
    if not os.path.isfile(DB_LOC):
        conn = sqlite3.connect(DB_LOC)
        cur = conn.cursor()
        cur.execute(DB_INIT)
        conn.commit()


def convert_request_info(request):
    rep = request["representative"]
    town = request["venue"]["town"]["name"]
    return {
        "status": request["status"],
        "rep": f"{rep['id']} {rep['name']} {rep['surname']} ({town})",
    }


def get_requests(t_id, only_new=True):
    logger.debug(f"getting requests from {t_id}...")
    req = httpx.get(f"{API}/tournaments/{t_id}/requests.json?pagination=false")
    time.sleep(0.5)
    if req.status_code != 200:
        sys.stderr.write(f"got response with error {req.status_code}: {req.text}\n")
        return
    obj = req.json()
    converted = {str(r["id"]): convert_request_info(r) for r in obj}
    if only_new:
        converted = {k: v for k, v in converted.items() if v["status"] == "N"}
    return converted


def get_info(t_id):
    req = httpx.get(f"{API}/tournaments/{t_id}.json")
    time.sleep(0.5)
    return req.json()


async def start(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    await update.message.reply_markdown(START)


def tryint(s):
    try:
        return int(s)
    except Exception as e:
        sys.stderr.write(f"couldn't convert {s} to int: {type(e)} {e}\n")
        return


def parse_chat_ids(chat_ids_str: str) -> list[int]:
    if not chat_ids_str:
        return []
    sp = chat_ids_str.split(",")
    return [int(x) for x in sp]


def serialize_chat_ids(chat_ids: list[int]) -> str:
    return ",".join(str(x) for x in chat_ids)


def get_req_form(x: int):
    s_x = str(x)
    if s_x.endswith(("11", "12", "13", "14")):
        return "нерассмотренных заявок"
    if s_x.endswith("1"):
        return "нерассмотренная заявка"
    if s_x.endswith(("2", "3", "4")):
        return "нерассмотренные заявки"
    return "нерассмотренных заявок"


def add_to_subscribers(tourn_id, chat_id) -> str:
    conn = sqlite3.connect(DB_LOC)
    cur = conn.cursor()
    data = cur.execute(
        f"""select id, name, state, chat_ids from data where id = {tourn_id}"""
    ).fetchall()
    if not data:
        reqs = get_requests(tourn_id)
        info = get_info(tourn_id)
        name = info["name"]
        logger.debug(
            f"adding tournament {tourn_id} to base, with chat {chat_id} as first subscriber"
        )
        cur.execute(
            """insert into data(id,name,state,chat_ids) values (?,?,?,?)""",
            (tourn_id, name, json.dumps(reqs), str(chat_id)),
        )
        conn.commit()
        msg = f"Вы теперь подписаны на турнир <b>{tourn_id} {name}</b>. Там {len(reqs)} {get_req_form(len(reqs))}."
        if reqs:
            msg += f""" <a href="https://rating.chgk.info/tournament/{tourn_id}/requests)">Рассмотреть</a>"""
        return msg
    else:
        data = data[0]
        name = data[1]
        reqs = json.loads(data[2])
        chat_ids = parse_chat_ids(data[3])
        if chat_id in chat_ids:
            return f"Вы уже подписаны на турнир <b>{tourn_id} {name}</b>."
        else:
            chat_ids.append(chat_id)
            logger.debug(
                f"adding chat {chat_id} to subscribers of tournament {tourn_id}"
            )
            cur.execute(
                """update data set chat_ids = ? where id = ?""",
                (serialize_chat_ids(chat_ids), tourn_id),
            )
            conn.commit()
            msg = f"Вы теперь подписаны на турнир <b>{tourn_id} {name}</b>. Там {len(reqs)} {get_req_form(len(reqs))}."
            if reqs:
                msg += f""" <a href="https://rating.chgk.info/tournament/{tourn_id}/requests)">Рассмотреть</a>"""
            return msg


def remove_from_subscribers(tourn_id, chat_id) -> str:
    conn = sqlite3.connect(DB_LOC)
    cur = conn.cursor()
    data = cur.execute(
        f"""select id, name, state, chat_ids from data where id = {tourn_id}"""
    ).fetchall()
    if not data:
        return f"Вы и так не подписаны на турнир {tourn_id}."
    else:
        data = data[0]
        name = data[1]
        chat_ids = parse_chat_ids(data[3])
        if chat_id in chat_ids:
            chat_ids = [x for x in chat_ids if x != chat_id]
            if chat_ids:
                logger.debug(
                    f"removing chat {chat_id} from subscribers of tournament {tourn_id}"
                )
                cur.execute(
                    """update data set chat_ids = ? where id = ?""",
                    (serialize_chat_ids(chat_ids), tourn_id),
                )
            else:
                logger.debug(f"removing tournament {tourn_id} from base")
                cur.execute("""delete from data where id = ?""", (tourn_id,))
            conn.commit()
            return f"Вы теперь отписаны от турнира <b>{tourn_id} {name}</b>."
        else:
            return f"Вы и так не подписаны на турнир {tourn_id}."


async def subscribe(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    text = update.message.text[len("/subscribe") :]
    tourn_ids = [tryint(s.strip()) for s in text.split(",") if tryint(s.strip())]
    msgs = []
    for id_ in tourn_ids:
        msgs.append(add_to_subscribers(id_, update.effective_chat.id))
    if msgs:
        await update.message.reply_html("\n".join([x for x in msgs if x]))
    else:
        await update.message.reply_text(
            "Пожалуйста, укажите id турниров через запятую."
        )


async def unsubscribe(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    text = update.message.text[len("/unsubscribe") :]
    tourn_ids = [tryint(s.strip()) for s in text.split(",") if tryint(s.strip())]
    msgs = []
    for id_ in tourn_ids:
        msgs.append(remove_from_subscribers(id_, update.effective_chat.id))
    if msgs:
        await update.message.reply_html("\n".join([x for x in msgs if x]))
    else:
        await update.message.reply_text(
            "Пожалуйста, укажите id турниров через запятую."
        )


def wrap_link(s):
    id_ = s.split()[0]
    return f"""<a href="https://rating.chgk.info/player/{id_}">{s}</a>"""


def tourn_wrap_link(s):
    id_ = s.split()[0]
    return f"""<a href="https://rating.chgk.info/tournament/{id_}">{s}</a>"""


def get_sorting_key(x):
    sp = x.split()
    return (sp[1], sp[2], sp[0])


async def check_requests(context: CallbackContext):
    logger.debug("running regular job...")
    conn = sqlite3.connect(DB_LOC)
    cur = conn.cursor()
    data = cur.execute("""select id, name, state, chat_ids from data""").fetchall()
    for x in data:
        logger.debug(x)
    reqs_for_committing = []
    user_to_message = defaultdict(list)
    for rec in data:
        tourn_id = rec[0]
        tourn_name = rec[1]
        reqs = json.loads(rec[2])
        chat_ids = parse_chat_ids(rec[3])
        new_reqs = get_requests(tourn_id)
        info = get_info(tourn_id)
        new_diff = set(new_reqs) - set(reqs)
        old_diff = set(reqs) - set(new_reqs)
        if new_diff or old_diff:
            logger.debug(f"adding request for updating data for tourn_id {tourn_id}")
            reqs_for_committing.append(
                (
                    """update data set state = ? where id = ?""",
                    (json.dumps(new_reqs), tourn_id),
                )
            )
        if new_diff:
            logger.debug(
                f"adding requests for sending messages for tourn_id {tourn_id}"
            )
            srt = sorted([new_reqs[x]["rep"] for x in new_reqs], key=get_sorting_key)
            text = (
                f'Для турнира <b>{tourn_id} {tourn_name}</b> есть {len(new_reqs)} {get_req_form(len(new_reqs))}. <a href="https://rating.chgk.info/tournament/{tourn_id}/requests">Рассмотреть</a>\n\n'
                + "\n".join([wrap_link(rep) for rep in srt])
            )
            for chat_id in chat_ids:
                user_to_message[chat_id].append(text)
        if parse_dt(info["dateEnd"]) < now():
            logger.debug(f"adding request for removal of tourn_id {tourn_id}")
            reqs_for_committing.append(
                ("""delete from data where id = ?""", (tourn_id,))
            )
    for chat_id in user_to_message:
        logger.debug(f"sending messages to {chat_id}")
        final_text = "\n\n".join(user_to_message[chat_id])
        await context.application.bot.send_message(
            chat_id, final_text, parse_mode=ParseMode.HTML
        )
    if reqs_for_committing:
        logger.debug("committing requests")
    for tup in reqs_for_committing:
        cur.execute(*tup)
        conn.commit()


async def echo_md(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    text = update.message.text[len("/echo_md ") :]
    await update.message.reply_markdown(text)


async def debug_info(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    if update.effective_chat.id not in ADMINS:
        await update.message.reply_text("Вы не админ бота.")
        return
    next_ts = sorted([j.next_t for j in context.application.job_queue.jobs()])[0]
    await update.message.reply_text(f"next regular job will be run at {next_ts}")


async def echo_html(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    text = update.message.text[len("/echo_html ") :]
    await update.message.reply_html(text)


async def log_tail(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    if update.effective_chat.id not in ADMINS:
        await update.message.reply_text("Вы не админ бота.")
        return
    log_path = os.path.join(DIR, "rating_bot.log")
    if not os.path.isfile(log_path):
        await update.message.reply_text("Лог-файл не найден.")
        return
    with open(log_path, "r") as f:
        cnt = f.read().split("\n")
    await update.message.reply_text("\n".join(cnt[-25:]))


async def get_subscribers(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    if update.effective_chat.id not in ADMINS:
        await update.message.reply_text("Вы не админ бота.")
        return
    conn = sqlite3.connect(DB_LOC)
    data = conn.cursor().execute("""select id, name, chat_ids from data""").fetchall()
    user_to_tourns = defaultdict(list)
    for row in data:
        chat_ids = parse_chat_ids(row[2])
        for chat_id in chat_ids:
            user_to_tourns[chat_id].append(f"{row[0]} {row[1]}")
    result = []
    for i, user in enumerate(sorted(user_to_tourns)):
        tourns = user_to_tourns[user]
        result.append(
            f"{i + 1}. {user} - {len(tourns)} tournaments ({', '.join(tourns)})"
        )
    result.append(f"{len(result)} chats subscribed to {len(data)} unique tournaments")
    await update.message.reply_text("\n".join(result))


async def my_subscriptions(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    conn = sqlite3.connect(DB_LOC)
    data = conn.cursor().execute("""select id, name, chat_ids from data""").fetchall()
    user_to_tourns = defaultdict(list)
    for row in data:
        chat_ids = parse_chat_ids(row[2])
        for chat_id in chat_ids:
            user_to_tourns[chat_id].append(f"{row[0]} {row[1]}")
    tourns = user_to_tourns[update.effective_chat.id]
    text = "Турниры, на которые вы подписаны:\n" + "\n".join(
        [tourn_wrap_link(s) for s in tourns]
    )
    await update.message.reply_html(text)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--debug", action="store_true")
    args = parser.parse_args()

    admins_path = os.path.join(DIR, "admins.json")
    if os.path.exists(admins_path):
        with open(admins_path) as f:
            ADMINS.extend(json.loads(f.read()))

    db_init()
    token_path = os.path.join(DIR, "token")
    if os.path.isfile(token_path):
        with open(token_path, "r") as f:
            token = f.read().strip()
    else:
        sys.stderr.write(f"no token found at {token_path}\n")
        sys.exit(1)

    app = ApplicationBuilder().token(token).build()
    if args.debug:
        app.job_queue.run_repeating(check_requests, 60)
    else:
        utcnow = datetime.datetime.utcnow()
        if utcnow.hour % 2:
            delta_hours = 1
        else:
            delta_hours = 2
        first = (utcnow + datetime.timedelta(hours=delta_hours)).replace(
            minute=0, second=0, microsecond=0
        )
        app.job_queue.run_repeating(
            check_requests,
            datetime.timedelta(hours=2),
            first=first,
        )
    app.add_handler(CommandHandler("start", start))
    app.add_handler(CommandHandler("subscribe", subscribe))
    app.add_handler(CommandHandler("unsubscribe", unsubscribe))
    app.add_handler(CommandHandler("my_subscriptions", my_subscriptions))

    #  admin commands below
    app.add_handler(CommandHandler("debug_info", debug_info))
    app.add_handler(CommandHandler("log_tail", log_tail))
    app.add_handler(CommandHandler("get_subscribers", get_subscribers))
    if args.debug:
        app.add_handler(CommandHandler("echo_md", echo_md))
        app.add_handler(CommandHandler("echo_html", echo_html))
    logger.debug("Starting bot...")
    app.run_polling()


if __name__ == "__main__":
    main()
