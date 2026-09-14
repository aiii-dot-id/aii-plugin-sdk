# github-issues

A connector built with the kit: the HTTP verbs under one egress grant, a
credential that is a handle and never a value, an honest effect on every
write, and a streamed reply consumed in chunks.

The hermetic cases (`tests/`) prove the connector's honesty without the
network: every call is denied without a grant, and the denial names what
would lift it; a missing setting and a missing title fail before any
call.

    aiisdk build && aiisdk test

The live cases reach the real API for a public repository, which needs
no token:

    aiisdk test -cases tests-live -grant net.outbound:api.github.com:443

Writes need the host: install the package on an identity at T2 (the
broker's credential floor), grant `net.outbound:api.github.com:443` and a
credential handle in `plugins.grants.<id>`, configure the handle as an
auth profile pinned to `api.github.com:443` with the token in its secret
source, and name it as the `token` setting in the plugins view. Then
`issues.create` opens an issue, and the receipt's `effect` says whether
the request was performed, not performed, or sent with its response
lost — the one case this connector refuses to retry on its own.
