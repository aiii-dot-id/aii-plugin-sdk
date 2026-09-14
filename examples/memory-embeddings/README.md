# memory-embeddings

A memory through the identity's own embeddings provider:
`memory.remember` embeds each text with `sdk.Embeddings.Create`,
`memory.recall` ranks by cosine similarity, and `memory.forget` removes
one. It requests `ring4.kv` and `model.embeddings`; the operator grants
`kv` and `embeddings` and names an embeddings model on the provider.

    aiisdk build && aiisdk test -grant kv -grant embeddings

Under `aiisdk test` the vectors come from a hashed-words stand-in —
enough to prove store-and-recall, nothing about meaning.
