//! `kitoken` must give the same token ids as HF `tokenizers` for the pruned tokenizer.
//!
//! The corpus matches `tools/model/verify.py`: 15-line fragments of the repository's
//! sources and docs plus fixed ru/en texts and code.

use std::env;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;

use kitoken::Kitoken;
use tokenizers::Tokenizer;

const EXTENSIONS: &[&str] = &[
    "go", "ts", "svelte", "md", "yml", "yaml", "json", "css", "html", "sh",
];
const EXCLUDED_PATH_PARTS: &[&str] = &["testdata", "package-lock"];
const LINES_PER_FRAGMENT: usize = 15;
const FIXED_TEXTS: &[&str] = &[
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
];

fn repo_root() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("../..")
        .canonicalize()
        .expect("the crate lives two levels below the repository root")
}

fn model_dir(root: &Path) -> PathBuf {
    let dir =
        env::var_os("SMARTNOTE_MODEL_DIR").map_or_else(|| "build/model".into(), PathBuf::from);
    root.join(dir)
}

fn is_corpus_file(path: &str) -> bool {
    let has_extension = Path::new(path)
        .extension()
        .and_then(|ext| ext.to_str())
        .is_some_and(|ext| EXTENSIONS.contains(&ext));
    has_extension && !EXCLUDED_PATH_PARTS.iter().any(|part| path.contains(part))
}

fn fragments(text: &str) -> Vec<String> {
    let lines: Vec<&str> = text.split('\n').collect();
    lines
        .chunks(LINES_PER_FRAGMENT)
        .map(|chunk| chunk.join("\n"))
        .filter(|fragment| !fragment.trim().is_empty())
        .collect()
}

fn corpus(root: &Path) -> Vec<String> {
    let output = Command::new("git")
        .arg("-C")
        .arg(root)
        .arg("ls-files")
        .output()
        .expect("git runs");
    assert!(output.status.success(), "git ls-files failed");
    let listing = String::from_utf8(output.stdout).expect("git prints UTF-8 paths");
    let mut texts: Vec<String> = FIXED_TEXTS.iter().map(ToString::to_string).collect();
    for path in listing.lines().filter(|path| is_corpus_file(path)) {
        let text = fs::read_to_string(root.join(path))
            .unwrap_or_else(|err| panic!("reading {path}: {err}"));
        texts.extend(fragments(&text));
    }
    texts
}

#[test]
#[ignore = "needs the prepared model in SMARTNOTE_MODEL_DIR (default build/model): task model:build or task model:fetch"]
fn kitoken_matches_hf_on_repo_corpus() {
    let root = repo_root();
    let dir = model_dir(&root);
    let kit_path = dir.join("tokenizer.kit");
    let json_path = dir.join("tokenizer.json");
    let kit = Kitoken::from_file(&kit_path)
        .unwrap_or_else(|err| panic!("loading {}: {err}", kit_path.display()));
    let hf = Tokenizer::from_file(&json_path)
        .unwrap_or_else(|err| panic!("loading {}: {err}", json_path.display()));
    let texts = corpus(&root);

    let mismatches: Vec<&String> = texts
        .iter()
        .filter(|text| {
            let expected = hf
                .encode(text.as_str(), false)
                .expect("HF encodes")
                .get_ids()
                .to_vec();
            let actual = kit.encode(text.as_str(), true).expect("kitoken encodes");
            actual != expected
        })
        .collect();

    println!("{} texts, {} mismatches", texts.len(), mismatches.len());
    assert!(texts.len() >= 150, "corpus too small: {}", texts.len());
    assert!(
        mismatches.is_empty(),
        "{} of {} texts differ, first: {:?}",
        mismatches.len(),
        texts.len(),
        mismatches.first()
    );
}
