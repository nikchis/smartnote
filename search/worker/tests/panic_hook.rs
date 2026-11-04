//! The panic hook reports the location only: no payload, so no note text (spec §6.2).
#![cfg(feature = "test-probes")]

use std::process::Command;

#[test]
fn panic_message_has_no_payload() {
    let out = Command::new(env!("CARGO_BIN_EXE_probe_zalloc"))
        .args(["panic", "МЕТКА-ж42"])
        .output()
        .expect("probe starts");
    let stderr = String::from_utf8_lossy(&out.stderr);

    assert!(stderr.contains("panic at "), "stderr: {stderr}");
    assert!(!stderr.contains("МЕТКА"), "stderr: {stderr}");
    assert!(!stderr.contains('ж'), "stderr: {stderr}");
    assert!(!stderr.contains("char boundary"), "stderr: {stderr}");
    #[cfg(unix)]
    {
        use std::os::unix::process::ExitStatusExt;
        assert_eq!(out.status.signal(), Some(libc::SIGABRT), "{:?}", out.status);
    }
    #[cfg(not(unix))]
    assert!(!out.status.success(), "{:?}", out.status);
}
