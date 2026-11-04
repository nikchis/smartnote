"""Checks a prepared model directory against the source model (design spec 5.2 p.5, 12.3).

- the manifest matches the files, the model id, the source revision and the vocabulary size;
- the pruned tokenizer splits the corpus exactly like the source one;
- embeddings of the prepared model have cosine >= MIN_COSINE to the source model's.

The corpus matches search/model-tool/tests/parity.rs. Exits with 1 on any violation.

Usage: uv run verify.py --sources <download cache dir> --model-dir <model dir>
"""

import argparse
import json
import subprocess
import sys
from pathlib import Path

import numpy as np
import onnx
import onnxruntime as ort
from tokenizers import Tokenizer

import build

REPO_ROOT = Path(__file__).resolve().parents[2]
EXTENSIONS = {".go", ".ts", ".svelte", ".md", ".yml", ".yaml", ".json", ".css", ".html", ".sh"}
EXCLUDED_PATH_PARTS = ("testdata", "package-lock")
LINES_PER_FRAGMENT = 15
FIXED_TEXTS = (
    "Купить молоко, хлеб и яйца; позвонить маме в субботу.",
    "Рецепт блинов: 2 яйца, 500 мл молока, 250 г муки, щепотка соли.",
    "Ёжик в тумане — «мультфильм» 1975 года, режиссёр Юрий Норштейн.",
    "Meeting notes: discuss the Q3 roadmap, budget and hiring plan.",
    "Pancakes recipe: whisk eggs with milk, add flour, rest 20 minutes.",
    "The quick brown fox jumps over the lazy dog. Café, naïve, Straße, Øresund.",
    "Смешанный текст: deploy в prod после code review, см. PR #42 → master.",
    "Цена 1 999,50 ₽ ≈ $25 ± 5 %; температура −5 °C; ✓ готово ✗ отменено 🚀",
    "if (a && b || !c) { x[i] = y->z; }",
    "let re = /^[a-z]{3,}$/u;",
    "func (s *Store) Get(ctx context.Context, id uuid.UUID) (*Note, error) {",
    "SELECT uuid, info FROM note WHERE deleted_at IS NULL ORDER BY updated_at DESC;",
)
MIN_COSINE = 0.9999
MIN_COSINE_TEXTS = 150
MAX_TOKENS = 512
PASSAGE_PREFIX = "passage: "
MODEL_FILES = ("model.onnx", "tokenizer.json", "tokenizer.kit")


def is_corpus_file(path: str) -> bool:
    return Path(path).suffix in EXTENSIONS and not any(part in path for part in EXCLUDED_PATH_PARTS)


def fragments(text: str) -> list[str]:
    lines = text.split("\n")
    chunks = ("\n".join(lines[i : i + LINES_PER_FRAGMENT]) for i in range(0, len(lines), LINES_PER_FRAGMENT))
    return [chunk for chunk in chunks if chunk.strip()]


def corpus() -> list[str]:
    listing = subprocess.run(
        ["git", "-C", str(REPO_ROOT), "ls-files"], check=True, capture_output=True, text=True, encoding="utf-8"
    ).stdout
    texts = list(FIXED_TEXTS)
    for path in filter(is_corpus_file, listing.splitlines()):
        texts.extend(fragments((REPO_ROOT / path).read_bytes().decode("utf-8")))
    return texts


def check_files(model_dir: Path) -> list[str]:
    manifest = json.loads((model_dir / "manifest.json").read_text(encoding="utf-8"))
    failures = []
    expected = {"model": build.MODEL_ID, "source_revision": build.SOURCE_REVISION, "vocab": build.EXPECTED_VOCAB}
    for key, value in expected.items():
        if manifest.get(key) != value:
            failures.append(f"manifest {key} = {manifest.get(key)!r}, expected {value!r}")
    if sorted(manifest.get("files", {})) != sorted(MODEL_FILES):
        failures.append(f"manifest files {sorted(manifest.get('files', {}))}, expected {sorted(MODEL_FILES)}")
    for name in MODEL_FILES:
        path = model_dir / name
        actual = build.sha256_file(path) if path.exists() else "missing"
        print(f"{name}: {path.stat().st_size if path.exists() else 0} bytes, sha256 {actual}")
        if actual != manifest.get("files", {}).get(name):
            failures.append(f"{name}: SHA-256 {actual} differs from the manifest")
    return failures


