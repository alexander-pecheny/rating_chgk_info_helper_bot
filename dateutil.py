from typing import Optional
from dataclasses import dataclass
import datetime


class DT:
    day_to_n = {
        "mon": 0,
        "tue": 1,
        "wed": 2,
        "thu": 3,
        "fri": 4,
        "sat": 5,
        "sun": 6,
    }
    n_to_day = {v: k for k, v in day_to_n.items()}
    n_to_day_ru = {
        0: "понедельник",
        1: "вторник",
        2: "среда",
        3: "четверг",
        4: "пятница",
        5: "суббота",
        6: "воскресенье",
    }
    n_to_day_ru_genitive = {
        0: "понедельника",
        1: "вторника",
        2: "среды",
        3: "четверга",
        4: "пятницы",
        5: "субботы",
        6: "воскресенья",
    }
    month_to_hr = {
        1: "января",
        2: "февраля",
        3: "марта",
        4: "апреля",
        5: "мая",
        6: "июня",
        7: "июля",
        8: "августа",
        9: "сентября",
        10: "октября",
        11: "ноября",
        12: "декабря",
    }
    utc_plus_3_secs = 10800
    utc_plus_3 = datetime.timezone(datetime.timedelta(seconds=utc_plus_3_secs))

    def __init__(self, *args, **kwargs):
        if len(args) == 1 and isinstance(args[0], str):
            for f_str in (
                "%Y-%m-%d",
                "%Y-%m-%dT%H:%M:%S",
                "%Y-%m-%dT%H:%M:%S%z",
                "%Y-%m-%d %H:%M",
            ):
                try:
                    self.dt = datetime.datetime.strptime(args[0], f_str)
                    break
                except ValueError:
                    continue
        elif len(args) == 2 and isinstance(args[0], str) and isinstance(args[1], str):
            self.dt = datetime.datetime.strptime(args[0], args[1])
        elif isinstance(args[0], datetime.datetime):
            self.dt = args[0]
        elif isinstance(args[0], DT):
            self.dt = datetime.datetime(
                args[0].dt.year, args[0].dt.month, args[0].dt.day, 0, 0, 0
            )
        elif isinstance(args[0], datetime.date):
            self.dt = datetime.datetime(
                args[0].year, args[0].month, args[0].day, 0, 0, 0
            )
        else:
            raise ValueError
        if not getattr(self.dt, "tzinfo", None):
            self.dt = self.dt.replace(tzinfo=self.utc_plus_3)
        if self.dt.tzinfo.utcoffset(self.dt).seconds != self.utc_plus_3_secs:
            self.dt = self.dt.astimezone(self.utc_plus_3)

    def __str__(self) -> str:
        return self.hr()

    def __repr__(self) -> str:
        return self.dt_plain()

    def __eq__(self, other):
        if isinstance(other, DT):
            return self.dt == other.dt
        elif isinstance(other, datetime.datetime):
            return self.dt == other
        raise TypeError

    def __gt__(self, other):
        if isinstance(other, DT):
            return self.dt > other.dt
        elif isinstance(other, datetime.datetime):
            return self.dt > other
        raise TypeError

    def __lt__(self, other):
        if isinstance(other, DT):
            return self.dt < other.dt
        elif isinstance(other, datetime.datetime):
            return self.dt < other
        raise TypeError

    def plus_days(self, n: int):
        return DT(self.dt + datetime.timedelta(days=n))

    def plus_minutes(self, n: int):
        return DT(self.dt + datetime.timedelta(minutes=n))

    def plus_hours(self, h: int):
        return DT(self.dt + datetime.timedelta(hours=h))

    def date(self) -> datetime.date:
        return self.dt.date()

    def day(self) -> int:
        return self.dt.day

    def wday(self, type_="en_short") -> str:
        dict_ = {
            "en_short": self.n_to_day,
            "ru_nom": self.n_to_day_ru,
            "ru_gen": self.n_to_day_ru_genitive,
        }[type_]
        return dict_[self.dt.weekday()]

    def gen(self) -> str:
        return self.wday("ru_gen")

    def nom(self) -> str:
        return self.wday("ru_nom")

    def hour(self) -> int:
        return self.dt.hour

    def replace(self, **kwargs):
        return DT(self.dt.replace(**kwargs))

    def time(self) -> str:
        return f"{str(self.dt.hour).zfill(2)}:{str(self.dt.minute).zfill(2)}"

    def next_weekday(self, weekday, reverse=False, skip=0):
        assert skip >= 0
        delta = datetime.timedelta(days=-1 if reverse else 1)
        mover = self.dt + delta
        target_weekday = self.day_to_n[weekday]
        while not (mover.weekday() == target_weekday):
            mover += delta
        return DT(mover + datetime.timedelta(days=skip * 7 * delta.days))

    def prev_weekday(self, weekday, skip=0):
        return self.next_weekday(weekday, reverse=True, skip=skip)

    def hr(self) -> str:
        return f"{self.dt.day} {self.month_to_hr[self.dt.month]}"

    def hr_with_year(self) -> str:
        return f"{self.hr()} {self.dt.dt.year} года"

    def dt_hr(self) -> str:
        return f"{self.dt.hour}:{self.dt.minute} {self.hr()}"

    def plain(self) -> str:
        return self.dt.strftime("%Y-%m-%d")

    def dt_plain(self) -> str:
        return self.dt.strftime("%Y-%m-%d %H:%M:%S")

    def wrap_late_date(self):
        if self.hour() <= 12:
            return self.plus_days(-1).replace(hour=18, minute=0)
        return self

    def wrap_early_date(self):
        if self.hour() >= 18:
            return self.plus_days(1).replace(hour=11, minute=0)
        return self


