#!/usr/bin/env python
# -*- coding: utf-8 -*-
import argparse
import datetime
import functools
import itertools
import json
import logging
from pathlib import Path
import logging.handlers
import os
import re
import sqlite3
import subprocess
import sys
import urllib
from collections import defaultdict

from dateutil import DatesPrefs, generate_dates, parse_dt_prefs, tryint, DT
from ratingutil import get_tourn_top3, _get_requests, _get_info, tourn_info_to_reminders
from telegram import Update
from telegram.constants import ParseMode
from telegram.ext import (
    ApplicationBuilder,
    CallbackContext,
    CommandHandler,
    ContextTypes,
    ConversationHandler,
    MessageHandler,
    filters,
)

API = "https://api.rating.chgk.net"
DIR = os.path.dirname(os.path.abspath(__file__))
DB_LOC = os.path.join(DIR, "bot.db")
DB_INIT = [
    """CREATE TABLE IF NOT EXISTS data (
    id integer PRIMARY KEY,
    name text,
    state text,
    chat_ids text,
    prefs text
);""",
    """CREATE TABLE IF NOT EXISTS chat_prefs (
    chat_id integer PRIMARY KEY,
    prefs text
)""",
    """CREATE TABLE IF NOT EXISTS banned_users (
    chat_id integer PRIMARY KEY
)""",
]
START = """\
Привет! Это бот-помощник для турнирного сайта.

Он умеет оповещать о новых заявках на турниры.

Чтобы подписаться на обновления, напиши `/subscribe` и id турниров через запятую, вот так:

`/subscribe 7000, 9002, 9015`

Чтобы отписаться, то же самое, но с командой `/unsubscribe`.
"""
ID_TOURNS_TEXT = "Пожалуйста, укажите id турниров через запятую или /cancel для отмены."
ID_TOURN_TEXT = "Пожалуйста, укажите id турнира или /cancel для отмены."
NOT_CANCEL = "^(?!/cancel)"
UTC_PLUS_3 = datetime.timezone(datetime.timedelta(seconds=10800))
DEFAULT_HOST = "rating.chgk.info"
CONFIG = json.loads((Path(DIR) / "config.json").read_text())
ANNOUNCE_CHANNEL_ID = CONFIG["announce_channel_id"]
ADMINS = CONFIG["admins"]


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


log_suffix = "_debug" if "token_path" in " ".join(sys.argv) else ""
formatter = Formatter("%(asctime)s %(message)s")
fileHandler = logging.handlers.RotatingFileHandler(
    os.path.join(DIR, f"rating_bot{log_suffix}.log"),
    maxBytes=1024 * 1024 * 16,
    backupCount=1,
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
    os.path.join(DIR, "rating_bot_ext.log"), maxBytes=1024 * 1024 * 16, backupCount=1
)
default_fileHandler.setFormatter(formatter)
default_logger.setLevel(logging.DEBUG)
default_logger.addHandler(default_fileHandler)


def now():
    return datetime.datetime.now(tz=UTC_PLUS_3)


def parse_dt(dt):
    return datetime.datetime.strptime(dt, "%Y-%m-%dT%H:%M:%S%z")


def info_is_bad(info):
    detail = info.get("detail")
    return detail and detail.lower() == "not found"


def db_init():
    conn = sqlite3.connect(DB_LOC)
    cur = conn.cursor()
    for statement in DB_INIT:
        cur.execute(statement)
    conn.commit()


def convert_request_info(request):
    rep = request["representative"]
    town = request["venue"]["town"]["name"]
    return {
        "status": request["status"],
        "rep": f"{rep['id']} {rep['name']} {rep['surname']} ({town})",
    }


async def try_send_message(context, *args, **kwargs):
    try:
        return await context.application.bot.send_message(*args, **kwargs)
    except Exception as e:
        logger.error(
            f"exception {type(e)} {e} while trying to send message with args {args} {kwargs}"
        )


async def try_send_photo(context, *args, **kwargs):
    try:
        return await context.application.bot.send_photo(*args, **kwargs)
    except Exception as e:
        logger.error(
            f"exception {type(e)} {e} while trying to send photo with args {args} {kwargs}"
        )


async def try_copy_message(context, *args, **kwargs):
    try:
        return await context.application.bot.copy_message(*args, **kwargs)
    except Exception as e:
        logger.error(
            f"exception {type(e)} {e} while trying to send photo with args {args} {kwargs}"
        )


async def try_forward_message(context, *args, **kwargs):
    try:
        return await context.application.bot.forward_message(*args, **kwargs)
    except Exception as e:
        logger.error(
            f"exception {type(e)} {e} while trying to send photo with args {args} {kwargs}"
        )


