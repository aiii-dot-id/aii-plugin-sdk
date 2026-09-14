# memory-keywords

A memory with no model: `memory.remember` stores a text with its word
set, `memory.recall` returns the best word-overlap matches, and
`memory.forget` removes one. It needs `ring4.kv` only and runs at T0
with a kv grant — the honest floor to compare memory-embeddings against.

    aiisdk build && aiisdk test -grant kv

The cases store, recall and forget, and prove the operator's
`recall_limit` setting is honored.
