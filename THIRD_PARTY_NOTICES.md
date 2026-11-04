# Third-party notices

Smartnote is distributed under the MIT License (`LICENSE`). Its packages also contain the
third-party components below. Their licence texts are in the `licenses/` directory next to this
file:

| File | Contents |
|---|---|
| `licenses/onnxruntime/LICENSE` | Licence of ONNX Runtime 1.31.0 |
| `licenses/onnxruntime/ThirdPartyNotices.txt` | Notices of the components inside ONNX Runtime 1.31.0 |
| `licenses/rust-crates.txt` | List and licence files of the Rust crates in `smartnote-search` |
| `licenses/go-modules.txt` | List and licence files of the Go modules in `smartnote` and of the Go standard library |

Where the files are installed:

| Package | Directory |
|---|---|
| `.deb` | `/usr/share/doc/smartnote/` |
| AppImage | `usr/share/doc/smartnote/` inside the image |
| macOS | `smartnote.app/Contents/Resources/` |
| Windows | the installation directory |

## 1. Embedding model

The search model (`model.onnx`, `tokenizer.kit`, `manifest.json`) is derived from
multilingual-e5-small.

- Source: `intfloat/multilingual-e5-small`, revision `614241f622f53c4eeff9890bdc4f31cfecc418b3`,
  <https://huggingface.co/intfloat/multilingual-e5-small>.
- Authors, as stated in the model card: Liang Wang, Nan Yang, Xiaolong Huang, Linjun Yang,
  Rangan Majumder, Furu Wei, "Multilingual E5 Text Embeddings: A Technical Report",
  arXiv:2402.05672.
- Licence: MIT (the `license: mit` field of the model card; the model repository has no licence
  file and no copyright line). The model is initialised from
  `microsoft/Multilingual-MiniLM-L12-H384` (MIT) and uses the tokenizer vocabulary of
  `FacebookAI/xlm-roberta-base` (MIT).
- Changes: the vocabulary is reduced to 149 642 tokens (Latin, Greek and Cyrillic scripts,
  punctuation, symbols and emoji), the word embedding table is quantised to int8 with a scale per
  row, and the tokenizer is converted to the `kitoken` format (`tokenizer.kit`); the other weights
  are unchanged. `manifest.json` lists the SHA-256 of the files.

```text
MIT License

Copyright (c) the authors of multilingual-e5-small, as stated in the model card

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## 2. ONNX Runtime

The search worker loads ONNX Runtime 1.31.0, the official CPU build of Microsoft
(`libonnxruntime.so.1.31.0`, `libonnxruntime.1.31.0.dylib` or `onnxruntime.dll`, unmodified, from
<https://github.com/microsoft/onnxruntime/releases/tag/v1.31.0>).

- Copyright (c) Microsoft Corporation, MIT License: `licenses/onnxruntime/LICENSE`.
- The components built into ONNX Runtime and their licences, among them protobuf
  (BSD-3-Clause), abseil-cpp (Apache-2.0), re2 (BSD-3-Clause), flatbuffers (Apache-2.0), cpuinfo
  (BSD-2-Clause), XNNPACK (BSD) and Eigen (MPL-2.0; its source files are available at
  <https://gitlab.com/libeigen/eigen>):
  `licenses/onnxruntime/ThirdPartyNotices.txt`, the file of the same name from the release
  archive.

## 3. Rust crates

`smartnote-search` links the Rust standard library (MIT OR Apache-2.0) and the crates of
`search/Cargo.lock` that its target uses. `licenses/rust-crates.txt` lists them with versions,
licences and repositories, followed by their licence files, and is generated with every package
by `tools/notices` from `cargo metadata`. Licences besides MIT and
Apache-2.0 include those of kitoken (BSD-2-Clause, Copyright (c) 2023 Christian Sdunek), frostem
(BSD-3-Clause, with the Snowball licence), unicode-normalization and unicode-ident (also
Unicode-3.0), foldhash (Zlib) and, on Linux, seccompiler (Apache-2.0 OR BSD-3-Clause).

## 4. Go modules

`smartnote` links the Go standard library (BSD-3-Clause) and the modules of `go.sum` that its
target uses. `licenses/go-modules.txt` lists them, followed by their licence files, and is
generated with every package by `tools/notices` from `go list`.
