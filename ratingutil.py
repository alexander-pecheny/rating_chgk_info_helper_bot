import httpx
import logging
import time
import sys
from dateutil import DT

logger = logging.getLogger("rating_bot")

API = "https://api.rating.chgk.net"


def _get_results(tourn_id):
    res = httpx.get(
        f"{API}/tournaments/{tourn_id}"
        + "/results.json?pagination=false&includeTeamMembers=1"
        + "&includeTeamFlags=1&includeMasksAndControversials=1"
    ).json()
    time.sleep(0.5)
    return res


def _get_appeals(tourn_id):
    res = httpx.get(
        f"{API}/tournaments/{tourn_id}/appeals.json?pagination=false"
    ).json()
    time.sleep(0.5)
    return res


def _get_info(tourn_id):
    return httpx.get(f"{API}/tournaments/{tourn_id}.json").json()


def convert_request_info(request):
    rep = request["representative"]
    town = request["venue"]["town"]["name"]
    return {
        "status": request["status"],
        "rep": f"{rep['id']} {rep['name']} {rep['surname']} ({town})",
    }


def _get_requests(t_id, only_new=True):
    logger.debug(f"getting requests from {t_id}...")
    req = httpx.get(f"{API}/tournaments/{t_id}/requests.json?pagination=false")
    time.sleep(0.5)
    if req.status_code != 200:
        sys.stderr.write(f"got response with error {req.status_code}: {req.text}\n")
        return
    obj = req.json()
    converted = {str(r["id"]): convert_request_info(r) for r in obj}
    if only_new:
        converted = {k: v for k, v in converted.items() if v["status"] == "N"}
    return converted


def can_check_controversials(info, now):
    hide_questions = DT(info["synchData"]["hideQuestionsTo"])
    return hide_questions < now


def generate_init_message(info, end_date):
    tourn_name = f"<b>{info['id']} {info['name']}</b>"
    return f"Крайний срок рассмотрения спорных на турнире {tourn_name} — {end_date.plus_days(6)}, апелляций — {end_date.plus_days(16)}"


def generate_controversials_reminder(info, now):
    tourn_name = f"<b>{info['id']} {info['name']}</b>"
    return f"""Спорные на турнире {tourn_name} должны быть <a href="https://rating.chgk.info/{info['id']}/controversials">рассмотрены</a> до конца сегодняшнего дня."""


def generate_controversials_reminder_exact(info, now):
    tourn_name = f"<b>{info['id']} {info['name']}</b>"
    new_controversials = 0
    results = _get_results(info["id"])
    for res in results:
        for controversial in res["controversials"]:
            if controversial["status"] == "N":
                new_controversials += 1
    if new_controversials:
        return f"""На турнире {tourn_name} {new_controversials} нерассмотренных спорных. <a href="https://rating.chgk.info/{info['id']}/controversials">Рассмотреть</a>"""


def generate_appeals_reminder(info, now):
    tourn_name = f"<b>{info['id']} {info['name']}</b>"
    return f"""Апелляции на турнире {tourn_name} должны быть <a href="https://rating.chgk.info/{info['id']}/appeals">рассмотрены</a> до конца сегодняшнего дня."""


def generate_appeals_reminder_exact(info, now):
    tourn_name = f"<b>{info['id']} {info['name']}</b>"
    appeals = _get_appeals(info["id"])
    new_appeals = len([a for a in appeals if a["status"] == "N"])
    if new_appeals:
        return f"""На турнире {tourn_name} {new_appeals} нерассмотренных апелляций. <a href="https://rating.chgk.info/{info['id']}/appeals">Рассмотреть</a>"""


def tourn_info_to_reminders(info, now: DT):
    curr_date = now.dt.date()
    end_date = DT(info["dateEnd"]).dt.date()
    can_check = can_check_controversials(info, now)
    messages = []
    delta = (curr_date - end_date).days
    if delta == 1:
        messages.append(generate_init_message(info, end_date))
    if can_check:
        if delta >= 6:
            controversials = generate_controversials_reminder_exact(info)
            if controversials:
                messages.append(controversials)
        if delta >= 16:
            appeals = generate_appeals_reminder_exact(info["id"])
            if controversials:
                messages.append(appeals)
    else:
        if delta == 6:
            messages.append(generate_controversials_reminder(info, now))
        elif delta == 16:
            messages.append(generate_appeals_reminder(info, now))
    return "\n\n".join(messages)


def format_place(place):
    if place[0] == place[1]:
        return str(place[0])
    return f"{place[0]}–{place[1]}"


def get_recaps(res):
    result = []
    for pl in sorted(
        res["teamMembers"], key=lambda x: (x["player"]["surname"], x["player"]["name"])
    ):
        result.append(f"{pl['player']['name']} {pl['player']['surname']}")
    return ", ".join(result)


def get_top3_by_flag(results, flag=None):
    teams = []
    if flag:
        results = [r for r in results if flag in {x["shortName"] for x in r["flags"]}]
    for i, res in enumerate(results):
        teams.append(res)
        if (
            len(teams) >= 3
            and (i + 1) < len(results)
            and results[i + 1]["questionsTotal"] < res["questionsTotal"]
        ):
            break
    pointslist = [x["questionsTotal"] for x in teams]
    result = []
    for t in teams:
        place = [
            pointslist.index(t["questionsTotal"]) + 1,
            len(pointslist) - pointslist[::-1].index(t["questionsTotal"]),
        ]
        recaps = f" ({get_recaps(t)})"
        result.append(
            f"{format_place(place)}. {t['current']['name']} ({t['current']['town']['name']}){recaps} — {t['questionsTotal']}"
        )
    return "\n".join(result)


def get_flags(results):
    result = set()
    for res in results:
        for flag in res["flags"]:
            result.add(flag["shortName"])
    return result


def get_tourn_top3(tourn_id: int) -> str:
    results = _get_results(tourn_id)
    try:
        results = sorted(results, key=lambda x: x["questionsTotal"], reverse=True)
    except TypeError:
        return "Результаты пока недоступны или доступны не полностью."
    flags = get_flags(results)
    result = ["<b>Топ-3 в общем зачёте</b>", get_top3_by_flag(results)]
    for flag in sorted(flags):
        result.append(f"<b>Топ-3 по флагу {flag}</b>")
        result.append(get_top3_by_flag(results, flag))
    return "\n".join(result)