def get_banned_users():
    conn = sqlite3.connect(DB_LOC)
    cur = conn.cursor()
    banned_users = cur.execute("""select chat_id from banned_users;""").fetchall()
    return {x[0] for x in banned_users}


def ban_user(chat_id):
    if chat_id in get_banned_users():
        return False
    conn = sqlite3.connect(DB_LOC)
    cur = conn.cursor()
    cur.execute("""insert into banned_users(chat_id) values (?)""", (chat_id,))
    conn.commit()
    return True


def unban_user(chat_id):
    if chat_id not in get_banned_users():
        return False
    conn = sqlite3.connect(DB_LOC)
    cur = conn.cursor()
    cur.execute("""delete from banned_users where chat_id = ?""", (chat_id,))
    conn.commit()
    return True


def admin_command(func):
    @functools.wraps(func)
    async def wrapped_func(update: Update, context: ContextTypes.DEFAULT_TYPE):
        if update.effective_chat.id not in ADMINS:
            await update.message.reply_text("Вы не админ бота.")
            return
        return await func(update, context)

    return wrapped_func


def command(func):
    @functools.wraps(func)
    async def wrapped_func(update: Update, context: ContextTypes.DEFAULT_TYPE):
        if update.effective_chat.id in get_banned_users():
            await update.message.reply_text("Вы забанены.")
            return
        return await func(update, context)

    return wrapped_func


