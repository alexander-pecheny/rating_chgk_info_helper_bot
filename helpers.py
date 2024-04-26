import json
import datetime
import itertools
import re
import urllib

from dateutil import UTC_PLUS_3, tryint

API = "https://api.rating.chgk.net"
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
DEFAULT_HOST = "rating.chgk.info"
RE_HOST = re.compile("[a-z][a-z\\.]+[a-z]")


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


def get_list_of_ints(str_):
    sp1 = str_.split(",")
    return [
        tryint(x)
        for x in itertools.chain.from_iterable(x.split() for x in sp1)
        if tryint(x)
    ]


def strip_host(host):
    host = (host or "").strip()
    for prefix in ("http://", "https://", "www."):
        if host.startswith(prefix):
            host = host[len(prefix) :]
    host = urllib.parse.urlparse("https://" + host).netloc
    return host


def validate_host(host):
    return host and RE_HOST.search(host) and "." in host


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


def get_batches(res):
    batches = []
    while len(res) >= 2048:
        batch, res = res[:2047], res[2047:]
        batches.append(batch)
    batches.append(res)
    return batches


def udumps(s):
    return json.dumps(s, ensure_ascii=False)


def is_forward(message):
    result = False
    try:
        if message.forward_origin:
            result = True
    except AttributeError:
        pass
    try:
        if message.forward_from_chat:
            result = True
    except AttributeError:
        pass
    try:
        if message.forward_from:
            result = True
    except AttributeError:
        pass
    return result


def get_next_reminder_job_time():
    now = datetime.datetime.now(UTC_PLUS_3)
    target = now.replace(hour=15, minute=30, second=0, microsecond=0)
    if target < now:
        target += datetime.timedelta(days=1)
    return target
