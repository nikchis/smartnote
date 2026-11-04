//! Model files and the ONNX Runtime library, checked against the SHA-256 digests pinned at build
//! time (spec §6.1, §6.9, §10.2 p.1).

use std::collections::BTreeMap;
use std::fs::{self, File};
use std::io::{self, Read};
use std::path::{Path, PathBuf};

use sha2::{Digest, Sha256};

use super::ModelError;

mod pins {
    include!(concat!(env!("OUT_DIR"), "/pins.rs"));
}

const MANIFEST: &str = "manifest.json";
const MODEL_ONNX: &str = "model.onnx";
const TOKENIZER_KIT: &str = "tokenizer.kit";
const ORT_LIBRARY: &str = "onnxruntime";

/// Digests of the model files.
struct ModelPins {
    manifest: [u8; 32],
    onnx: [u8; 32],
    kit: [u8; 32],
}

const MODEL_PINS: ModelPins = ModelPins {
    manifest: pins::MANIFEST_SHA256,
    onnx: pins::MODEL_ONNX_SHA256,
    kit: pins::TOKENIZER_KIT_SHA256,
};

/// The parts of `manifest.json` that identify the model (spec §4.2).
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Manifest {
    /// SHA-256 of `model.onnx`.
    pub onnx_sha256: [u8; 32],
    /// SHA-256 of `tokenizer.kit`.
    pub kit_sha256: [u8; 32],
}

impl Manifest {
    /// The manifest of the model pinned at build time: the only one [`ModelFiles::read`]
    /// accepts. Known without reading the files, so the index version id
    /// ([`super::index_id`]) is the same whether or not the model is available.
    #[must_use]
    pub fn pinned() -> Self {
        Self {
            onnx_sha256: MODEL_PINS.onnx,
            kit_sha256: MODEL_PINS.kit,
        }
    }
}

/// The model files, read into memory and verified.
pub struct ModelFiles {
    /// `model.onnx`.
    pub onnx: Vec<u8>,
    /// `tokenizer.kit`.
    pub kit: Vec<u8>,
    /// `manifest.json`.
    pub manifest: Manifest,
}

impl ModelFiles {
    /// Reads `manifest.json`, `model.onnx` and `tokenizer.kit` from `dir`; each must have the
    /// SHA-256 pinned at build time from `model.lock.json`, and the manifest must name the
    /// digests of the other two.
    ///
    /// # Errors
    ///
    /// [`ModelError::Io`] when a file cannot be read, [`ModelError::Hash`] when a digest
    /// differs, [`ModelError::Manifest`] when the manifest is malformed.
    pub fn read(dir: &Path) -> Result<Self, ModelError> {
        Self::read_pinned(dir, &MODEL_PINS)
    }

    fn read_pinned(dir: &Path, pins: &ModelPins) -> Result<Self, ModelError> {
        let manifest = read_verified(dir, MANIFEST, &pins.manifest)?;
        let onnx = read_verified(dir, MODEL_ONNX, &pins.onnx)?;
        let kit = read_verified(dir, TOKENIZER_KIT, &pins.kit)?;
        let manifest = parse_manifest(&manifest)?;
        if manifest.onnx_sha256 != pins.onnx || manifest.kit_sha256 != pins.kit {
            return Err(ModelError::Manifest);
        }
        Ok(Self {
            onnx,
            kit,
            manifest,
        })
    }
}

fn read_verified(dir: &Path, name: &'static str, pin: &[u8; 32]) -> Result<Vec<u8>, ModelError> {
    let data = fs::read(dir.join(name))?;
    if Sha256::digest(&data).as_slice() == pin {
        Ok(data)
    } else {
        Err(ModelError::Hash { file: name })
    }
}

fn parse_manifest(data: &[u8]) -> Result<Manifest, ModelError> {
    #[derive(serde::Deserialize)]
    struct Raw {
        files: BTreeMap<String, String>,
    }
    let raw: Raw = serde_json::from_slice(data).map_err(|_| ModelError::Manifest)?;
    let digest = |name: &str| {
        raw.files
            .get(name)
            .and_then(|hex| parse_hex(hex))
            .ok_or(ModelError::Manifest)
    };
    Ok(Manifest {
        onnx_sha256: digest(MODEL_ONNX)?,
        kit_sha256: digest(TOKENIZER_KIT)?,
    })
}

/// Parses 64 lower-case hex digits.
fn parse_hex(hex: &str) -> Option<[u8; 32]> {
    let digits = hex.as_bytes();
    if digits.len() != 64 {
        return None;
    }
    let mut out = [0; 32];
    let (pairs, _) = digits.as_chunks::<2>();
    for (byte, &[high, low]) in out.iter_mut().zip(pairs) {
        *byte = hex_digit(high)? << 4 | hex_digit(low)?;
    }
    Some(out)
}

fn hex_digit(c: u8) -> Option<u8> {
    match c {
        b'0'..=b'9' => Some(c - b'0'),
        b'a'..=b'f' => Some(c - b'a' + 10),
        _ => None,
    }
}

