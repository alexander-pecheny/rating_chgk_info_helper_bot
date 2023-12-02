import httpx

API = "https://api.rating.chgk.net/tournaments/{tourn_id}/results.json?pagination=false&includeTeamMembers=1&includeTeamFlags=1"


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
    for i, res in enumerate(results):
        if flag is not None and flag not in {x["shortName"] for x in res["flags"]}:
            continue
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
    results = httpx.get(API.format(tourn_id=tourn_id)).json()
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
