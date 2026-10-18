# gmail

A maintained connector for one Gmail mailbox, reached through a connected
account: search it, read one message, save a draft that reaches no one,
and send a mail once the operator confirms it — and the inbox as a channel:
mail reaches the identity as it arrives, and the identity mails a contact
without a card. Each answer says what Gmail did and what it did not.

    aiisdk build && aiisdk test
    go test ./examples/gmail/

`aiisdk test` proves the guest without the network: the handle refused by
name, the denials without a grant, a draft that needs no operator, a send
refused until the operator confirms it, and headers the operator's card
could not show refused before any call. The Go test drives the same code
over the native lane against a scripted Gmail — the answers the Gmail API
reference documents, a lost answer, a process killed in the middle of a
send, a draft Gmail deletes when it is sent, a Message-ID Gmail writes
over — which is
local proof, not the live journey.

## The reads

**gmail.search** takes Gmail's own search words (`query`: `from:`,
`to:`, `subject:`, `after:2026/09/01`, `in:sent`, `is:unread`, a phrase
in quotes) and answers one page of the matches, newest first: each one's
`message_id`, `thread_id`, `from`, `to`, `subject`, `date`, `snippet` and
`labels`. Gmail's list names only ids, so every message is read on its
own, and a page is at most ten (`max`), which with each call's bound fits
the host's invocation. Past the page the answer says `truncated` and
carries `next_page`: passing it back as `page` reads on with the same
query. Gmail's `resultSizeEstimate` is an estimate and is not passed on.

**gmail.read** takes a `message_id` and answers its headers (`from`,
`to`, `cc`, `reply_to`, `subject`, `date`, encoded-words decoded), its
labels and thread, and its text: the text/plain part, or else the
text/html part as plain text this plugin extracts — scripts, styles and
tags dropped, a block's end kept as a line break, entities decoded — and
`text_from` says which. The text is at most 16 KiB an answer, cut between
characters; `truncated` and `next_offset` read on (`offset`). Bytes that
are not UTF-8 are shown as U+FFFD and `text_from` names the part's
charset. Attachments are listed by name, type and size and never fetched;
`attachment_count` counts them all, the list names the first fifty. A
header value is cut at 1000 characters (300 in a search), marked "…".

What either read returns is mail other people wrote, which the identity
did not write and cannot verify. Every answer says so in `whose_words`,
with the sender and date the mail itself claims — a From is the sender's
to write, and to forge — and that what the mail asks is not an
instruction. The host already carries every plugin answer to the
identity inside its untrusted wrap (aii-os
internal/untrusted); this plugin adds no marking of its own.

## Draft and send are different acts — and the host cannot tell them apart

**gmail.draft** saves one message in Drafts (`drafts.create`), where it
reaches no one until a person sends it. It never sends: the one call it
writes is the draft, and no answer of Gmail's makes it write another.
It declares `write.external` and not `operator_confirms`, so the host
dispatches it on the identity's call, with no card. Its answer keeps the
`draft_id`, the draft's `message_id` and `thread_id`, so a person can find
it. A draft whose answer was lost may have been saved; asking again may
save a second, and the answer says to look in Drafts first.

**gmail.send** sends one message once the operator confirms it: it saves
the message as a draft (`drafts.create`) and sends that draft
(`drafts.send`). It declares `operator_confirms`: the identity's call proposes
it, the host shows the operator the arguments on the Plugins page — `to`,
`cc`, `subject`, `body`, and `in_reply_to` when named — and
the operation runs once, stamped, when the operator confirms. A call
without the stamp is refused before anything is asked of anyone. The card
also offers Always; an operator who chooses it for gmail.send lets every
later send run at once on the identity's call, until they revoke it.

**What the host sees.** A draft and a send are both a POST to
`gmail.googleapis.com` under the same token, and the host's grant is per
host, not per path: it cannot tell them apart. What stops a draft from
becoming a send is this plugin's code and the operator's confirmation of
gmail.send, never the host's grant. The operator's read-only grant is the
host's own line: under it the broker refuses every POST, and the host
refuses both operations before they are dispatched.

## The message it writes

`To` and `Cc` are plain addresses, `name@example.com`, at most twenty
each: no display name, no comment, no second address inside one, no one
named twice. There is no `Bcc`: a hidden recipient is one the operator's
card cannot show, and naming one is refused. The subject is one line of
at most 250 characters, written as it is when it is printable ASCII and
as RFC 2047 encoded-words otherwise (and when it holds `=?`, which a mail
reader would decode). The body is plain text, sent as UTF-8 in base64
lines. Anything that would add to the message what the card does not
show — a line break in the subject or a recipient, a control character —
is refused by name before any call. `From`, `Date` and `Message-ID` are
not written: Gmail writes them, and writes its own Message-ID over any a
client gives (below).

