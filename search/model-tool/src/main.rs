//! Model preparation helper: `model-tool convert <dir>` turns `<dir>/tokenizer.json`
//! into the `kitoken` format (`<dir>/tokenizer.kit`) and records its SHA-256 in
//! `<dir>/manifest.json`.

use std::env;
use std::error::Error;
use std::fmt::Write as _;
use std::fs;
use std::path::Path;
use std::process::ExitCode;

use kitoken::Kitoken;
use serde_json::Value;
use sha2::{Digest, Sha256};

const USAGE: &str = "usage: model-tool convert <model-dir>";
const TOKENIZER_JSON: &str = "tokenizer.json";
const TOKENIZER_KIT: &str = "tokenizer.kit";
const MANIFEST: &str = "manifest.json";

type Result<T> = std::result::Result<T, Box<dyn Error>>;

fn main() -> ExitCode {
    let args: Vec<String> = env::args().skip(1).collect();
    let [command, dir] = args.as_slice() else {
        eprintln!("{USAGE}");
        return ExitCode::from(2);
    };
    if command != "convert" {
        eprintln!("{USAGE}");
        return ExitCode::from(2);
    }
    match convert(Path::new(dir)) {
        Ok(()) => ExitCode::SUCCESS,
        Err(err) => {
            eprintln!("model-tool: {err}");
            ExitCode::FAILURE
        }
    }
}

/// Writes `tokenizer.kit` next to `tokenizer.json` and adds its digest to the manifest.
fn convert(dir: &Path) -> Result<()> {
    let json_path = dir.join(TOKENIZER_JSON);
    let kit_path = dir.join(TOKENIZER_KIT);
    let manifest_path = dir.join(MANIFEST);

    let tokenizer = Kitoken::from_tokenizers_file(&json_path)
        .map_err(|err| format!("converting {}: {err}", json_path.display()))?;
    let kit = tokenizer.to_vec();
    fs::write(&kit_path, &kit).map_err(|err| format!("writing {}: {err}", kit_path.display()))?;

    let manifest = fs::read_to_string(&manifest_path)
        .map_err(|err| format!("reading {}: {err}", manifest_path.display()))?;
    let updated = with_file_digest(&manifest, TOKENIZER_KIT, &sha256_hex(&kit))
        .map_err(|err| format!("updating {}: {err}", manifest_path.display()))?;
    fs::write(&manifest_path, updated)
        .map_err(|err| format!("writing {}: {err}", manifest_path.display()))?;
    Ok(())
}

/// Returns the manifest with `files[name] = digest`, keys sorted, two-space indent and a
/// trailing newline (the same layout `tools/model/build.py` writes).
fn with_file_digest(manifest: &str, name: &str, digest: &str) -> Result<String> {
    let mut root: Value = serde_json::from_str(manifest)?;
    let files = root
        .get_mut("files")
        .and_then(Value::as_object_mut)
        .ok_or("manifest has no \"files\" object")?;
    files.insert(name.to_owned(), Value::String(digest.to_owned()));
    let mut text = serde_json::to_string_pretty(&root)?;
    text.push('\n');
    Ok(text)
}

fn sha256_hex(data: &[u8]) -> String {
    Sha256::digest(data)
        .iter()
        .fold(String::with_capacity(64), |mut hex, byte| {
            // Writing to a String cannot fail.
            let _ = write!(hex, "{byte:02x}");
            hex
        })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn with_file_digest_adds_entry_and_sorts_keys() {
        let manifest = r#"{"vocab": 3, "files": {"model.onnx": "aa"}, "model": "m"}"#;

        let updated = with_file_digest(manifest, "tokenizer.kit", "bb").unwrap();

        assert_eq!(
            updated,
            "{\n  \"files\": {\n    \"model.onnx\": \"aa\",\n    \"tokenizer.kit\": \"bb\"\n  },\n  \"model\": \"m\",\n  \"vocab\": 3\n}\n"
        );
    }

    #[test]
    fn with_file_digest_rejects_manifest_without_files() {
        let err = with_file_digest(r#"{"model": "m"}"#, "tokenizer.kit", "bb").unwrap_err();

        assert!(err.to_string().contains("files"), "{err}");
    }

    #[test]
    fn sha256_hex_of_empty_input() {
        assert_eq!(
            sha256_hex(b""),
            "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
        );
    }
}
