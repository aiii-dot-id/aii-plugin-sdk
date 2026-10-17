# logging

A logging plugin built with the kit: the identity's events — a tool
called, a turn started and ended, its rhythm's alarms firing — are
delivered by the host to `log.event`, which writes a line to a log in
the plugin's private directory; `log.read` hands the recent lines back.

    aiisdk build && aiisdk test

The cases deliver events the way the host does and read them back. On
an identity the host delivers what `plugin.json` subscribes to; the
private directory needs no grant. The identity never calls `log.event`
itself — a subscribed operation is the host's to invoke.
