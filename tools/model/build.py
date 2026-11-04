"""Prepares the Smartnote search model from intfloat/multilingual-e5-small.

Steps (design spec section 5.2): download the pinned source files and check their
SHA-256, prune the vocabulary to the "broad" character set, quantize the word
embedding table to int8 with a per-row scale, write model.onnx, tokenizer.json and
manifest.json. `model-tool convert` then adds tokenizer.kit to the manifest.

Usage: uv run build.py --sources <download cache dir> --out <model dir>
"""

import argparse
import hashlib
import json
import sys
import urllib.request
from pathlib import Path

import numpy as np
import onnx
from onnx import TensorProto, helper, numpy_helper

MODEL_ID = "e5s-ru-en-q8"
SOURCE_REPO = "intfloat/multilingual-e5-small"
SOURCE_REVISION = "614241f622f53c4eeff9890bdc4f31cfecc418b3"
SOURCE_BASE_URL = f"https://huggingface.co/{SOURCE_REPO}/resolve/{SOURCE_REVISION}/"
# Local name -> (path in the source repository, SHA-256).
SOURCE_FILES = {
    "model.onnx": (
        "onnx/model.onnx",
        "ca456c06b3a9505ddfd9131408916dd79290368331e7d76bb621f1cba6bc8665",
    ),
    "tokenizer.json": (
        "onnx/tokenizer.json",
        "0b44a9d7b51c3c62626640cda0e2c2f70fdacdc25bbbd68038369d14ebdf4c39",
    ),
}

# Characters a kept vocabulary piece may consist of (inclusive code point ranges).
BROAD_RANGES = (
    (0x2581, 0x2581),  # ▁, the SentencePiece word boundary
    (0x20, 0x7E),  # ASCII
    (0xA0, 0x24F),  # Latin-1, Latin Extended-A/B
    (0x1E00, 0x1EFF),  # Latin Extended Additional
    (0x370, 0x3FF),  # Greek
    (0x400, 0x52F),  # Cyrillic and its supplement
    (0x2000, 0x206F),  # General Punctuation
    (0x20A0, 0x20CF),  # Currency Symbols
    (0x2100, 0x23FF),  # letterlike, number forms, arrows, math operators, technical
    (0x2500, 0x27BF),  # box drawing, block elements, geometric shapes, dingbats
    (0xFE0F, 0xFE0F),  # emoji variation selector
    (0x1F300, 0x1FAFF),  # emoji
)
SPECIAL_IDS = range(4)  # <s>, <pad>, </s>, <unk> keep ids 0-3
EXPECTED_VOCAB = 149_642
EMBEDDING = "embeddings.word_embeddings.weight"
INT8_MAX = 127


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as file:
        while block := file.read(1 << 20):
            digest.update(block)
    return digest.hexdigest()


def fetch_sources(cache: Path) -> dict[str, Path]:
    """Returns local copies of SOURCE_FILES, downloading those missing or corrupt."""
    cache.mkdir(parents=True, exist_ok=True)
    paths = {}
    for name, (remote, expected) in SOURCE_FILES.items():
        path = cache / name
        if not path.exists() or sha256_file(path) != expected:
            download(SOURCE_BASE_URL + remote, path)
        actual = sha256_file(path)
        if actual != expected:
            sys.exit(f"{path}: SHA-256 {actual}, expected {expected}")
        paths[name] = path
    return paths


def download(url: str, path: Path) -> None:
    print(f"downloading {url}", flush=True)
    partial = path.with_name(path.name + ".part")
    with urllib.request.urlopen(url, timeout=60) as response, partial.open("wb") as file:
        while block := response.read(1 << 20):
            file.write(block)
    partial.replace(path)


def is_broad(piece: str) -> bool:
    return all(any(lo <= ord(ch) <= hi for lo, hi in BROAD_RANGES) for ch in piece)


def kept_ids(vocab: list) -> list[int]:
    """Ids of the pieces to keep, in their original order."""
    return [i for i, (piece, _score) in enumerate(vocab) if i in SPECIAL_IDS or is_broad(piece)]


