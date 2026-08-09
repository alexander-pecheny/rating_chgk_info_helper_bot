from rating_bot.handlers import admin, announce, common, prefs, tournaments

# /start and /cancel first, so a conversation can never swallow them.
ROUTERS = [
    common.router,
    announce.router,
    tournaments.router,
    prefs.router,
    admin.router,
]

__all__ = ["ROUTERS", "admin", "announce", "common", "prefs", "tournaments"]
