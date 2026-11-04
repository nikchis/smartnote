# Search model preparation

The semantic search uses `intfloat/multilingual-e5-small` at revision
`614241f622f53c4eeff9890bdc4f31cfecc418b3`, prepared reproducibly:

1. `build.py` downloads `onnx/model.onnx` and `onnx/tokenizer.json` of that revision and checks
   their SHA-256 (constants in `build.py`).
2. The vocabulary is pruned to pieces made only of the "broad" character set (Latin with its
   extensions, Greek, Cyrillic, punctuation, symbols, emoji; see `BROAD_RANGES`): 149,642 of
   250,002 pieces stay, special tokens keep ids 0–3, the embedding rows are selected in the same
   order. Text made of these characters tokenizes as before; other characters become `<unk>`.
3. The word embedding table is quantized to int8 with one scale per row:
   `Gather(W)` becomes `Gather(Wq) → Cast → Mul(Gather(scale))`. Other weights stay fp32.
4. `search/model-tool` (`model-tool convert <dir>`) converts the pruned `tokenizer.json` to the
   `kitoken` format (`tokenizer.kit`) and adds its SHA-256 to `manifest.json`.
5. `verify.py` checks the result: manifest digests, vocabulary size, tokenization equal to the
   source tokenizer and embedding cosine to the source model ≥ 0.9999 on the repository corpus.
   The `kitoken_matches_hf_on_repo_corpus` test of `model-tool` checks that `kitoken` and HF
   `tokenizers` give the same ids.

Output in `build/model/`: `model.onnx` (~138 MiB), `tokenizer.kit` (~2.7 MB), `tokenizer.json`
(for the parity test) and `manifest.json`. The files are not committed.

## Commands

- `task model:build` — all steps above; needs [uv](https://docs.astral.sh/uv/) and cargo.
  `task model:build MODEL_DIR=/some/dir` writes elsewhere. Source files are cached in
  `build/model-src/`.
- `task model:fetch` — downloads the published files listed in `model.lock.json` (release
  `model-e5s-1`) into `build/model/` and checks their SHA-256. `MODEL_BASE_URL` overrides the
  release URL for local work, e.g. `MODEL_BASE_URL=file:///path/to/model/ task model:fetch`.

- `reference.py` — reference embeddings for the worker's model test: the texts of
  `search/testdata/embed/texts.json` embedded by the original fp32 model and HF tokenizer
  (NFC, special tokens, truncation to 512, mean pooling, L2), written to
  `search/testdata/embed/ref.json`:
  `uv run --project tools/model --locked python tools/model/reference.py --sources build/model-src
  --texts search/testdata/embed/texts.json --out search/testdata/embed/ref.json`.
  The test (`cargo test -p worker --test embed -- --ignored` in `search/`) needs
  `SMARTNOTE_MODEL_DIR` and `SMARTNOTE_ORT_LIB` (the library from `task ort:fetch`).

After a new build is published, update `model.lock.json` with the digests from `manifest.json`
and the SHA-256 of `manifest.json` itself.
