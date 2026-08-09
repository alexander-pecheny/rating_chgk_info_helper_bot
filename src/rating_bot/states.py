from aiogram.fsm.state import State, StatesGroup


class Flows(StatesGroup):
    subscribe = State()
    subscribe_izh = State()
    unsubscribe = State()
    top3 = State()
    host = State()
    dates_prefs = State()
    announce = State()
    dates_start = State()
    dates_sync_days = State()
    dates_async_days = State()