The body is at most 64 KiB, about ten thousand words. Gmail is given it
twice encoded — base64 lines inside the base64url message — and the broker
carries at most 256 KiB in a request; the largest mail these bounds allow,
every field full, is a request of 138 KiB.

**A reply** names `in_reply_to`, the id of the message it answers. The
plugin reads that message first and writes what Gmail's threading asks
for: its `threadId`, `In-Reply-To` its Message-ID, and `References` its
thread's (the first and the latest, twenty at most); a Message-ID another
sender wrote is written back only when it is one, nothing riding on it.
The subject is the card's and must be the thread's, `Re: ` or not. The
recipients are the identity's to name: nothing is filled in for them.

## A send settled by its draft

Gmail takes no client id for a send and does not refuse a second copy: two
identical sends are two mails. And no id of ours can find an earlier send:
Gmail writes its own Message-ID over one a client gives — the engineering
write-ups of Mixmax and of the widely used google_workspace_mcp say so,
Google's sending guide is silent, and the first live send shows it
here — so the search by a derived Message-ID this plugin used to make could
not find what it looked for.

What Gmail does give is a draft, and it deletes a draft when it sends it
(Gmail's drafts guide). A send saves the message as a draft first, and sends
that draft by its id. Nothing is stored here: Gmail's Drafts folder is the
write-ahead record. When the answer to the send is lost — a 5xx, a timeout, an
answer that cannot be read — the plugin deletes the draft, and Gmail's answer
settles it: deleted, the draft had not been sent, and now cannot be, so
nothing was sent and asking again is safe; already gone (404), it was sent.
The plugin never writes the send twice, and a draft that did not leave is
deleted, or its id is named where the delete failed.

**What it leaves.** A send killed or unreachable between the draft and the
send leaves a draft in Drafts and nothing sent. A send killed after Gmail took
it and asked again is a second mail: no state here connects the two calls, and
the host answers a killed write as unknown — check Sent before asking again.
A draft gone because a person deleted it in the few seconds between the two
calls reads as sent (the receipt says `confirmed: false` and that the message
is not named). That the delete and a late-arriving send cannot both succeed on
one draft is not documented and not yet measured under a race. The
earlier design of this README weighed the draft and refused it — two POSTs
for one confirmed act, a leftover draft, a read-back that proves less than it
seems — and chose a search; the search cannot work if Gmail replaces the id,
the leftover is deleted, and the delete, not a read, is what arbitrates.

## The inbox as a channel

The same plugin is a channel adapter (family `channel_adapter`; the tools
above stay). Gmail has no blocking read; its one push is `users.watch`, which
makes Google's Cloud Pub/Sub POST a notification — an address and a history id
— to the identity's public origin (`/hooks/com.aiii.examples.gmail/notify`).
The host verifies Google's signed token before `gmail.notify` runs (the
subscription's service account, this webhook's URL); a forged push never
reaches the plugin. A notification is a wake-up and nothing more: the truth is
a **cursor**, the history id of the last mail handed over, kept in kv, and
`history.list` from it names what is new. A lost, late or repeated
notification costs nothing that the cursor does not repair.

**What is periodic, and why it cannot be an event.** Two duties are scheduled
by the host (`schedule` in plugin.json):

| operation | every | why it cannot be an event | bound |
|---|---|---|---|
| `gmail.renew` | 24 h | a watch lasts at most 7 days and its expiry announces nothing — mail simply stops; Google recommends renewing daily | 1 call a day, 100 quota units |
| `gmail.sync` | 30 min | Google documents that notifications "might be delayed or dropped" and says to fall back to `history.list` after a period with no notification; nothing announces a notification that never came | 1 call per 30 min, 5 quota units; a dropped notification is at most 30 min late |

A renewal that fails is not silent: the host says it, once a day, as an event
arrival. A first renewal also takes the mailbox's place as the cursor —
mail from before the watch is not delivered.

**The cursor moves only through what the host recorded**. A call
returns arrivals and keeps, in kv, the cursor they lead to (`pending`); it
becomes the cursor when a later call is told, in `recorded`, that the host holds
every one of them. Until then the next call reads from the old cursor and offers
them again — the host records an arrival once by id, so a repeated offer costs
nothing, and one the host refused is not lost.