def prune_tokenizer(tokenizer: dict, keep: list[int]) -> dict:
    """Returns tokenizer.json content with the Unigram vocabulary limited to `keep`."""
    model = tokenizer["model"]
    if model["type"] != "Unigram":
        raise ValueError(f"unexpected tokenizer model {model['type']}")
    new_id = {old: new for new, old in enumerate(keep)}
    pruned = dict(tokenizer)
    pruned["model"] = dict(model, unk_id=new_id[model["unk_id"]], vocab=[model["vocab"][i] for i in keep])
    pruned["added_tokens"] = [dict(token, id=new_id[token["id"]]) for token in tokenizer["added_tokens"]]
    post = tokenizer["post_processor"]
    if post["type"] != "TemplateProcessing":
        raise ValueError(f"unexpected post-processor {post['type']}")
    pruned["post_processor"] = dict(
        post,
        special_tokens={
            name: dict(special, ids=[new_id[i] for i in special["ids"]])
            for name, special in post["special_tokens"].items()
        },
    )
    return pruned


def quantize_rows(weights: np.ndarray) -> tuple[np.ndarray, np.ndarray]:
    """Symmetric int8 quantization with one float32 scale per row (shape [rows, 1])."""
    absmax = np.abs(weights).max(axis=1, keepdims=True)
    scale = np.where(absmax > 0, absmax / INT8_MAX, 1).astype(np.float32)
    quantized = np.clip(np.rint(weights / scale), -INT8_MAX, INT8_MAX).astype(np.int8)
    return quantized, scale


def prune_and_quantize_model(model: onnx.ModelProto, keep: list[int]) -> None:
    """Replaces Gather(W) of the word embeddings by Gather(Wq) -> Cast -> Mul(Gather(scale))."""
    graph = model.graph
    [table] = [init for init in graph.initializer if init.name == EMBEDDING]
    users = [node for node in graph.node if EMBEDDING in node.input]
    if len(users) != 1 or users[0].op_type != "Gather" or users[0].input[0] != EMBEDDING:
        raise ValueError(f"{EMBEDDING} must feed exactly one Gather as its data input")
    [gather] = users
    ids, output = gather.input[1], gather.output[0]

    weights = numpy_helper.to_array(table)[keep]
    quantized, scale = quantize_rows(weights)
    graph.initializer.remove(table)
    graph.initializer.extend(
        [
            numpy_helper.from_array(quantized, f"{EMBEDDING}.int8"),
            numpy_helper.from_array(scale, f"{EMBEDDING}.scale"),
        ]
    )
    replacement = [
        helper.make_node("Gather", [f"{EMBEDDING}.int8", ids], [f"{output}.int8"], name=f"{gather.name}.int8"),
        helper.make_node(
            "Cast", [f"{output}.int8"], [f"{output}.float"], name=f"{gather.name}.cast", to=TensorProto.FLOAT
        ),
        helper.make_node("Gather", [f"{EMBEDDING}.scale", ids], [f"{output}.scale"], name=f"{gather.name}.scale"),
        helper.make_node("Mul", [f"{output}.float", f"{output}.scale"], [output], name=f"{gather.name}.dequantize"),
    ]
    position = list(graph.node).index(gather)
    graph.node.remove(gather)
    for offset, node in enumerate(replacement):
        graph.node.insert(position + offset, node)
    onnx.checker.check_model(model)


def write_manifest(out: Path, names: list[str]) -> None:
    manifest = {
        "files": {name: sha256_file(out / name) for name in names},
        "model": MODEL_ID,
        "source_revision": SOURCE_REVISION,
        "vocab": EXPECTED_VOCAB,
    }
    (out / "manifest.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--sources", type=Path, required=True, help="download cache for the source files")
    parser.add_argument("--out", type=Path, required=True, help="output model directory")
    args = parser.parse_args()

    sources = fetch_sources(args.sources)
    tokenizer = json.loads(sources["tokenizer.json"].read_text(encoding="utf-8"))
    keep = kept_ids(tokenizer["model"]["vocab"])
    assert len(keep) == EXPECTED_VOCAB, f"kept {len(keep)} pieces, expected {EXPECTED_VOCAB}"

    args.out.mkdir(parents=True, exist_ok=True)
    pruned = prune_tokenizer(tokenizer, keep)
    (args.out / "tokenizer.json").write_text(
        json.dumps(pruned, ensure_ascii=False, separators=(",", ":")), encoding="utf-8"
    )
    model = onnx.load(sources["model.onnx"])
    prune_and_quantize_model(model, keep)
    onnx.save(model, args.out / "model.onnx")
    write_manifest(args.out, ["model.onnx", "tokenizer.json"])
    print(f"vocab {len(keep)}, written to {args.out}")


if __name__ == "__main__":
    main()
