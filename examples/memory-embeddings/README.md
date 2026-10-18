# memory-embeddings

A memory through the identity's own embeddings provider:
`memory.remember` embeds each text with `sdk.Embeddings.Create`,
`memory.recall` ranks by cosine similarity, and `memory.forget` removes
one. It requests `ring4.kv` and `model.embeddings`; the operator grants
`kv` and `embeddings` and names an embeddings model on the provider.

    aiisdk build && aiisdk test -grant kv -grant embeddings

Under `aiisdk test` the vectors come from a hashed-words stand-in —
enough to prove store-and-recall, nothing about meaning.

Each memory is stored with the model that embedded it, and a recall
compares only the memories of the model that embedded the query: a
vector means something only beside vectors of the same model, and two
models may share a width. When the operator names another embeddings
model, the memories of the old one are passed over and counted
(`skipped_other_model`), not ranked on a number about nothing; they are
found again under their own model, or once remembered anew. A case can
have the stand-in answer as another model with `"embeddings_model"`.
