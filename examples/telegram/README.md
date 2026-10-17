# telegram

A Telegram channel adapter built with the kit: the three methods every
adapter speaks — describe, send, receive — with receive a long poll of
the bot API, so it needs no public origin and no webhook. It keeps the
host's channel contract (WRITING_A_PLUGIN.md §18) both ways.

    aiisdk build && aiisdk test
    go test ./examples/telegram/        # from the kit's root

`aiisdk test` proves the module on the host's worker without the
network: describe names the channel, says it receives by poll and
promises a ten-second budget; an address that is not a chat id or
@username is refused before any call; a send without the bot_token
handle is refused by name; a send and a receive are denied without a
grant. `go test` drives the same handlers over the native lane against
a stand-in host and a scripted Telegram: every answer a send can get,
the messages a receive maps, a failed poll, and a process killed
between a receive's return and the host's record.

**Send** writes one `sendMessage` per message — never parts, never
declared idempotent, so nothing writes it twice — and answers what
Telegram accepted, in the code the host acts on:

- the message: the receipt `chat:message_id`;
- a body outside Telegram's 1 to 4096 characters, refused before any
  call; a 403; a 400 about the chat (`chat not found`, or a group that
  became a supergroup, whose new chat id the answer names):
  `OPERATION_ARGUMENT_INVALID` — the host tries the next channel and
  parks the message after the walk;
- a 429, with the wait Telegram named (`parameters.retry_after`, else
  the Retry-After header the host read); any other 4xx; `ok:false`;
  Telegram not reached: `NET_REMOTE_FAILED` — nothing was accepted, so
  the host tries the next channel and counts the attempt;
- a 5xx, a lost response, a success it cannot read:
  `NET_EFFECT_UNKNOWN` — the host parks the message as maybe sent, says
  so, and never sends it again;
- the host's own refusal: relayed with its code.

**Send window.** A channel's sends are bounded: `send_limit` in any
`send_window_minutes` (defaults 20 and 60, both settings on the plugin's card,
1 to 1000 and 1 to 1440). A send over the limit is refused before Telegram is
asked, in `NET_REMOTE_FAILED` — nothing accepted — saying which limit, where the
operator changes it, and when the next send may go. A send whose outcome is not
known counts, for it may have gone; one Telegram refused does not. The times of the
recent sends are the plugin's own kv (`sdk.SendWindow`), so the adapter needs
`ring4.kv`; without the grant, or without the host's clock, it sends nothing and
says which is missing. This is the field's containment for a runaway identity
(a per-window rate limit on autonomous sends): a contact in the address book is
the consent to a message, not to a thousand.

**Receive** asks `getUpdates` for one message update a poll, waiting up
to nine seconds, and answers each as `{id: "<chat>:<message_id>", from:
"<chat>", body}`: the text, the caption, or `[photo]` and its kin.
Telegram forgets an update once it is asked for a later offset, so the
offset moves past an update, and is written to kv, only once the host
says it recorded the arrival the update became: describe says
`acknowledges`, and every receive carries `recorded`, the ids the host
holds of the last answer. An arrival the host's record refused is not in
it, so the offset stays at that update and the next poll gets it again;
an update that made no arrival moves on with the ones before it. A
process killed before the host said anything revives, gets the same
updates again, and the host records each once by id. Without kv the
offset lives in memory, and a revived process gets what Telegram never
forgot. A host that sends no `recorded` is an older one: its next
receive is its word that it is finished with the last answer, as it
always was.

What it does not do, and where that shows:

- Edits are not asked for: the identity reads a message as first
  written.
- "chat not found" is read from Telegram's description, a sentence; any
  other 400 is taken as this message refused, which the host still
  walks on from.
- Telegram documents 4096 characters without naming the unit; this
  counts code points, as its input schema does. A text Telegram measures
  as longer is refused there, and answered `NET_REMOTE_FAILED`.
- A plugin's failure has no field for a wait, so the wait a 429 names
  rides in the answer's words.

On an identity: install at T2 (the credential floor), grant
`net.outbound:api.telegram.org:443`, `kv` and a credential handle in
`plugins.grants.<id>`, configure that handle as an auth profile with
`"scheme": "path"` pinned to `api.telegram.org:443` whose secret is the
bot token from @BotFather, and set the `bot_token` handle in the plugins
view. The URLs this code writes name `{credential}` where Telegram
wants the token; the broker substitutes it when it dials and this code
never sees it. Add a contact with channel `telegram`, the chat id as the
address (send the bot a message and read `from` in the host's log, or
ask @userinfobot), and wake if that person may wake the identity. The
host records each arrival once by `chat:message_id`, wakes the identity
only for a contact with wake, and delivers the identity's replies
through `send` in the operator's channel order.
