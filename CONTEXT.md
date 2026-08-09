# Tournament Organiser Helper

A Telegram bot for organisers of ЧГК tournaments listed on rating.chgk.info. It watches tournaments they care about, tells them when something needs their attention, and relays their announcements to a shared channel.

## Language

**Tournament**:
A competition listed on rating.chgk.info, identified by its numeric site id.

**Application**:
A team's submission asking to play a tournament, which an organiser then reviews.
_Avoid_: Request, Entry. The rating.chgk.info API calls this a "request", so code that touches the API says so at the boundary and nowhere else.

**Subscription**:
A chat's standing interest in one tournament, covering some or all of the three notification kinds below.

**Controversial**:
A disputed answer on a played tournament, resolved by the game jury (ИЖ) within six days of the tournament ending.

**Appeal**:
A formal challenge to a ruling on a played tournament, resolved by the appeals jury (АЖ) within sixteen days of the tournament ending.

**Reminder**:
A message warning a subscriber that a controversials or appeals deadline is approaching.

**Announce**:
An announcement an organiser submits to the bot, which the bot relays verbatim into the announce channel. Only the organiser writes it; the bot never composes one.

**Announce Channel**:
The single Telegram channel every announce is relayed into.

**Host**:
Which rating site a subscriber wants their links to point at — the main site or a mirror.
