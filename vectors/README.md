# Conformance vectors

Byte-for-byte copies of the AII OS runtime's BBB conformance vectors:
the strict JSON domain (`json_domain.json`), the frame codec
(`framing.json`), the closed JSON Schema subset (`schema_subset.json`),
the settings declaration grammar (`settings_decl.json`) and the audio
frame codec (`audio_framing.json`). The package tests run this kit's own
codecs against them, so the two implementations cannot drift silently.

The runtime owns these files. A fix belongs there first; a change here
alone is a fork.
