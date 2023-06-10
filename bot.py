#!/usr/bin/env python
# -*- coding: utf-8 -*-
import os
import argparse
import sqlite3

DB_LOC = os.path.join(os.path.dirname(os.path.abspath(__file__), "bot.db"))
DB_INIT = """\
CREATE TABLE IF NOT EXISTS data (
    id integer PRIMARY KEY,
    state text,
    chat_ids text
);
"""

def db_init():
    if not os.path.isfile(DB_LOC):
        conn = sqlite3.connect(DB_LOC)
        cur = conn.cursor()
        cur.execute(DB_INIT)
        conn.commit()



def main():
    db_init()


if __name__ == "__main__":
    main()
