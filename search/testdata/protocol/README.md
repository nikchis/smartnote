# Protocol samples

Shared CBOR bodies of the worker protocol (spec §7, §12.2), without the 4-byte length prefix.
Rust writes them (`UPDATE_GOLDEN=1 cargo test -p worker --test protocol`); both sides must
decode each file to the value in `worker/tests/protocol.rs` and encode it back **byte for byte**.

| File | Message |
|---|---|
| `hello.cbor` | `hello`: proto 1, model and ONNX Runtime paths, 4 threads, parent pid |
| `upsert_with_emb.cbor` | `upsert`: two notes; the first with a 40-byte `emb`, Cyrillic, a newline and an emoji; the second without `emb` |
| `remove.cbor` | `remove`: two items |
| `search.cbor` | `search`: query `"retry backoff" ->`, limit 50 |
| `ready.cbor` | `ready`: 32-byte `index`, lexical `ready`, semantic `loading`, sandbox `full` |
| `result.cbor` | `result`: two hits; marks, all three `sources`, fractional `score` |
| `embedded.cbor` | `embedded`: 40-byte `emb` |
| `status.cbor` | `status`: lexical `ready`, semantic `unavailable` |
| `error.cbor` | `error`: `req` 9, code `bad_request` |

Encoding rules the Go side mirrors:

- a definite-length map; `t` first, then the fields in the order of the message definition;
  nested structs (`notes`, `items`, `hits`) are maps in the same way, `marks` are arrays of two
  integers;
- integers in the shortest form; `score` (`f32`) in the shortest lossless float (`ShortestFloat16`);
- `emb` and `index` are byte strings, not arrays;
- an absent optional field (`notes[].emb`, `error.req`) is omitted, not null (`omitempty`);
  a null is accepted on decoding;
- unknown `t`, unknown fields and missing required fields are errors.