@command
async def start(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    await update.message.reply_markdown(START)


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


def get_prefs(chat_id):
    conn = sqlite3.connect(DB_LOC)
    cur = conn.cursor()
    prefs_list = cur.execute(
        f"""select chat_id, prefs from chat_prefs where chat_id = {chat_id}"""
    ).fetchall()
    if prefs_list:
        return json.loads(prefs_list[0][1])
    return {}


def add_to_subscribers(tourn_id, chat_id) -> str:
    conn = sqlite3.connect(DB_LOC)
    cur = conn.cursor()
    data = cur.execute(
        f"""select id, name, state, chat_ids from data where id = {tourn_id}"""
    ).fetchall()
    prefs = get_prefs(chat_id)
    host = prefs.get("host") or DEFAULT_HOST
    if not data:
        reqs = _get_requests(tourn_id)
        info = _get_info(tourn_id)
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
            msg += f""" <a href="https://{host}/tournament/{tourn_id}/requests">Рассмотреть</a>"""
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
                msg += f""" <a href="https://{host}/tournament/{tourn_id}/requests">Рассмотреть</a>"""
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


def set_host_req(host: str, chat_id: int) -> str:
    conn = sqlite3.connect(DB_LOC)
    cur = conn.cursor()
    data = cur.execute(
        f"""select chat_id, prefs from chat_prefs where chat_id = {chat_id}"""
    ).fetchall()
    if data:
        prefs = json.loads(data[0][1])
    else:
        prefs = {}
    prefs["host"] = host
    if data:
        cur.execute(
            """update chat_prefs set prefs = ? where chat_id = ?""",
            (json.dumps(prefs), chat_id),
        )
    else:
        cur.execute(
            """insert into chat_prefs(chat_id,prefs) values (?,?)""",
            (chat_id, json.dumps(prefs)),
        )
    conn.commit()
    return f"Ваш хост теперь <pre>{host}</pre>."


def set_dates_prefs_req(dates_update: str, chat_id: int) -> str:
    conn = sqlite3.connect(DB_LOC)
    cur = conn.cursor()
    data = cur.execute(
        f"""select chat_id, prefs from chat_prefs where chat_id = {chat_id}"""
    ).fetchall()
    if data:
        prefs = json.loads(data[0][1])
    else:
        prefs = {}
    curr_dates = DatesPrefs()
    curr_dates.update_from_dict(prefs.get("dates") or {})
    curr_dates.update_from_str(dates_update)
    prefs["dates"] = curr_dates.json()
    if data:
        cur.execute(
            """update chat_prefs set prefs = ? where chat_id = ?""",
            (json.dumps(prefs), chat_id),
        )
    else:
        cur.execute(
            """insert into chat_prefs(chat_id,prefs) values (?,?)""",
            (chat_id, json.dumps(prefs)),
        )
    conn.commit()
    return f"Ваши настройки дат теперь: {curr_dates.hr()}."


def _get_dates_prefs_req(chat_id: int) -> DatesPrefs:
    conn = sqlite3.connect(DB_LOC)
    cur = conn.cursor()
    data = cur.execute(
        f"""select chat_id, prefs from chat_prefs where chat_id = {chat_id}"""
    ).fetchall()
    if data:
        prefs = json.loads(data[0][1])
    else:
        prefs = {}
    curr_dates = DatesPrefs()
    curr_dates.update_from_dict(prefs.get("dates") or {})
    return curr_dates


def get_dates_prefs_req(chat_id: int) -> str:
    curr_dates = _get_dates_prefs_req(chat_id)
    return f"Ваши настройки дат: {curr_dates.hr()}."


def get_list_of_ints(str_):
    sp1 = str_.split(",")
    return [
        tryint(x)
        for x in itertools.chain.from_iterable(x.split() for x in sp1)
        if tryint(x)
    ]


@command
async def subscribe_msg(update: Update, context: ContextTypes.DEFAULT_TYPE) -> int:
    text = update.message.text
    tourn_ids = get_list_of_ints(text)
    msgs = []
    for id_ in tourn_ids:
        msgs.append(add_to_subscribers(id_, update.effective_chat.id))
    if msgs:
        await update.message.reply_html("\n".join([x for x in msgs if x]))
        return -1
    else:
        await update.message.reply_html(ID_TOURNS_TEXT)
        return 1


@command
async def subscribe(update: Update, context: ContextTypes.DEFAULT_TYPE) -> int:
    text = update.message.text[len("/subscribe") :]
    tourn_ids = get_list_of_ints(text)
    msgs = []
    for id_ in tourn_ids:
        msgs.append(add_to_subscribers(id_, update.effective_chat.id))
    if msgs:
        await update.message.reply_html("\n".join([x for x in msgs if x]))
        return -1
    else:
        await update.message.reply_html(ID_TOURNS_TEXT)
        return 1


@command
async def get_top3_msg(update: Update, context: ContextTypes.DEFAULT_TYPE) -> int:
    text = update.message.text
    tourn_ids = get_list_of_ints(text)
    if len(tourn_ids) == 1:
        await update.message.reply_html(get_tourn_top3(tourn_ids[0]))
        return -1
    else:
        await update.message.reply_html(ID_TOURN_TEXT)
        return 1


@command
async def get_top3(update: Update, context: ContextTypes.DEFAULT_TYPE) -> int:
    text = update.message.text[len("/get_top3") :]
    tourn_ids = get_list_of_ints(text)
    if len(tourn_ids) == 1:
        await update.message.reply_html(get_tourn_top3(tourn_ids[0]))
        return -1
    else:
        await update.message.reply_html(ID_TOURN_TEXT)
        return 1


@command
async def unsubscribe_msg(update: Update, context: ContextTypes.DEFAULT_TYPE) -> int:
    text = update.message.text
    tourn_ids = get_list_of_ints(text)
    msgs = []
    for id_ in tourn_ids:
        msgs.append(remove_from_subscribers(id_, update.effective_chat.id))
    if msgs:
        await update.message.reply_html("\n".join([x for x in msgs if x]))
        return -1
    else:
        await update.message.reply_html(ID_TOURNS_TEXT)
        return 1


@command
async def unsubscribe(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    text = update.message.text[len("/unsubscribe") :]
    tourn_ids = get_list_of_ints(text)
    msgs = []
    for id_ in tourn_ids:
        msgs.append(remove_from_subscribers(id_, update.effective_chat.id))
    if msgs:
        await update.message.reply_html("\n".join([x for x in msgs if x]))
        return -1
    else:
        await update.message.reply_text(ID_TOURNS_TEXT)
        return 1


RE_HOST = re.compile("[a-z][a-z\\.]+[a-z]")


def strip_host(host):
    host = (host or "").strip()
    for prefix in ("http://", "https://", "www."):
        if host.startswith(prefix):
            host = host[len(prefix) :]
    host = urllib.parse.urlparse("https://" + host).netloc
    return host


def validate_host(host):
    return host and RE_HOST.search(host) and "." in host


@command
async def set_host(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    text = update.message.text[len("/set_host") :]
    host = strip_host(text)
    msgs = []
    if not host:
        await update.message.reply_html(
            "Введите новый хост (например, <pre>rating.chgk.info</pre> или <pre>rating.pecheny.me</pre>)"
        )
        return 1
    elif validate_host(host):
        msgs.append(set_host_req(host, update.effective_chat.id))
    else:
        await update.message.reply_html(
            "Не удалось распарсить хост, попробуйте ещё раз"
        )
        return 1
    if msgs:
        await update.message.reply_html("\n".join([x for x in msgs if x]))
    return -1


@command
async def set_host_msg(update: Update, context: ContextTypes.DEFAULT_TYPE) -> int:
    text = update.message.text
    host = strip_host(text)
    msgs = []
    if validate_host(host):
        msgs.append(set_host_req(host, update.effective_chat.id))
    else:
        await update.message.reply_html(
            "Не удалось распарсить хост, попробуйте ещё раз"
        )
        return 1
    if msgs:
        await update.message.reply_html("\n".join([x for x in msgs if x]))
    return -1


def wrap_link(s):
    id_ = s.split()[0]
    return f"""<a href="https://{{host}}/player/{id_}">{s}</a>"""


def tourn_wrap_link(s):
    id_ = s.split()[0]
    return f"""<a href="https://{{host}}/tournament/{id_}">{s}</a>"""


def get_sorting_key(x):
    sp = x.split()
    return (sp[1], sp[2], sp[0])


def make_msg_from_reqs(reqs):
    srt = sorted([reqs[x]["rep"] for x in reqs], key=get_sorting_key)
    return "\n".join([wrap_link(rep) for rep in srt])


async def _check_requests(context: CallbackContext, chat_ids_whitelist=None):
    logger.debug("running regular job...")
    conn = sqlite3.connect(DB_LOC)
    cur = conn.cursor()
    data = cur.execute("""select id, name, state, chat_ids from data""").fetchall()
    for x in data:
        logger.debug(x)
    reqs_for_committing = []
    user_to_message = defaultdict(list)
    tourn_to_reqs = {}
    user_to_subscriptions = defaultdict(list)

    def _check_requests_inner(tourn_id, tourn_name, reqs, chat_ids):
        new_reqs = _get_requests(tourn_id)
        new_diff = set(new_reqs) - set(reqs)
        old_diff = set(reqs) - set(new_reqs)
        tourn_to_reqs[(tourn_id, tourn_name)] = new_reqs
        if (new_diff or old_diff) and not chat_ids_whitelist:
            logger.debug(
                f"adding request for updating data for tourn_id {tourn_id}: was {reqs}, became {new_reqs}"
            )
            reqs_for_committing.append(
                (
                    """update data set state = ? where id = ?""",
                    (json.dumps(new_reqs), tourn_id),
                )
            )
        if new_diff:
            logger.debug(
                f"adding requests for sending messages for tourn_id {tourn_id} for chat_ids {','.join(str(x) for x in chat_ids)}"
            )
            text = (
                f'Для турнира <b>{tourn_id} {tourn_name}</b> есть {len(new_reqs)} {get_req_form(len(new_reqs))}. <a href="https://{{host}}/tournament/{tourn_id}/requests">Рассмотреть</a>\n\n'
                + make_msg_from_reqs(new_reqs)
            )
            for chat_id in chat_ids:
                if chat_ids_whitelist and chat_id not in chat_ids_whitelist:
                    continue
                prefs = get_prefs(chat_id)
                host = prefs.get("host") or DEFAULT_HOST
                user_to_message[chat_id].append((tourn_id, text.format(host=host)))
                logger.debug(f"added message for {chat_id} about {tourn_id}")

    for rec in data:
        tourn_id = rec[0]
        tourn_name = rec[1]
        reqs = json.loads(rec[2])
        chat_ids = parse_chat_ids(rec[3])
        for chat_id in chat_ids:
            user_to_subscriptions[chat_id].append((tourn_id, tourn_name))
        info = _get_info(tourn_id)
        if DT(info["dateEnd"]) > DT(now()):
            _check_requests_inner(tourn_id, tourn_name, reqs, chat_ids)
        if not chat_ids_whitelist and info_is_bad(info):
            logger.debug(f"adding request for removal of tourn_id {tourn_id}")
            reqs_for_committing.append(
                ("""delete from data where id = ?""", (tourn_id,))
            )
    logger.debug(f"total {len(user_to_message)} chats will be messaged")

    def get_messages_for_chat(chat_id):
        tups = user_to_message[chat_id]
        texts = [x[1] for x in tups]
        tourn_ids = [x[0] for x in tups]
        other_subscribed_tourns = [
            t for t in user_to_subscriptions[chat_id] if t[0] not in tourn_ids
        ]
        prefs = get_prefs(chat_id)
        host = prefs.get("host") or DEFAULT_HOST
        for t in sorted(other_subscribed_tourns):
            tourn_id, tourn_name = t
            reqs = tourn_to_reqs.get(t)
            if reqs:
                texts.append(
                    f"""Ранее нерассмотренные заявки на турнир <b>{tourn_id} {tourn_name}</b>. <a href="https://{host}/tournament/{tourn_id}/requests">Рассмотреть</a>\n\n"""
                    + make_msg_from_reqs(reqs)
                )
        return texts

    for chat_id in user_to_message:
        if chat_ids_whitelist and chat_id not in chat_ids_whitelist:
            continue
        logger.debug(f"processing messages to {chat_id}")
        try:
            texts = get_messages_for_chat(chat_id)
        except Exception as e:
            logger.error(
                f"error while trying to process messages for {chat_id}: {type(e)} {e}"
            )
            continue
        final_text = "\n\n".join(texts)
        if not final_text:
            continue
        logger.debug(f"sending messages to {chat_id}")
        for batch in get_batches(final_text):
            await try_send_message(context, chat_id, batch, parse_mode=ParseMode.HTML)
    if not chat_ids_whitelist and reqs_for_committing:
        logger.debug("committing requests")
        for tup in reqs_for_committing:
            try:
                cur.execute(*tup)
                conn.commit()
            except Exception as e:
                logger.debug(
                    f"exception while trying to commit req {tup}: {type(e)} {e}"
                )
        logger.debug("end of committing requests")


async def test_job(context: CallbackContext):
    for chat_id in ADMINS:
        await try_send_message(
            context, chat_id, "<b>test</b>", parse_mode=ParseMode.HTML
        )


async def check_requests(context: CallbackContext):
    await _check_requests(context=context)


async def make_reminders(context: CallbackContext):
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
        logger.info(f"processing tournament {tourn_id}...")
        chat_ids = parse_chat_ids(rec[3])
        info = _get_info(tourn_id)
        if info_is_bad(info):
            logger.debug(f"adding request for removal of tourn_id {tourn_id}")
            reqs_for_committing.append(
                ("""delete from data where id = ?""", (tourn_id,))
            )
            continue
        if (DT(now()).dt - DT(info["dateEnd"]).dt).days >= 30:
            logger.debug(f"adding request for removal of tourn_id {tourn_id}")
            reqs_for_committing.append(
                ("""delete from data where id = ?""", (tourn_id,))
            )
            continue
        reminders = tourn_info_to_reminders(info, DT(now()))
        if reminders:
            for chat_id in chat_ids:
                user_to_message(chat_id).append(reminders)
    logger.info(f"got messages for {len(user_to_message)} chats")
    for chat_id in user_to_message:
        logger.debug(f"processing messages to {chat_id}")
        text = "\n\n".join(user_to_message[chat_id])
        prefs = get_prefs(chat_id)
        host = prefs.get("host") or DEFAULT_HOST
        if host != DEFAULT_HOST:
            text = text.replace(DEFAULT_HOST, host)
        for batch in get_batches(text):
            await try_send_message(context, chat_id, batch, parse_mode=ParseMode.HTML)
    if reqs_for_committing:
        logger.debug("committing requests")
        for tup in reqs_for_committing:
            try:
                cur.execute(*tup)
                conn.commit()
            except Exception as e:
                logger.debug(
                    f"exception while trying to commit req {tup}: {type(e)} {e}"
                )
        logger.debug("end of committing requests")


async def check_requests_debug(context: CallbackContext):
    await _check_requests(context=context, chat_ids_whitelist=ADMINS)


@admin_command
async def echo_md(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    text = update.message.text[len("/echo_md ") :]
    await update.message.reply_markdown(text)


@admin_command
async def debug_info(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    next_ts = sorted([j.next_t for j in context.application.job_queue.jobs()])[0]
    await update.message.reply_text(f"next regular job will be run at {next_ts}")


@admin_command
async def echo_html(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    text = update.message.text[len("/echo_html ") :]
    await update.message.reply_html(text)


@admin_command
async def log_tail(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    log_path = os.path.join(DIR, "rating_bot.log")
    if not os.path.isfile(log_path):
        await update.message.reply_text("Лог-файл не найден.")
        return
    text = subprocess.check_output(["/usr/bin/tail", log_path, "-n", "25"])
    if text:
        await update.message.reply_text(text.decode("utf8"))


@admin_command
async def log_grep(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    log_path = os.path.join(DIR, "rating_bot.log")
    if not os.path.isfile(log_path):
        await update.message.reply_text("Лог-файл не найден.")
        return
    text = update.message.text[len("/log_grep ") :]
    proc1 = subprocess.Popen(["/usr/bin/grep", text, log_path], stdout=subprocess.PIPE)
    proc2 = subprocess.Popen(
        ["/usr/bin/tail", "-n", "25"], stdin=proc1.stdout, stdout=subprocess.PIPE
    )
    proc1.stdout.close()
    out, _ = proc2.communicate()
    if out:
        await update.message.reply_text(out.decode("utf8"))


@admin_command
async def ban(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    chat_id = tryint(update.message.text[len("/ban") :].strip())
    if not chat_id:
        await update.message.reply_text("ID чата не распознан")
        return
    if chat_id in get_banned_users():
        await update.message.reply_text(f"Пользователь {chat_id} уже забанен")
        return
    result = ban_user(chat_id)
    if result:
        await update.message.reply_text(f"Пользователь {chat_id} забанен")
    else:
        await update.message.reply_text("Что-то пошло не так :(")


@admin_command
async def unban(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    chat_id = tryint(update.message.text[len("/unban") :].strip())
    if not chat_id:
        await update.message.reply_text("ID чата не распознан")
        return
    if chat_id not in get_banned_users():
        await update.message.reply_text(f"Пользователь {chat_id} и так не забанен")
        return
    result = unban_user(chat_id)
    if result:
        await update.message.reply_text(f"Пользователь {chat_id} разбанен")
    else:
        await update.message.reply_text("Что-то пошло не так :(")


@admin_command
async def run_check_requests(
    update: Update, context: ContextTypes.DEFAULT_TYPE
) -> None:
    await update.message.reply_text("running regular job..")
    context.application.job_queue.run_once(check_requests, when=1)


@admin_command
async def run_make_reminders(
    update: Update, context: ContextTypes.DEFAULT_TYPE
) -> None:
    await update.message.reply_text("running regular job..")
    context.application.job_queue.run_once(make_reminders, when=1)


@admin_command
async def run_check_requests_debug(
    update: Update, context: ContextTypes.DEFAULT_TYPE
) -> None:
    await update.message.reply_text("running regular job in debug mode..")
    context.application.job_queue.run_once(check_requests_debug, when=1)


@admin_command
async def run_test_job(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    await update.message.reply_text("running test job..")
    context.application.job_queue.run_once(test_job, when=1)


def get_batches(res):
    batches = []
    while len(res) >= 2048:
        batch, res = res[:2047], res[2047:]
        batches.append(batch)
    batches.append(res)
    return batches


@admin_command
async def get_subscribers(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
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
    res = "\n".join(result)
    for batch in get_batches(res):
        await update.message.reply_text(batch)


@command
async def my_subscriptions(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    conn = sqlite3.connect(DB_LOC)
    data = conn.cursor().execute("""select id, name, chat_ids from data""").fetchall()
    user_to_tourns = defaultdict(list)
    for row in data:
        chat_ids = parse_chat_ids(row[2])
        for chat_id in chat_ids:
            user_to_tourns[chat_id].append(f"{row[0]} {row[1]}")
    chat_id = update.effective_chat.id
    tourns = user_to_tourns[chat_id]
    prefs = get_prefs(chat_id)
    host = prefs.get("host") or "rating.chgk.info"
    if tourns:
        text = "Турниры, на которые вы подписаны:\n" + "\n".join(
            [tourn_wrap_link(s).format(host=host) for s in tourns]
        )
        await update.message.reply_html(text)
    else:
        await update.message.reply_text("Сейчас вы не подписаны ни на один турнир.")


@command
async def get_dates_entry(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    await update.message.reply_html(
        "Введите дату либо дату и время (UTC+3) начала синхрона в формате: <pre>2023-01-31</pre> либо <pre>2023-01-31 11:00</pre>"
    )
    return 1


@command
async def get_dates_enter_date(
    update: Update, context: ContextTypes.DEFAULT_TYPE
) -> None:
    try:
        text = update.message.text.strip()
        _ = parse_dt_prefs(text)
        context.user_data["date_sync_start"] = text
        await update.message.reply_html("Сколько дней от 1 до 7 будет длиться синхрон?")
        return 2
    except:
        await update.message.reply_html(
            "Неверный формат. Введите дату либо дату и время (UTC+3) начала синхрона в формате: <pre>2023-01-31</pre> либо <pre>2023-01-31 11:00</pre>"
        )
        return 1


@command
async def get_dates_enter_sync_days(
    update: Update, context: ContextTypes.DEFAULT_TYPE
) -> None:
    try:
        text = update.message.text.strip()
        days = tryint(text)
        if days:
            context.user_data["date_sync_days"] = days
            await update.message.reply_html(
                "Сколько дней будет длиться асинхрон? Если не хотите асинхрон, напишите 0"
            )
            return 3
        else:
            await update.message.reply_html(
                "Неверный формат. Введите количество дней от 1 до 7"
            )
            return 2
    except:
        await update.message.reply_html(
            "Неверный формат. Введите количество дней от 1 до 7"
        )
        return 2


@command
async def get_dates_enter_async_days(
    update: Update, context: ContextTypes.DEFAULT_TYPE
) -> None:
    text = update.message.text.strip()
    async_days = tryint(text) or 0
    date_sync_start = context.user_data["date_sync_start"]
    date_sync_days = context.user_data["date_sync_days"]
    prefs = _get_dates_prefs_req(chat_id=update.effective_chat.id)
    dates = generate_dates(date_sync_start, date_sync_days, async_days, prefs)
    await update.message.reply_html(dates)
    return -1


@command
async def get_dates_prefs(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    prefs = get_dates_prefs_req(update.effective_chat.id)
    await update.message.reply_html(prefs)


@command
async def set_dates_prefs_entry(
    update: Update, context: ContextTypes.DEFAULT_TYPE
) -> None:
    text = update.message.text[len("/set_dates_prefs ") :]
    if text:
        reply = set_dates_prefs_req(dates_update=text, chat_id=update.effective_chat.id)
        await update.message.reply_html(reply)
        return -1
    else:
        reply = (
            get_dates_prefs_req(update.effective_chat.id)
            + "\n\nВведите новые настройки:"
        )
        await update.message.reply_html(reply)
        return 1


@command
async def set_dates_prefs(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    text = update.message.text
    reply = set_dates_prefs_req(dates_update=text, chat_id=update.effective_chat.id)
    await update.message.reply_html(reply)
    return -1


@command
async def announce_entry(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    reply = "Пришлите сообщение для отправки в канал с анонсами:"
    await update.message.reply_html(reply)
    return 1


def udumps(s):
    return json.dumps(s, ensure_ascii=False)


@command
async def announce(update: Update, context: ContextTypes.DEFAULT_TYPE) -> None:
    if update.message.photo:
        if len(update.message.caption) < 200:
            await update.message.reply_html(
                "Слишком короткое сообщение. Введите новую версию или нажмите /cancel"
            )
            return 1
        if update.message.forward_from_chat:
            method = try_forward_message
        else:
            method = try_copy_message
        msg = await method(
            context,
            chat_id=ANNOUNCE_CHANNEL_ID,
            from_chat_id=update.effective_chat.id,
            message_id=update.message.message_id,
        )
        if msg:
            await update.message.reply_html("Анонс успешно отправлен!")
            logger.info(
                f"user {update.effective_chat.id} sent announce {udumps(update.message.caption)}"
            )
        else:
            await update.message.reply_html(
                "Что-то пошло не так :( Напишите разработчику бота"
            )
            logger.error(
                f"error while trying to send announce {udumps(update.message.caption)} from user {update.effective_chat.id}"
            )
        return -1
    elif update.message.text:
        if len(update.message.text) < 200:
            await update.message.reply_html(
                "Слишком короткое сообщение. Введите новую версию или нажмите /cancel"
            )
            return 1
        if update.message.forward_from_chat:
            method = try_forward_message
        else:
            method = try_copy_message
        msg = await method(
            context,
            chat_id=ANNOUNCE_CHANNEL_ID,
            from_chat_id=update.effective_chat.id,
            message_id=update.message.message_id,
        )
        if msg:
            await update.message.reply_html("Анонс успешно отправлен!")
            logger.info(
                f"user {update.effective_chat.id} sent announce {udumps(update.message.text)}"
            )
        else:
            await update.message.reply_html(
                "Что-то пошло не так :( Напишите разработчику бота"
            )
            logger.error(
                f"error while trying to send announce {udumps(update.message.text)} from user {update.effective_chat.id}"
            )
        return -1
    else:
        await update.message.reply_html(
            "Не получилось распарсить сообщение :( Введите новую версию или нажмите /cancel"
        )
        return 1


@command
async def cancel(update: Update, context: ContextTypes.DEFAULT_TYPE) -> int:
    """Cancels and ends the conversation."""
    await update.message.reply_text("Команда отменена.")
    logger.info(f"chat {update.effective_chat.id} canceled the conversation.")
    return ConversationHandler.END


def get_next_reminder_job_time():
    now = datetime.datetime.now(UTC_PLUS_3)
    target = now.replace(hour=15, minute=30, second=0, microsecond=0)
    if target < now:
        target += datetime.timedelta(days=1)
    return target


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--debug", action="store_true")
    parser.add_argument("--token_path")
    args = parser.parse_args()

    db_init()
    token_path = args.token_path or os.path.join(DIR, "token")
    if os.path.isfile(token_path):
        with open(token_path, "r") as f:
            token = f.read().strip()
    else:
        sys.stderr.write(f"no token found at {token_path}\n")
        sys.exit(1)

    app = ApplicationBuilder().token(token).build()
    if not args.debug:
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
        next_reminder_job_time = get_next_reminder_job_time()
        app.job_queue.run_repeating(
            make_reminders,
            datetime.timedelta(hours=24),
            first=next_reminder_job_time,
        )
    app.add_handler(CommandHandler("start", start))
    conv_handler = ConversationHandler(
        entry_points=[CommandHandler("subscribe", subscribe)],
        states={
            1: [MessageHandler(filters.Regex(NOT_CANCEL), subscribe_msg)],
        },
        fallbacks=[CommandHandler("cancel", cancel)],
        conversation_timeout=300,
    )
    app.add_handler(conv_handler)
    conv_handler = ConversationHandler(
        entry_points=[CommandHandler("unsubscribe", unsubscribe)],
        states={
            1: [MessageHandler(filters.Regex(NOT_CANCEL), unsubscribe_msg)],
        },
        fallbacks=[CommandHandler("cancel", cancel)],
        conversation_timeout=300,
    )
    app.add_handler(conv_handler)
    conv_handler = ConversationHandler(
        entry_points=[CommandHandler("get_top3", get_top3)],
        states={
            1: [MessageHandler(filters.Regex(NOT_CANCEL), get_top3_msg)],
        },
        fallbacks=[CommandHandler("cancel", cancel)],
        conversation_timeout=300,
    )
    app.add_handler(conv_handler)
    conv_handler = ConversationHandler(
        entry_points=[CommandHandler("set_host", set_host)],
        states={
            1: [MessageHandler(filters.Regex(NOT_CANCEL), set_host_msg)],
        },
        fallbacks=[CommandHandler("cancel", cancel)],
        conversation_timeout=300,
    )
    app.add_handler(conv_handler)
    app.add_handler(CommandHandler("my_subscriptions", my_subscriptions))
    conv_handler = ConversationHandler(
        entry_points=[CommandHandler("get_dates", get_dates_entry)],
        states={
            1: [MessageHandler(filters.Regex(NOT_CANCEL), get_dates_enter_date)],
            2: [MessageHandler(filters.Regex(NOT_CANCEL), get_dates_enter_sync_days)],
            3: [MessageHandler(filters.Regex(NOT_CANCEL), get_dates_enter_async_days)],
        },
        fallbacks=[CommandHandler("cancel", cancel)],
        conversation_timeout=300,
    )
    app.add_handler(conv_handler)
    conv_handler = ConversationHandler(
        entry_points=[CommandHandler("set_dates_prefs", set_dates_prefs_entry)],
        states={
            1: [MessageHandler(filters.Regex(NOT_CANCEL), set_dates_prefs)],
        },
        fallbacks=[CommandHandler("cancel", cancel)],
        conversation_timeout=300,
    )
    app.add_handler(conv_handler)
    app.add_handler(CommandHandler("get_dates_prefs", get_dates_prefs))
    conv_handler = ConversationHandler(
        entry_points=[CommandHandler("announce", announce_entry)],
        states={
            1: [
                MessageHandler(filters.Regex(NOT_CANCEL), announce),
                MessageHandler(filters.FORWARDED, announce),
                MessageHandler(filters.PHOTO, announce),
            ],
        },
        fallbacks=[CommandHandler("cancel", cancel)],
        conversation_timeout=300,
    )
    app.add_handler(conv_handler)

    #  admin commands below
    app.add_handler(CommandHandler("debug_info", debug_info))
    app.add_handler(CommandHandler("log_tail", log_tail))
    app.add_handler(CommandHandler("log_grep", log_grep))
    app.add_handler(CommandHandler("ban", ban))
    app.add_handler(CommandHandler("unban", unban))
    app.add_handler(CommandHandler("get_subscribers", get_subscribers))
    app.add_handler(CommandHandler("run_check_requests", run_check_requests))
    app.add_handler(CommandHandler("run_make_reminders", run_make_reminders))
    app.add_handler(
        CommandHandler("run_check_requests_debug", run_check_requests_debug)
    )
    app.add_handler(CommandHandler("run_test_job", run_test_job))
    if args.debug:
        app.add_handler(CommandHandler("echo_md", echo_md))
        app.add_handler(CommandHandler("echo_html", echo_html))
    logger.debug("Starting bot...")
    app.run_polling()


if __name__ == "__main__":
    main()