def check_vocab(model_dir: Path, pruned: Tokenizer) -> list[str]:
    failures = []
    size = pruned.get_vocab_size(with_added_tokens=True)
    graph = onnx.load(model_dir / "model.onnx").graph
    rows = {init.name: init.dims[0] for init in graph.initializer}.get(f"{build.EMBEDDING}.int8")
    print(f"vocab: tokenizer {size}, embedding rows {rows}")
    if size != build.EXPECTED_VOCAB or rows != build.EXPECTED_VOCAB:
        failures.append(f"vocab: tokenizer {size}, embedding rows {rows}, expected {build.EXPECTED_VOCAB}")
    return failures


def check_tokenization(original: Tokenizer, pruned: Tokenizer, texts: list[str]) -> list[str]:
    mismatches = []
    for text in texts:
        source = original.encode(text).tokens
        result = pruned.encode(text)
        if result.tokens != source or [pruned.id_to_token(i) for i in result.ids] != source:
            mismatches.append(text)
    print(f"tokenization: {len(texts)} texts, {len(mismatches)} mismatches")
    return [f"tokenization differs: {text[:80]!r}" for text in mismatches[:5]] + (
        [f"tokenization differs on {len(mismatches)} texts"] if mismatches else []
    )


def embed(session: ort.InferenceSession, ids: list[int]) -> np.ndarray:
    """Mean-pooled, L2-normalized embedding of one sequence."""
    feeds = {
        "input_ids": np.array([ids], dtype=np.int64),
        "attention_mask": np.ones((1, len(ids)), dtype=np.int64),
        "token_type_ids": np.zeros((1, len(ids)), dtype=np.int64),
    }
    wanted = {i.name for i in session.get_inputs()}
    [hidden] = session.run(["last_hidden_state"], {k: v for k, v in feeds.items() if k in wanted})
    pooled = hidden[0].mean(axis=0)
    return pooled / np.linalg.norm(pooled)


def check_cosine(
    sources: dict[str, Path], model_dir: Path, original: Tokenizer, pruned: Tokenizer, texts: list[str]
) -> list[str]:
    reference = ort.InferenceSession(str(sources["model.onnx"]), providers=["CPUExecutionProvider"])
    prepared = ort.InferenceSession(str(model_dir / "model.onnx"), providers=["CPUExecutionProvider"])
    for tokenizer in (original, pruned):
        tokenizer.enable_truncation(MAX_TOKENS)
    cosines = np.array(
        [
            float(
                embed(reference, original.encode(PASSAGE_PREFIX + text).ids)
                @ embed(prepared, pruned.encode(PASSAGE_PREFIX + text).ids)
            )
            for text in texts
        ]
    )
    print(f"cosine: {len(cosines)} texts, min {cosines.min():.7f}, mean {cosines.mean():.7f}")
    failures = []
    if len(cosines) < MIN_COSINE_TEXTS:
        failures.append(f"cosine checked on {len(cosines)} texts, need {MIN_COSINE_TEXTS}")
    if cosines.min() < MIN_COSINE:
        failures.append(f"cosine {cosines.min():.7f} < {MIN_COSINE} on {texts[int(cosines.argmin())][:80]!r}")
    return failures


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--sources", type=Path, required=True, help="download cache for the source files")
    parser.add_argument("--model-dir", type=Path, required=True, help="prepared model directory")
    args = parser.parse_args()

    sources = build.fetch_sources(args.sources)
    original = Tokenizer.from_file(str(sources["tokenizer.json"]))
    pruned = Tokenizer.from_file(str(args.model_dir / "tokenizer.json"))
    texts = corpus()

    failures = check_files(args.model_dir)
    failures += check_vocab(args.model_dir, pruned)
    failures += check_tokenization(original, pruned, texts)
    failures += check_cosine(sources, args.model_dir, original, pruned, texts)
    for failure in failures:
        print(f"FAIL: {failure}", file=sys.stderr)
    if failures:
        sys.exit(1)
    print("OK")


if __name__ == "__main__":
    main()