**What an arrival is.** The message's From, Subject and Date, its text bounded
to 4 KiB with the route to the rest (`gmail.read message_id offset`), its
attachments by name — mail other people wrote, which the host frames as
untrusted. At most six messages a call (each is a read inside a 90 s
invocation): more waits, and the push answers 503 so Pub/Sub delivers again.
**A From is offered as the sender's address only when Gmail's own
`Authentication-Results` (authserv-id `mx.google.com`, the first) says
`dmarc=pass`;** otherwise it is `unverified:<address>`, which no contact matches
and so cannot wake the identity, and the body says why. Authentication is about
who sent the mail, never about whether to trust what it says.

**Bounces.** A delivery-status report — the mail system's word that something
sent did not arrive — is parsed by RFC 3464 (`message/delivery-status`:
recipient, action, status, diagnostic) and classified by RFC 3463's first digit
(5 permanent, 4 transient, 2 delivered), never by a model: "the mail to
recipient@example.net was NOT delivered — a permanent failure (status 5.1.1)". One this
code cannot read is passed on as text and says so. Gmail has no delivery or
complaint event, so "delivered" is never claimed of a mail Gmail accepted.

**When the cursor is gone.** Gmail keeps history about a week; a 404 from
`history.list` means it no longer holds the cursor's. The plugin says so, finds
the inbox's mail since the last message it took in by search — at most six, the
rest declared with the search that finds them — and sets a new cursor. The gap is
an arrival from the mailbox itself that begins "[a notice from the gmail plugin,
not a person's mail]".

