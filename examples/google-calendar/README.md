# google-calendar

A maintained connector for one Google calendar, reached through a
connected account: what is on today and next, free time on this calendar
alone, and events created, changed and deleted — each write proposed by
the identity and run once the operator confirms it.

    aiisdk build && aiisdk test
    go test ./examples/google-calendar/

`aiisdk test` proves the guest without the network: the handle refused by
name, the denials without a grant, every write refused until the operator
confirms it, a time the clock skips and a zone no one keeps refused before
any call. The Go test drives the same code over the native lane against a
scripted Google — the answers the Calendar API reference documents for
each operation, a lost answer, a process killed in the middle of a create
— which is local proof, not the live journey.

## The reads

**calendar.today** reads the calendar's whole day, midnight to midnight in
the calendar's own time zone, which it asks Google for first. A day the
clocks change is 23 or 25 hours; it is never a UTC day or the host's. The
answer carries Google's events (`events.items`, with each one's `etag`),
`time_zone`, `day_start` and `day_end`. Until 0.2.0 it read from now to
midnight UTC, fifty events at most, with no word when there were more.

**calendar.upcoming** reads the next few events (`limit`, 5 by default,
at most 50) from the host's now.

Both read page after page — Google may return fewer events than asked
while more remain — up to a bound (300 events for a day). Past it the
answer says `truncated` and carries `next_page`; passing it back as `page`
reads the rest of that same query.

**calendar.availability** answers free time between two wall-clock times
in the calendar's zone (`from`, `to`, at most 14 days apart) of at least
`minutes`: the window less what this calendar's events hold. An event
shown as free, one its owner declined and a cancelled one hold nothing; a
busy all-day event holds its day. It reads this calendar and nothing
else, so it says nothing about any other person's time or whether they
would accept — every answer says so in `scope`. Past its bound it claims
free time only up to the last event it read and says from when to ask
again.

## The writes

Each declares `operator_confirms`: the identity's call proposes it, the
host shows the operator the arguments on the Plugins page, and the
operation runs once, stamped, when the operator confirms. A call without
the stamp is refused before anything is asked of anyone. Each names
`notify` — `none` (the safe choice), `externalOnly` or `all` — so the card
always shows whom Google will email, and the answer says what Google was
asked to send. No write is declared idempotent to the broker, which would
send it again on a transport failure: a second change or cancellation can
email every guest again. The card also offers Always: an operator who chooses
it for one of these lets every later call of it run at once on the identity's
word — a create, a change or a cancellation that emails its guests — until
they revoke it on the Plugins page.

**calendar.create** takes `title`, `start` and `end` as wall-clock times
(`2026-10-02T10:00`) in `time_zone`, and optionally `location`,
`description` and `attendees`. It creates timed events only. The event's
id is derived from the intended event, so the same create asked again —
after a lost answer, a restart, a second proposal — names the same event
and needs no storage: the base32hex of SHA-256 over
`"24:google-calendar create 1;"` followed by the calendar, the start and
end instants in UTC (RFC 3339), the title and the `distinct` word, each
as `<length in bytes>:<value>;` — 52 characters. On Google's 409 the
event is read back, and the create claims it (`created_earlier`) only
when Google holds exactly this event, not cancelled: a deleted event
keeps its id, and a different one is refused — never taken for this one.
`distinct` makes a second event with the same title and times, or one
again after a delete. Google itself warns that it "cannot guarantee that
ID collisions will be detected at event creation time"; the derived id
makes a second event unlikely, not impossible.

**calendar.update** changes the fields it names — a new `title`, `start`
with `end` and `time_zone`, `location`, `description` — and nothing else.
It names the event by `event_id` and `event_title`, both as a read gave
them, and is bound to `version`, the event's etag from that read. It reads
the event first: a later version, a different title or a deleted event
stops it with what the event is now. The write itself carries
`If-Match: <version>`, so a change made between that read and the write
is Google's 412, read again and stopped — never an overwrite. It never
changes the guest list.

**calendar.delete** names the event by `event_id` and `event_title`. It
reads the event first: one already deleted is answered as
`already_deleted` and nothing is deleted; an id Google never held, or a
title that is not the event's, is refused. After Google takes the delete,
a read confirms it: cancelled, 404 or 410 is `confirmed`; a read that
fails proves nothing, and the answer says the delete was taken and not
confirmed, and why.

An update and a delete act on one event: the reads name each occurrence
of a repeating event by its own id, and the id of a whole series is
refused, since a write to it would reach every occurrence while the
operator's card shows one title.

## What the answers mean

Every write answers in the host's codes (WRITING_A_PLUGIN.md §11): a
receipt when Google did it; `NET_EFFECT_UNKNOWN` when it may have (a 5xx,
a lost answer, an answer that cannot be read) — never repeated, and
asking again finds out rather than doing it twice; `NET_REMOTE_FAILED`
when Google accepted nothing (a 429 with its wait, another 4xx, not
reached); `OPERATION_ARGUMENT_INVALID` when these arguments will not be
carried out (a 400, a stale version, an id held by another event). The
host's own refusals are relayed with their codes:
`NET_AUTH_PROFILE_DISCONNECTED` means the operator connects the Google
account again on the Plugins page, `NET_AUTH_PROFILE_UNAVAILABLE` that a
refresh failed just now. A failed read answers `NET_REMOTE_FAILED`: a
read has no effect to be unsure of.

## On an identity

Install at T2 (the credential floor). On the Plugins page, create a
profile for Google with the calendar service — read for the reads, modify
to create, change and delete — and connect it in your browser, or answer
the identity's connect card, which opens that form prefilled from this
plugin's own hint. Grant `net.outbound:www.googleapis.com:443` and the
profile's name as a credential handle in `plugins.grants.<id>`, and set
the `google` setting to the profile's name. The host refreshes the token
and injects it at the wire; this code never sees it. A Google project in
Testing status expires its refresh tokens after seven days, so reconnect
weekly until the project is verified.
