> **The main repository of this project lives at https://code.pecheny.me/pecheny/rating_chgk_info_helper_bot. All issues should be created there.**

A Telegram bot for organisers of ЧГК tournaments on rating.chgk.info. See [CONTEXT.md](./CONTEXT.md) for what the words mean.

## Running

The working directory holds `config.json`, `token` and the two databases.

```
go build -o rating_bot ./cmd/rating-bot
./rating_bot                                  # live bot, jobs enabled
./rating_bot --debug --token_path token_test  # test bot, no scheduled jobs
go test ./...
```

## Deploying

The binary is static and cross-compiled, so the vps never needs a Go toolchain
and never spends its memory compiling.

```
./deploy/build.sh                    # build for linux/amd64, copy, restart
```

The unit is [deploy/rating_bot.service](./deploy/rating_bot.service); it passes
`--data-dir` explicitly rather than relying on the working directory.

## Logs

`logs.db` holds message traffic and the application log, pruned nightly to 100MB. Long-poll calls are not recorded.

```sql
select datetime(ts,'unixepoch','localtime'), direction, chat_id, text from traffic order by ts desc limit 20;
select datetime(ts,'unixepoch','localtime'), level, message from app_log where level = 'ERROR' order by ts desc limit 20;
```

Console output goes to journald: `journalctl -u rating_bot -f`.

## Layout

| Package | What lives there |
| --- | --- |
| `cmd/rating-bot` | flags, startup, shutdown |
| `internal/tg` | update routing, the commands, the announce relay, the two jobs |
| `internal/store` | `bot.db` and `logs.db`: subscriptions, conversation state, traffic |
| `internal/rating` | the rating.chgk.info API, reminders and podiums |
| `internal/dates` | parsing the site's timestamps, generating a tournament's date grid |
| `internal/text` | wording, Russian plurals, and splitting a message to Telegram's limit |
