# Use aiogram rather than python-telegram-bot

Telegram shipped rich messages in Bot API 10.1 on 11 June 2026, and organisers immediately began composing announcements in the new post editor. Such a message carries no `text` and no `caption` — its whole body lives in `rich_message.blocks` — so the bot could not read it and refused every one.

python-telegram-bot 22.8 implements Bot API 10.0. Two months after 10.1 its tracking issue was still open with an untouched checklist, `rich_message` was absent even from `master`, and no prerelease existed. aiogram shipped 10.1 three days after Telegram published it, and 10.2 a month later. Since this bot's whole job is relaying whatever organisers send, being a release behind on message types is not a cosmetic lag — it is the bot being broken.

We could have read `rich_message` out of PTB's `api_kwargs` in about ten lines and stayed put. We migrated instead because the same gap will reopen at Bot API 10.3, and a relay bot cannot afford to be the last to learn what a message is.

The cost was real and is worth remembering: `ConversationHandler` has no aiogram equivalent, so the eight conversation flows became FSM states over a sqlite storage written for this repo, and `JobQueue` became APScheduler directly.
