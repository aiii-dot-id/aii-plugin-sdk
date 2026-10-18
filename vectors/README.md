# Conformance vectors

Byte-for-byte copies of the AII OS runtime's conformance vectors, each
beside the path the runtime keeps it at:

| File | What it pins | The runtime's copy |
|---|---|---|
| `json_domain.json` | the strict JSON domain | `spec/bbb/vectors/json_domain.json` |
| `framing.json` | the frame codec | `spec/bbb/vectors/framing.json` |
| `schema_subset.json` | the closed JSON Schema subset | `internal/jsonschema/testdata/schema_subset.json` |
| `settings_decl.json` | the settings declaration grammar | `internal/pluginhost/testdata/settings_decl.json` |
| `audio_framing.json` | the audio frame codec | `spec/audio/vectors/audio_framing.json` |
| `session_topology.json` | the audio topology of a resident speech session's open | `spec/audio/vectors/session_topology.json` |
| `runtime_extent.json` | a runtime declaration's extent and the host's pre-download ceiling refusal | `internal/pluginhost/testdata/runtime_extent.json` |
| `host_window.json` | a package's host-version window: the version grammar and how versions compare | `internal/pluginhost/testdata/host_window.json` |
| `variant_selection.json` | variant preference and accelerator matching | `internal/pluginhost/testdata/variant_selection.json` |
| `model_when.json` | a model needed only for some values of a setting: the member's form, the package's own settings, and the floor | `internal/pluginhost/testdata/model_when.json` |
| `embeddings.json` | the declaration of a package that answers the host with vectors: its bounds, its probes, and how strictly it is read | `internal/pluginhost/testdata/embeddings.json` |
| `validation_checks.json` | a package's checks (validation.json): the cosine a vector check is decided on, the JSON pointer a `$ref` and a json expectation read with, the file's grammar (a session check's recordings carried as binary members; a lane no host runs read and not run), and the session lane's normaliser, a reply's level and the session judge | `internal/pluginhost/testdata/validation_checks.json` |

The package tests run this kit's own codecs against them, so the two
implementations cannot drift silently. `e2e.TestVectorsMatchSibling`
compares every file with the runtime's copy, and
`e2e.TestEveryVectorIsDeclared` (no host needed) refuses a file in this
directory that the comparison does not name.

The runtime owns these files. A fix belongs there first; a change here
alone is a fork.
