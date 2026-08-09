# Rewrite the bot in Go

On 2026-08-09 this bot was the second-largest process on vps2day-ee, at 143MB PSS out of the box's 960MB, behind only caddy. Measurement placed almost all of it in one line: importing `aiogram.types` takes a Python process from 11MB to about 156MB, because aiogram builds pydantic core schemas for roughly a thousand Bot API types at import time. That is a fixed cost, not a leak — Python 3.12, 3.13 and 3.14 all paid it, and no aiogram model was holding a namespace that could be released.

The options were more RAM, a hand-written dict-based Telegram client keeping the Python, or a rewrite. We rewrote, because the memory is not the only thing that gets cheaper: the scheduler, the FSM storage and the conversation plumbing were all bespoke already, and none of them survive as dependencies here.

The bot keeps both sqlite files exactly as they were, down to the msgpack encoding of the `chat_ids` column and the aiogram state names in `fsm_state`, so the switch is a binary swap with no migration and no lost conversation.

## The cost, which is the same one ADR 0001 was about

[ADR 0001](./0001-aiogram-over-python-telegram-bot.md) chose aiogram because python-telegram-bot was two months behind on Bot API 10.1 and a relay bot cannot be the last to learn what a message is. go-telegram/bot is current — it shipped 10.2 — but its `RichBlock` and `RichText` are closed unions: an unrecognised `type` makes `UnmarshalJSON` return an error rather than degrade.

That is worse here than being a release behind. `getUpdates` decodes a whole batch at once and only advances its offset after the batch decodes, so one message containing one unknown block type would make the bot refetch that batch forever and answer nobody, until someone noticed and deleted the message.

So the bot does not trust the library with raw updates. `internal/tg/updates.go` wraps the HTTP client, and any update that fails to parse has its `rich_message` replaced by the flattened text of its blocks — which is all any handler here reads, since an announcement is relayed by message id rather than by content. A future block type costs the announcement its formatting in the log, and nothing else.