/// An ONNX Runtime library file whose SHA-256 matched the digest pinned for the build target.
#[derive(Debug)]
pub struct OrtLibrary {
    path: PathBuf,
}

impl OrtLibrary {
    /// Reads the library at the absolute `path`, compares its SHA-256 with the digest pinned
    /// at build time from `ort.lock.json` and closes the file.
    ///
    /// # Errors
    ///
    /// [`ModelError::RelativePath`] for a relative path, [`ModelError::UnsupportedTarget`]
    /// when no library is pinned for the build target, [`ModelError::Io`] when the file cannot
    /// be read, [`ModelError::Hash`] when the digest differs.
    pub fn verify(path: &Path) -> Result<Self, ModelError> {
        Self::verify_pinned(path, pins::ORT_LIB_SHA256.as_ref())
    }

    fn verify_pinned(path: &Path, pin: Option<&[u8; 32]>) -> Result<Self, ModelError> {
        if !path.is_absolute() {
            return Err(ModelError::RelativePath);
        }
        let pin = pin.ok_or(ModelError::UnsupportedTarget)?;
        if file_sha256(path)?.as_slice() == pin {
            Ok(Self {
                path: path.to_path_buf(),
            })
        } else {
            Err(ModelError::Hash { file: ORT_LIBRARY })
        }
    }

    /// Path of the verified library.
    #[must_use]
    pub fn path(&self) -> &Path {
        &self.path
    }
}

/// SHA-256 of the file at `path`, read in blocks; the file is closed on return.
fn file_sha256(path: &Path) -> io::Result<[u8; 32]> {
    let mut file = File::open(path)?;
    let mut hasher = Sha256::new();
    let mut block = vec![0; 1 << 16];
    loop {
        match file.read(&mut block) {
            Ok(0) => return Ok(hasher.finalize().into()),
            Ok(n) => hasher.update(&block[..n]),
            Err(err) if err.kind() == io::ErrorKind::Interrupted => {}
            Err(err) => return Err(err),
        }
    }
}

/// Whether the CPU can run the ONNX Runtime build: AVX2 on `x86_64`, always on other
/// architectures (spec §6.9).
#[must_use]
pub fn has_avx2() -> bool {
    #[cfg(target_arch = "x86_64")]
    {
        std::arch::is_x86_feature_detected!("avx2")
    }
    #[cfg(not(target_arch = "x86_64"))]
    {
        true
    }
}

#[cfg(test)]
mod tests {
    use std::fmt::Write as _;
    use std::sync::atomic::{AtomicUsize, Ordering};

    use super::*;

    /// A fresh temporary directory, removed on drop.
    struct TempDir(PathBuf);

    impl TempDir {
        fn new() -> Self {
            static NEXT: AtomicUsize = AtomicUsize::new(0);
            let name = format!(
                "smartnote-embed-files-{}-{}",
                std::process::id(),
                NEXT.fetch_add(1, Ordering::Relaxed)
            );
            let dir = std::env::temp_dir().join(name);
            fs::create_dir(&dir).expect("create temp dir");
            Self(dir)
        }

        fn write(&self, name: &str, data: &[u8]) {
            fs::write(self.0.join(name), data).expect("write temp file");
        }
    }

    impl Drop for TempDir {
        fn drop(&mut self) {
            let _ = fs::remove_dir_all(&self.0);
        }
    }

    fn sha(data: &[u8]) -> [u8; 32] {
        Sha256::digest(data).into()
    }

    fn hex(digest: &[u8; 32]) -> String {
        digest.iter().fold(String::new(), |mut s, b| {
            write!(s, "{b:02x}").expect("String write");
            s
        })
    }

    const ONNX: &[u8] = b"onnx bytes";
    const KIT: &[u8] = b"kit bytes";

    /// A model directory with consistent files and the pins of its files.
    fn model_dir() -> (TempDir, ModelPins) {
        let dir = TempDir::new();
        let manifest = format!(
            r#"{{"files": {{"model.onnx": "{}", "tokenizer.kit": "{}"}}, "model": "test"}}"#,
            hex(&sha(ONNX)),
            hex(&sha(KIT))
        );
        dir.write(MANIFEST, manifest.as_bytes());
        dir.write(MODEL_ONNX, ONNX);
        dir.write(TOKENIZER_KIT, KIT);
        let pins = ModelPins {
            manifest: sha(manifest.as_bytes()),
            onnx: sha(ONNX),
            kit: sha(KIT),
        };
        (dir, pins)
    }

    #[test]
    fn model_files_read_verified() {
        let (dir, pins) = model_dir();

        let files = ModelFiles::read_pinned(&dir.0, &pins).expect("files match their pins");

        assert_eq!(files.onnx, ONNX);
        assert_eq!(files.kit, KIT);
        assert_eq!(
            files.manifest,
            Manifest {
                onnx_sha256: sha(ONNX),
                kit_sha256: sha(KIT)
            }
        );
    }

