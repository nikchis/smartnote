"""Computes reference embeddings for the worker's model test (design spec 6.7, 12.1).

Each text of the input file is embedded the way the worker does it, but with the ORIGINAL
fp32 model and HF tokenizer of the pinned revision: the model text (`passage: <name>\\n<body>`
or `query: <text>`) in NFC, the source tokenizer with special tokens and truncation to
MAX_TOKENS, mean pooling over the tokens, L2 normalization. The output lists, for every text,
the number of tokens without special tokens (before truncation) and the vector rounded to
7 significant digits.

Usage: uv run reference.py --sources <download cache dir> --texts <texts.json> --out <ref.json>
"""

import argparse
import json
import unicodedata
from pathlib import Path

import numpy as np
import onnxruntime as ort
from tokenizers import Tokenizer

import build
from verify import MAX_TOKENS, embed


def model_text(text: dict) -> str:
    if text["kind"] == "passage":
        raw = f"passage: {text['name']}\n{text['body']}"
    elif text["kind"] == "query":
        raw = f"query: {text['text']}"
    else:
        raise ValueError(f"unknown kind {text['kind']!r}")
    return unicodedata.normalize("NFC", raw)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--sources", type=Path, required=True, help="download cache for the source files")
    parser.add_argument("--texts", type=Path, required=True, help="JSON list of texts")
    parser.add_argument("--out", type=Path, required=True, help="output JSON with the reference vectors")
    args = parser.parse_args()

    sources = build.fetch_sources(args.sources)
    tokenizer = Tokenizer.from_file(str(sources["tokenizer.json"]))
    session = ort.InferenceSession(str(sources["model.onnx"]), providers=["CPUExecutionProvider"])
    texts = json.loads(args.texts.read_text(encoding="utf-8"))

    references = []
    for text in texts:
        model_input = model_text(text)
        tokenizer.no_truncation()
        tokens = len(tokenizer.encode(model_input, add_special_tokens=False).ids)
        tokenizer.enable_truncation(MAX_TOKENS)
        vector = embed(session, tokenizer.encode(model_input).ids)
        references.append({"tokens": tokens, "vec": [float(f"{x:.7g}") for x in vector.astype(np.float64)]})

    output = {
        "model": build.SOURCE_REPO,
        "revision": build.SOURCE_REVISION,
        "texts": args.texts.name,
        "references": references,
    }
    args.out.write_text(json.dumps(output, indent=1) + "\n", encoding="utf-8")
    print(f"{len(references)} reference vectors written to {args.out}")


if __name__ == "__main__":
    main()
