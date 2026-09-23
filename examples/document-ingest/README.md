# document-ingest

A document plugin built with the kit: a text file from the identity's
sandbox is read in pieces, split into chunks the operator sizes in the
plugins view, stored in RING4, and recalled by shared words; a private
index in the plugin's own directory records what was ingested.

    aiisdk build && aiisdk test -grant kv -grant files=sample

The cases prove the whole path on the author's machine: a listing, an
ingest, a recall, a missing file, and paths that leave the sandbox
refused before anything is opened. On an identity, the operator ticks
`files` and `kv` on the plugin's card — the private directory needs no
grant — and the plugin reads by the paths the identity's own tools take:
its home and the folders in Settings → Sandbox, never its data
directory.