    #[test]
    fn model_files_hash_mismatch() {
        let (dir, pins) = model_dir();
        dir.write(MODEL_ONNX, b"onnx bytes, tampered");

        let err = ModelFiles::read_pinned(&dir.0, &pins).err();

        assert!(
            matches!(err, Some(ModelError::Hash { file: "model.onnx" })),
            "{err:?}"
        );
    }

    #[test]
    fn model_files_missing_file() {
        let (dir, pins) = model_dir();
        fs::remove_file(dir.0.join(TOKENIZER_KIT)).expect("remove");

        let err = ModelFiles::read_pinned(&dir.0, &pins).err();

        assert!(matches!(err, Some(ModelError::Io(_))), "{err:?}");
    }

    #[test]
    fn model_files_manifest_must_name_the_pinned_files() {
        let (dir, mut pins) = model_dir();
        let manifest = format!(
            r#"{{"files": {{"model.onnx": "{}", "tokenizer.kit": "{}"}}}}"#,
            hex(&sha(ONNX)),
            hex(&sha(b"another tokenizer"))
        );
        dir.write(MANIFEST, manifest.as_bytes());
        pins.manifest = sha(manifest.as_bytes());

        let err = ModelFiles::read_pinned(&dir.0, &pins).err();

        assert!(matches!(err, Some(ModelError::Manifest)), "{err:?}");
    }

    #[test]
    fn model_files_reject_malformed_manifest() {
        for manifest in [
            "not json",
            r#"{"files": {"model.onnx": "00"}}"#,
            r#"{"model": "no files"}"#,
        ] {
            let (dir, mut pins) = model_dir();
            dir.write(MANIFEST, manifest.as_bytes());
            pins.manifest = sha(manifest.as_bytes());

            let err = ModelFiles::read_pinned(&dir.0, &pins).err();

            assert!(
                matches!(err, Some(ModelError::Manifest)),
                "{manifest}: {err:?}"
            );
        }
    }

    #[test]
    fn model_files_use_the_pins_of_model_lock() {
        let lock = include_str!("../../../../model.lock.json");
        for (name, pin) in [
            (MANIFEST, MODEL_PINS.manifest),
            (MODEL_ONNX, MODEL_PINS.onnx),
            (TOKENIZER_KIT, MODEL_PINS.kit),
        ] {
            assert!(
                lock.contains(&format!("\"{name}\": \"{}\"", hex(&pin))),
                "{name} pin differs from model.lock.json"
            );
        }
    }

    #[test]
    fn pinned_manifest_names_the_pinned_files() {
        let manifest = Manifest::pinned();

        assert_eq!(manifest.onnx_sha256, MODEL_PINS.onnx);
        assert_eq!(manifest.kit_sha256, MODEL_PINS.kit);
    }

    #[test]
    fn ort_library_verified() {
        let dir = TempDir::new();
        dir.write("libonnxruntime.so", b"library");
        let path = dir.0.join("libonnxruntime.so");

        let lib = OrtLibrary::verify_pinned(&path, Some(&sha(b"library"))).expect("digest matches");

        assert_eq!(lib.path(), path);
    }

    #[test]
    fn ort_library_hash_mismatch() {
        let dir = TempDir::new();
        dir.write("libonnxruntime.so", b"not the pinned library");

        let err = OrtLibrary::verify(&dir.0.join("libonnxruntime.so")).err();

        assert!(
            matches!(
                err,
                Some(ModelError::Hash {
                    file: "onnxruntime"
                })
            ),
            "{err:?}"
        );
    }

    #[test]
    fn ort_library_missing() {
        let dir = TempDir::new();

        let err = OrtLibrary::verify(&dir.0.join("libonnxruntime.so")).err();

        assert!(matches!(err, Some(ModelError::Io(_))), "{err:?}");
    }

    #[test]
    fn ort_library_relative_path() {
        let err = OrtLibrary::verify(Path::new("libonnxruntime.so")).err();

        assert!(matches!(err, Some(ModelError::RelativePath)), "{err:?}");
    }

    #[test]
    fn ort_library_without_pin_for_target() {
        let dir = TempDir::new();
        dir.write("libonnxruntime.so", b"library");

        let err = OrtLibrary::verify_pinned(&dir.0.join("libonnxruntime.so"), None).err();

        assert!(
            matches!(err, Some(ModelError::UnsupportedTarget)),
            "{err:?}"
        );
    }

    #[cfg(all(target_os = "linux", target_arch = "x86_64"))]
    #[test]
    fn ort_library_pin_of_linux_amd64() {
        let lock = include_str!("../../../../ort.lock.json");
        let pin = pins::ORT_LIB_SHA256.expect("linux-amd64 is pinned");

        assert!(lock.contains(&format!("\"lib_sha256\": \"{}\"", hex(&pin))));
        assert_eq!(
            hex(&pin),
            "469849977cb60744ea160927e1a3a0643fd4c55c0aff743251c6d293402e334e"
        );
    }
}
