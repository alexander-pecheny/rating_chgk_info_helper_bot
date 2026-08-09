> **The main repository of this project lives at https://code.pecheny.me/pecheny/rating_chgk_info_helper_bot. All issues should be created there.**

A Telegram bot for organisers of ЧГК tournaments on rating.chgk.info. See [CONTEXT.md](./CONTEXT.md) for what the words mean.

## Running

The working directory holds `config.json`, `token` and the two databases; the code lives in `src/`.

```
uv sync
uv run rating-bot                          # live bot, jobs enabled
uv run rating-bot --debug --token_path token_test   # test bot, no scheduled jobs
```

## Deploying

```
git pull && uv sync && sudo systemctl restart rating_bot
```

## Logs

`logs.db` holds message traffic and the application log, pruned nightly to 100MB. Long-poll calls are not recorded.

```sql
select datetime(ts,'unixepoch','localtime'), direction, chat_id, text from traffic order by ts desc limit 20;
select datetime(ts,'unixepoch','localtime'), level, message from app_log where level = 'ERROR' order by ts desc limit 20;
```

Console output goes to journald: `journalctl -u rating_bot -f`.