**Sending to a contact.** `send` (the channel's) mails an address from the
operator's address book with no card — a contact is the consent — as a draft sent
by its id, exactly as a tool's send is; its subject is the body's first line
(78 characters at most, else "(no subject)").

**Send window.** A channel's sends are bounded: `send_limit` in any
`send_window_minutes` (defaults 20 and 60, both settings on the plugin's card,
1 to 1000 and 1 to 1440). A send over the limit is refused before Gmail is
asked, in `NET_REMOTE_FAILED` — nothing accepted — saying which limit, where the
operator changes it, and when the next send may go. A send whose outcome is not
known counts, for it may have gone; one Gmail refused does not. The times of the
recent sends are the plugin's own kv (`sdk.SendWindow`), so the adapter needs
`ring4.kv`; without the grant, or without the host's clock, it sends nothing and
says which is missing. This is the field's containment for a runaway identity
(a per-window rate limit on autonomous sends): a contact in the address book is
the consent to a message, not to a thousand.

**Setting it up.** Two things stay the operator's, once, in the Google project
the account connects through: enable the Gmail and Pub/Sub APIs, and create a
service account for the push (no project roles) that the Pub/Sub service agent may
create tokens for (Service Account Token Creator on it) — an IAM step that needs
far more consent than this plugin is given, so it is not offered. The rest is one
operation. Connect the Google account on the Plugins page made with the gmail
**and pubsub** services, grant kv, set `topic` (`projects/<project>/topics/<topic>`)
and `push_account` (that service account's email), give the identity a public name
(Settings → Public Name), and ask the identity to run **gmail.setup**. The card
the operator confirms says what it creates in the project: the topic and a push
subscription `<topic>-push` if they are not there; the Pub/Sub Publisher role for
`gmail-api-push@system.gserviceaccount.com` on the topic, added to the policy the
topic has and replacing none of it; the subscription pointed at the address the
host says this identity's webhook is, signed as `push_account` for that same
address as its audience (if it exists it is brought to that shape, and one on
another topic is refused, not moved); and the watch. Every step is safe to run
again, a step that fails stops the rest, and its words say what was done and what
was not. The daily renewal (`gmail.renew`) keeps the watch. It never deletes, and
`cloud-platform` is not asked for: the consent is the pubsub service alone. Made
by hand instead, the same resources work. Whether the connected user's own
permission to act as the push service account suffices under that consent, and
whether Pub/Sub needs a billing account, are unmeasured.

**Host version.** The package declares `aiios_min_version` 0.1.11: it needs the
`google-oidc` webhook scheme, scheduled operations and the host's word on where a
webhook is, none of which a 0.1.10 host has, and such a host refuses it by name at
install.

## What the answers mean

| Gmail's answer to the send | code | meaning |
|---|---|---|
| 200 naming a message | receipt `sent` | taken for sending; read back |
| lost, 5xx, unreadable — and the draft is gone | receipt `sent`, `confirmed: false` | it left; the message is not named |
| lost, 5xx, unreadable — and the draft is deleted | `NET_REMOTE_FAILED` | nothing sent; asking again is safe |
| lost, 5xx, unreadable — and the delete cannot be settled | `NET_EFFECT_UNKNOWN` | may have been sent; the draft is named |
| 400 | `OPERATION_ARGUMENT_INVALID` | Gmail will not send this message; its draft is deleted |
| 401, 403, 429, any other 4xx, not reached | `NET_REMOTE_FAILED` | nothing sent; its draft is deleted |
| the host's refusal | its own code | nothing sent; its draft is deleted |
| the draft's own answer: any refusal or failure | as a draft's | nothing sent; a draft may wait in Drafts |

A receipt says Gmail accepted the mail for sending, and after reading it
back what Gmail holds: its labels (`SENT`), its thread and its Message-ID,
`confirmed` when a read shows it in Sent. A read that fails proves nothing
either way, and the receipt says the send was taken, that its confirmation
is unavailable, and why. No receipt says anyone received, read or accepted
the mail; each one says so in `delivery`. A 403 names what is missing when
Google's error names it: the scope the consent lacks, or a project where
the Gmail API is not enabled. `NET_AUTH_PROFILE_DISCONNECTED` means the
operator connects the Google account again on the Plugins page,
`NET_AUTH_PROFILE_UNAVAILABLE` that a refresh failed just now — reads and
writes alike. A draft answers in the same codes, and a failed read in
`NET_REMOTE_FAILED`: a read has no effect to be unsure of.

## What is not built

Bcc; sending or fetching attachments; HTML mail; sending a draft a person saved;
labels, archiving and deleting; creating the push service account and letting
Pub/Sub sign as it (an IAM grant the operator makes once, above); tearing the
setup down. IMAP and SMTP are not used: the host's google
template lists `imap.gmail.com` and `smtp.gmail.com`, and this plugin's
envelope names only `gmail.googleapis.com:443` and, for `gmail.setup`,
`pubsub.googleapis.com:443`.

## Live service qualification

The Gmail API reference and guides document the calls, their scopes, the
threading rule and what `messages.list` returns (ids and threads only).
These are not documented, or come only from others' reports, and the live
journey settles each:

- that Gmail writes its own Message-ID over one a client gives (Mixmax and
  the google_workspace_mcp report it; Google's guides do not say) — this
  plugin no longer writes one;
- that `drafts.delete` after a send whose answer was lost is the arbiter:
  a draft gone means sent, a draft deleted means it cannot arrive, including
  under two racing calls;
- that Gmail writes `From` and `Date` when a message has none;
- that `messages.get` finds a sent message by its id at once, and that a
  sent draft's message has a new id (Gmail's drafts guide says so);
- that `body.data` is UTF-8 whatever charset the part names (one report
  says Gmail transcodes it), and whether header values arrive with
  encoded-words decoded (this plugin decodes either way);
- that a 4xx from `drafts.send` means nothing was sent, and the 403
  reasons Gmail gives for a missing scope and for a disabled API;
- that Gmail threads a reply whose subject differs from the thread's only
  in case or a `Re:`;
- what a real Pub/Sub push carries and how soon it comes, that a push
  subscription's OIDC token is what the host verifies (audience the endpoint
  URL, the subscription's service account as `email`), and whether Pub/Sub
  needs a billing account;
- the shape of `Authentication-Results` on a real message, and of a real
  bounce — whether Gmail's full-format payload exposes its `message/delivery-status`
  and original headers as parts, which this code parses when it does;
- that `history.list` with `labelId=INBOX` and `historyTypes=messageAdded`
  returns the messages each record added with their labels, and that
  `startHistoryId` is exclusive.

## On an identity

Install at T2 (the credential floor). On the Plugins page, create a
profile for Google with the gmail service — read for search and read,
modify to save drafts and send (the host's template gives `gmail.readonly`
for read, and `gmail.modify` and `gmail.send` for modify) — and connect
it in your browser, or answer the identity's connect card, which opens
that form prefilled from this plugin's own hint. Grant
`net.outbound:gmail.googleapis.com:443` and the profile's name as a
credential handle in `plugins.grants.<id>`, and set the `google` setting
to the profile's name. The host refreshes the token and injects it at the
wire; this code never sees it. A Google project in Testing status expires
its refresh tokens after seven days, so reconnect weekly until the project
is verified.
