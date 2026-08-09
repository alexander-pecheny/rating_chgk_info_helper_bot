import datetime
import itertools
import json
import re
import urllib

import msgpack

from rating_bot.dateutil import UTC_PLUS_3, tryint

START = """\
Привет! Это бот-помощник для турнирного сайта.

Он умеет оповещать о новых заявках на турниры.

Чтобы подписаться на обновления, напиши <code>/subscribe</code> и id турниров через запятую, вот так:

<code>/subscribe 7000, 9002, 9015</code>

Чтобы отписаться, то же самое, но с командой <code>/unsubscribe</code>.
"""
ID_TOURNS_TEXT = "Пожалуйста, укажите id турниров через запятую или /cancel для отмены."
ID_TOURN_TEXT = "Пожалуйста, укажите id турнира или /cancel для отмены."
DEFAULT_HOST = "rating.chgk.info"
RE_HOST = re.compile("[a-z][a-z\\.]+[a-z]")


ChatPrefsType = dict[int, dict[str, int]]


def default_subscription() -> dict[str, int]:
    return {"r": 1, "i": 1, "a": 1}


def parse_chat_ids(chat_ids_str: str) -> ChatPrefsType:
    if not chat_ids_str:
        return {}
    return msgpack.loads(chat_ids_str, strict_map_key=False)


def serialize_chat_ids(chat_ids: ChatPrefsType) -> str:
    return msgpack.dumps(chat_ids)


def get_application_form(x: int):
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


def wrap_link(s, host):
    id_ = s.split()[0]
    return f"""<a href="https://{host}/player/{id_}">{s}</a>"""


def tourn_wrap_link(s, host):
    id_ = s.split()[0]
    return f"""<a href="https://{host}/tournament/{id_}">{s}</a>"""


def get_sorting_key(x):
    sp = x.split()
    return (sp[1], sp[2], sp[0])


def format_applications(applications, host):
    srt = sorted([applications[x]["rep"] for x in applications], key=get_sorting_key)
    return "\n".join([wrap_link(rep, host) for rep in srt])


def get_open_tags(text):
    """Returns a list of currently open HTML tags in order."""
    open_tags = []
    tag_pattern = re.compile(r"<(/?)(\w+)(?:\s[^>]*)?>")
    for match in tag_pattern.finditer(text):
        is_closing = match.group(1) == "/"
        tag_name = match.group(2).lower()
        if is_closing:
            if open_tags and open_tags[-1] == tag_name:
                open_tags.pop()
        else:
            open_tags.append(tag_name)
    return open_tags


def close_tags(tags):
    """Generate closing tags in reverse order."""
    return "".join(f"</{tag}>" for tag in reversed(tags))


def open_tags(tags):
    """Generate opening tags in order."""
    return "".join(f"<{tag}>" for tag in tags)


def find_safe_split_point(text, max_len):
    """Find a safe point to split text, preferring newlines and avoiding tag breaks."""
    if len(text) <= max_len:
        return len(text)

    search_area = text[:max_len]

    # First try to find double newline (paragraph break)
    last_para = search_area.rfind("\n\n")
    if last_para > max_len // 2:
        return last_para + 2

    # Then try single newline
    last_newline = search_area.rfind("\n")
    if last_newline > max_len // 2:
        return last_newline + 1

    # Avoid splitting inside an HTML tag
    # Find last '>' that's not inside an unclosed '<'
    last_tag_end = search_area.rfind(">")
    last_tag_start = search_area.rfind("<")

    if last_tag_start > last_tag_end:
        # We're inside a tag, back up to before it
        return last_tag_start

    return max_len


def get_batches(res, max_len=2048):
    batches = []
    current_open_tags = []

    while res:
        if len(res) <= max_len:
            batches.append(open_tags(current_open_tags) + res)
            break

        # Account for tag overhead in this batch
        prefix = open_tags(current_open_tags)
        available_len = max_len - len(prefix)

        split_point = find_safe_split_point(res, available_len)
        batch_content = res[:split_point]

        # Find what tags are open at the end of this batch
        combined = prefix + batch_content
        tags_at_end = get_open_tags(combined)

        # Close any open tags at the end of this batch
        batch = combined + close_tags(tags_at_end)
        batches.append(batch)

        # Remember open tags for next batch
        current_open_tags = tags_at_end
        res = res[split_point:].lstrip("\n")

    return batches


def udumps(s):
    return json.dumps(s, ensure_ascii=False)


def get_next_reminder_job_time():
    now = datetime.datetime.now(UTC_PLUS_3)
    target = now.replace(hour=15, minute=30, second=0, microsecond=0)
    if target < now:
        target += datetime.timedelta(days=1)
    return target