def hr_range(date1: DT, date2: DT) -> str:
    assert date2 > date1
    if date1.dt.month == date2.dt.month:
        return f"с {date1.dt.day} по {date2.dt.day} {DT.month_to_hr[date1.dt.month]}"
    elif date1.dt.year != date2.dt.year:
        return f"с {date1.hr_with_year()} по {date2.hr_with_year()}"
    else:
        return f"с {date1.hr()} по {date2.hr()}"


def parse_date_or_return(s):
    try:
        return DT(datetime.datetime.strptime(s, "%Y-%m-%d"))
    except:
        return s


@dataclass
class TournamentData:
    dateStart: Optional[DT] = None
    dateEnd: Optional[DT] = None
    dateRequestsAllowedTo: Optional[DT] = None
    dateDownloadQuestionsFrom: Optional[DT] = None
    resultsRecapsTo: Optional[DT] = None
    hideResultsTo: Optional[DT] = None
    hideQuestionsTo: Optional[DT] = None
    dateAppealAllowedTo: Optional[DT] = None
    resultFixesTo: Optional[DT] = None


def format_dates(sync_data: TournamentData) -> str:
    result = []
    result.append(f"Начало: {sync_data.dateStart.dt_plain()}")
    result.append(f"Конец: {sync_data.dateEnd.dt_plain()}")
    result.append(f"Приём заявок до: {sync_data.dateRequestsAllowedTo.dt_plain()}")
    result.append(
        f"Скачивание пакетов ведущими с: {sync_data.dateDownloadQuestionsFrom.dt_plain()}"
    )
    result.append(
        f"Приём составов команд и результатов до: {sync_data.resultsRecapsTo.dt_plain()}"
    )
    result.append(f"Результаты скрыты до: {sync_data.hideResultsTo.dt_plain()}")
    result.append(f"Срок незасветки пакета до: {sync_data.hideQuestionsTo.dt_plain()}")
    result.append(f"Приём апелляций до: {sync_data.dateAppealAllowedTo.dt_plain()}")
    result.append(
        f"Корректировка результатов представителями до: {sync_data.resultFixesTo.dt_plain()}"
    )
    return result


StrOrDate = str | datetime.datetime


def generate_dates(
    datetime_sync_start: StrOrDate, sync_days: int, async_days: int
) -> str:
    try:
        if isinstance(datetime_sync_start, datetime.datetime):
            dt_sync_start = datetime_sync_start
        elif " " in datetime_sync_start:
            dt_sync_start = datetime.datetime.strptime(
                datetime_sync_start, "%Y-%m-%d %H:%M"
            )
        else:
            dt_sync_start = datetime.datetime.strptime(
                datetime_sync_start + " 11:00", "%Y-%m-%d %H:%M"
            )
    except:
        return "Не удалось распарсить дату. Правильный формат: <pre>2023-01-31</pre> или <pre>2023-01-31 11:00</pre>"
    if sync_days < 1 or sync_days > 7:
        return "Дней синхрона должно быть от 1 до 7"
    synch = TournamentData()
    synch.dateStart = DT(dt_sync_start)
    if (
        synch.dateStart.dt.hour == 11
        and synch.dateStart.dt.minute == 0
        and sync_days == 7
    ):
        synch.dateEnd = synch.dateStart.plus_days(6).replace(hour=23, minute=59)
    else:
        synch.dateEnd = synch.dateStart.plus_days(sync_days)
    synch.dateRequestsAllowedTo = synch.dateEnd
    synch.dateDownloadQuestionsFrom = synch.dateStart.plus_days(-1)
    synch.resultsRecapsTo = synch.dateEnd.plus_days(3)
    synch.dateAppealAllowedTo = synch.dateEnd.plus_days(9)
    synch.resultFixesTo = synch.dateEnd.plus_days(19)
    if async_days:
        asynch = TournamentData()
        asynch.dateStart = synch.dateEnd.plus_days(1).replace(hour=0, minute=1)
        asynch.dateEnd = asynch.dateStart.plus_days(async_days)
        asynch.dateRequestsAllowedTo = asynch.dateEnd
        asynch.dateDownloadQuestionsFrom = asynch.dateStart.plus_days(-1)
        asynch.resultsRecapsTo = asynch.dateEnd.plus_days(30)
        asynch.dateAppealAllowedTo = asynch.dateEnd.plus_days(7)
        asynch.resultFixesTo = asynch.dateEnd.plus_days(30)
        asynch.hideResultsTo = asynch.dateEnd
        asynch.hideQuestionsTo = asynch.dateEnd
        if async_days < 14:  # deadline imposed by https://www.maii.li/p/aegis-rating
            synch.hideResultsTo = asynch.dateEnd
        else:
            synch.hideResultsTo = synch.dateEnd.plus_days(14)
        synch.hideQuestionsTo = asynch.dateEnd
    else:
        synch.hideResultsTo = synch.dateEnd
        synch.hideQuestionsTo = synch.dateEnd
    result = ["<b>Синхрон:</b>"]
    result.extend(format_dates(synch))
    if async_days:
        result.extend(
            [
                "В синхроне не забудьте поставить галочку «Сразу показывать спорные, апелляции и расплюсовку всем причастным».",
                "",
                "<b>Асинхрон:</b>",
            ]
        )
        result.extend(format_dates(asynch))
    return "\n".join(result)
