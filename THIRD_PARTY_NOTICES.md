# Third-party notices

Smartnote is distributed under the MIT License (`LICENSE`). Its packages also contain the
third-party components below. Their licence texts are in the `licenses/` directory next to this
file:

| File | Contents |
|---|---|
| `licenses/onnxruntime/LICENSE` | Licence of ONNX Runtime 1.31.0 |
| `licenses/onnxruntime/ThirdPartyNotices.txt` | Notices of the components inside ONNX Runtime 1.31.0 |
| `licenses/rust-crates.txt` | List and licence files of the Rust crates in `smartnote-search` and of the Rust standard library |
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

## 5. Google Sans font and the Google "G" logo

The "Sign in with Google" button of the settings uses Google Sans Medium, as the Sign in with Google
Branding Guidelines require, and the standard colour Google "G" logo.

- Font: `frontend/src/assets/fonts/GoogleSans-Medium-subset.woff2`, built into the application.
  Source: `ofl/googlesans/GoogleSans[GRAD,opsz,wght].ttf` of <https://github.com/google/fonts>
  (SHA-256 `d0a87d835a944b8b40d0e82a5651bb59ab97b936a2aeed5946eb57e7b2a3a90a`), Copyright 2025
  The Google Sans Project Authors (<https://github.com/googlefonts/googlesans>), SIL Open Font
  License 1.1 without Reserved Font Names: the text below and
  `frontend/src/assets/fonts/GoogleSans-OFL.txt`. Changes: the static instance wght 500, GRAD 0,
  opsz 17, reduced to Basic Latin, U+00A0, the basic Cyrillic block U+0400-U+045F and U+2026
  (fontTools `varLib.instancer` and `subset`), in the WOFF2 format. "Google" and "Google Sans"
  are trademarks of Google LLC; no affiliation with or sponsorship by Google LLC is implied.
- Logo: `frontend/src/assets/icons/google-g.svg`, the standard colour "G" as the plain SVG paths
  of the HTML button generator on <https://developers.google.com/identity/branding-guidelines>,
  unchanged. It is a trademark of Google LLC, used as the guidelines permit.

```text
Copyright 2025 The Google Sans Project Authors (https://github.com/googlefonts/googlesans)

This Font Software is licensed under the SIL Open Font License, Version 1.1.
This license is copied below, and is also available with a FAQ at:
https://openfontlicense.org


-----------------------------------------------------------
SIL OPEN FONT LICENSE Version 1.1 - 26 February 2007
-----------------------------------------------------------

PREAMBLE
The goals of the Open Font License (OFL) are to stimulate worldwide
development of collaborative font projects, to support the font creation
efforts of academic and linguistic communities, and to provide a free and
open framework in which fonts may be shared and improved in partnership
with others.

The OFL allows the licensed fonts to be used, studied, modified and
redistributed freely as long as they are not sold by themselves. The
fonts, including any derivative works, can be bundled, embedded, 
redistributed and/or sold with any software provided that any reserved
names are not used by derivative works. The fonts and derivatives,
however, cannot be released under any other type of license. The
requirement for fonts to remain under this license does not apply
to any document created using the fonts or their derivatives.

DEFINITIONS
"Font Software" refers to the set of files released by the Copyright
Holder(s) under this license and clearly marked as such. This may
include source files, build scripts and documentation.

"Reserved Font Name" refers to any names specified as such after the
copyright statement(s).

"Original Version" refers to the collection of Font Software components as
distributed by the Copyright Holder(s).

"Modified Version" refers to any derivative made by adding to, deleting,
or substituting -- in part or in whole -- any of the components of the
Original Version, by changing formats or by porting the Font Software to a
new environment.

"Author" refers to any designer, engineer, programmer, technical
writer or other person who contributed to the Font Software.

PERMISSION & CONDITIONS
Permission is hereby granted, free of charge, to any person obtaining
a copy of the Font Software, to use, study, copy, merge, embed, modify,
redistribute, and sell modified and unmodified copies of the Font
Software, subject to the following conditions:

1) Neither the Font Software nor any of its individual components,
in Original or Modified Versions, may be sold by itself.

2) Original or Modified Versions of the Font Software may be bundled,
redistributed and/or sold with any software, provided that each copy
contains the above copyright notice and this license. These can be
included either as stand-alone text files, human-readable headers or
in the appropriate machine-readable metadata fields within text or
binary files as long as those fields can be easily viewed by the user.

3) No Modified Version of the Font Software may use the Reserved Font
Name(s) unless explicit written permission is granted by the corresponding
Copyright Holder. This restriction only applies to the primary font name as
presented to the users.

4) The name(s) of the Copyright Holder(s) or the Author(s) of the Font
Software shall not be used to promote, endorse or advertise any
Modified Version, except to acknowledge the contribution(s) of the
Copyright Holder(s) and the Author(s) or with their explicit written
permission.

5) The Font Software, modified or unmodified, in part or in whole,
must be distributed entirely under this license, and must not be
distributed under any other license. The requirement for fonts to
remain under this license does not apply to any document created
using the Font Software.

TERMINATION
This license becomes null and void if any of the above conditions are
not met.

DISCLAIMER
THE FONT SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO ANY WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT
OF COPYRIGHT, PATENT, TRADEMARK, OR OTHER RIGHT. IN NO EVENT SHALL THE
COPYRIGHT HOLDER BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
INCLUDING ANY GENERAL, SPECIAL, INDIRECT, INCIDENTAL, OR CONSEQUENTIAL
DAMAGES, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
FROM, OUT OF THE USE OR INABILITY TO USE THE FONT SOFTWARE OR FROM
OTHER DEALINGS IN THE FONT SOFTWARE.
```
