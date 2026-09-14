# document-ingest

A document plugin built with the kit: a text file from a folder the
operator granted is read in pieces, split into chunks the operator sizes
in the plugins view, stored in RING4, and recalled by shared words; a
private index in the plugin's own directory records what was ingested.

    aiisdk build && aiisdk test -grant kv -grant root:docs=sample

The cases prove the whole path on the author's machine: a listing, an
ingest, a recall, a missing file, a traversal refused before anything is
opened, and an ungranted root denied by name. On an identity, the
operator grants `kv` and a root in `plugins.grants.<id>` — the private
directory needs no grant — and the host refuses any root that contains
the identity's own files.
