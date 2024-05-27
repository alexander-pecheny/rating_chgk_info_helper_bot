from typing import NamedTuple
import sqlite3
from helpers import serialize_chat_ids, default_subscription

def parse_chat_ids_old(s):
    if not s:
        return []
    sp = s.split(",")
    return [int(x) for x in sp]

class Row(NamedTuple):
    id: int
    name: str
    state: str
    chat_ids: str

if __name__ == "__main__":
    conn = sqlite3.connect("bot.db")
    cur = conn.cursor()
    subscriptions = cur.execute("select id, name, state, chat_ids from data;").fetchall()
    for tup in subscriptions:
        row = Row(*tup)
        parsed = parse_chat_ids_old(row.chat_ids)
        new_chat_ids = {n: default_subscription() for n in parsed}
        cur.execute("update data set chat_ids = ? where id = ?", (serialize_chat_ids(new_chat_ids), row.id))
    conn.commit()
    print("successfully updated db!")