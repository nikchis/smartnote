//! The worker process end to end (spec §6.2, §7, §10.2 p. 1): the test writes frames to the
//! stdin of `smartnote-search` and reads its replies.
//!
//! Tests marked `#[ignore]` need the prepared model and the ONNX Runtime library:
//! `SMARTNOTE_MODEL_DIR=$PWD/build/model SMARTNOTE_ORT_LIB=$PWD/build/ort/libonnxruntime.so.1.31.0`
//! (absolute paths).

mod common;

use std::time::Duration;

use common::{Worker, model_paths, note, required_model_paths, upsert_marked};
use worker::protocol::{ErrorCode, Hit, PROTO, Reply, Request, Source, State};
#[cfg(target_os = "linux")]
use worker::sandbox::SandboxLevel;

/// The semantic state of a `ready` reply; panics on any other reply.
fn ready_semantic(reply: &Reply) -> State {
    match reply {
        Reply::Ready { semantic, .. } => *semantic,
        other => panic!("expected ready, got {}", kind(other)),
    }
}

/// The type of a reply, for messages that must not print its content.
fn kind(reply: &Reply) -> &'static str {
    match reply {
        Reply::Ready { .. } => "ready",
        Reply::Ok { .. } => "ok",
        Reply::Result { .. } => "result",
        Reply::Embedded { .. } => "embedded",
        Reply::Status { .. } => "status",
        Reply::Error { .. } => "error",
    }
}

/// Searches and returns the hits of `result{req}`, skipping other replies.
fn search(w: &mut Worker, req: u64, query: &str) -> Vec<Hit> {
    w.send(&Request::Search {
        req,
        query: query.into(),
        limit: 20,
    });
    w.recv_until(|r| match r {
        Reply::Result { req: got, hits } if got == req => Some(hits),
        _ => None,
    })
}

/// Asserts an `error` reply with `code` and no request number.
fn assert_error(reply: &Reply, code: ErrorCode) {
    match reply {
        Reply::Error {
            req: None,
            code: got,
            ..
        } => assert_eq!(*got, code),
        other => panic!("expected error {code:?}, got {}", kind(other)),
    }
}

#[test]
fn hello_wrong_proto_exits() {
    let mut w = Worker::start();
    let parent_pid = w.parent_pid;
    w.send(&Request::Hello {
        proto: PROTO + 1,
        model_dir: "/nonexistent".into(),
        ort_lib: "/nonexistent".into(),
        threads: 1,
        parent_pid,
    });

    assert_error(&w.recv(), ErrorCode::UnsupportedProto);
    let (status, _) = w.wait();
    assert_eq!(status.code(), Some(2));
}

#[test]
fn bad_frame_exits() {
    // A body that is no CBOR item, a frame over 16 MiB, a valid message out of place.
    let oversized = 0x0100_0001_u32.to_le_bytes().to_vec();
    let mut garbage = 2_u32.to_le_bytes().to_vec();
    garbage.extend_from_slice(b"\xff\xff");
    let mut second_hello = Vec::new();
    {
        let body = worker::protocol::encode(&Request::Hello {
            proto: PROTO,
            model_dir: "/nonexistent".into(),
            ort_lib: "/nonexistent".into(),
            threads: 1,
            parent_pid: 1,
        })
        .expect("encode");
        worker::protocol::write_frame(&mut second_hello, &body).expect("frame");
    }
    for (case, frame) in [
        ("garbage", &garbage),
        ("oversized", &oversized),
        ("second hello", &second_hello),
    ] {
        let mut w = Worker::start();
        assert_eq!(ready_semantic(&w.hello(None)), State::Unavailable, "{case}");
        w.send_raw(frame);

        assert_error(&w.recv(), ErrorCode::BadRequest);
        let (status, _) = w.wait();
        assert_eq!(status.code(), Some(3), "{case}");
    }

    // Before `hello`.
    let mut w = Worker::start();
    w.send_raw(&garbage);
    assert_error(&w.recv(), ErrorCode::BadRequest);
    let (status, _) = w.wait();
    assert_eq!(status.code(), Some(3), "garbage before hello");
}

#[test]
fn eof_exits_zero() {
    let w = Worker::start();
    let (status, _) = w.finish();
    assert_eq!(status.code(), Some(0), "EOF before hello");

    let mut w = Worker::start();
    w.hello(None);
    upsert_marked(&mut w, 3, "eof", 1);
    let (status, _) = w.finish();
    assert_eq!(status.code(), Some(0), "EOF after hello");
}

#[test]
fn upsert_search_without_model() {
    let mut w = Worker::start();
    let ready = w.hello(None);
    let Reply::Ready {
        worker,
        index,
        lexical,
        semantic,
        ..
    } = &ready
    else {
        panic!("expected ready, got {}", kind(&ready));
    };
    assert_eq!(worker, env!("CARGO_PKG_VERSION"));
    assert_eq!(index.len(), 32, "index version id");
    assert_eq!(*lexical, State::Ready);
    assert_eq!(*semantic, State::Unavailable);

    w.send(&Request::Upsert {
        req: 1,
        notes: vec![
            note(1, 1, "Код", "fn main() {\n    if a == b { return; }\n}"),
            note(
                2,
                2,
                "Безопасность",
                "Менеджер паролей\nдвухфакторная защита",
            ),
            note(3, 3, "Покупки", "молоко\nхлеб"),
        ],
    });
    assert!(matches!(w.recv(), Reply::Ok { req: 1 }));
    match w.recv() {
        Reply::Status {
            notes,
            queued,
            lexical,
            semantic,
        } => {
            assert_eq!((notes, queued), (3, 0));
            assert_eq!(lexical, State::Ready);
            assert_eq!(semantic, State::Unavailable);
        }
        other => panic!("expected status, got {}", kind(&other)),
    }

    let hits = search(&mut w, 2, "==");
    assert_eq!(hits.len(), 1, "one line has ==");
    assert_eq!((hits[0].line, hits[0].text.as_str()), (2, "=="));
    assert_eq!(hits[0].sources, [Source::Substring]);

    let hits = search(&mut w, 3, "пароли");
    assert!(!hits.is_empty(), "the word form finds паролей");
    assert_eq!(hits[0].note, note(2, 0, "", "").id);
    assert_eq!(hits[0].line, 1);
    assert!(hits[0].sources.contains(&Source::Words));

    w.send(&Request::Remove {
        req: 4,
        items: vec![worker::protocol::RemovalIn {
            id: note(2, 0, "", "").id,
            rev: 4,
        }],
    });
    w.wait_ok(4);
    assert!(
        search(&mut w, 5, "пароли").is_empty(),
        "the note is removed"
    );

    w.send(&Request::Reset { req: 6 });
    w.wait_ok(6);
    assert!(search(&mut w, 7, "==").is_empty(), "the index is reset");

    let (status, _) = w.finish();
    assert_eq!(status.code(), Some(0));
}

#[cfg(target_os = "linux")]
#[test]
fn ready_reports_sandbox_full() {
    let mut w = Worker::start();
    match w.hello(model_paths()) {
        Reply::Ready { sandbox, .. } => assert_eq!(sandbox, SandboxLevel::Full),
        other => panic!("expected ready, got {}", kind(&other)),
    }
}

/// A writable descriptor that Smartnote's process leaks to the worker without `O_CLOEXEC` (as
/// GTK and `WebKit` leave some) is closed before the worker reads its first frame (spec §10.2
/// p. 1): Landlock and seccomp do not restrict descriptors that are already open. The test
/// passes the write end of a pipe as descriptor 7 and keeps the read end: it reads end of file
/// only once no process holds the write end. The worker is not dumpable, so its
/// `/proc/<pid>/fd` cannot be read from here.
#[cfg(target_os = "linux")]
#[test]
fn inherited_descriptors_are_closed() {
    use std::io::Read;
    use std::os::fd::AsRawFd;
    use std::os::unix::process::CommandExt;
    use std::process::Command;
    use std::sync::mpsc;
    use std::thread;

    const LEAKED_FD: libc::c_int = 7;

    // Both ends are close-on-exec: only the copy made below reaches the worker.
    let (mut reader, writer) = std::io::pipe().expect("pipe");
    let write_end = writer.as_raw_fd();
    let mut cmd = Command::new(common::WORKER);
    // SAFETY: the closure runs in the child between fork and exec and calls only `dup2`, which
    // is async-signal-safe; `write_end` is a plain integer of a descriptor open in the parent
    // until after the spawn. `dup2` leaves the new descriptor without `FD_CLOEXEC`.
    #[allow(unsafe_code, reason = "dup2 in the forked child before exec")]
    unsafe {
        cmd.pre_exec(move || {
            if libc::dup2(write_end, LEAKED_FD) == -1 {
                return Err(std::io::Error::last_os_error());
            }
            Ok(())
        });
    }
    let mut w = Worker::spawn(cmd, false);
    drop(writer);
    assert!(
        matches!(w.hello(None), Reply::Ready { .. }),
        "expected ready"
    );

    let (tx, rx) = mpsc::channel();
    thread::spawn(move || {
        let mut buf = [0u8; 1];
        let _ = tx.send(reader.read(&mut buf).map_err(|e| e.kind()));
    });
    let read = rx.recv_timeout(Duration::from_secs(10));
    assert_eq!(
        read,
        Ok(Ok(0)),
        "the worker still holds the inherited descriptor {LEAKED_FD}"
    );

    let (status, stderr) = w.finish();
    assert_eq!(status.code(), Some(0));
    assert!(
        !stderr.contains("descriptors besides"),
        "stderr reports open descriptors: {stderr}"
    );
}

/// A paused index thread collects 20 searches: only the last one runs (spec §3.3 p. 7).
#[cfg(feature = "test-probes")]
#[test]
fn search_latest_wins() {
    let mut w = Worker::start();
    w.hello(None);
    let next = upsert_marked(&mut w, 5, "latest", 1);

    w.send(&Request::DebugPause { ms: 500 });
    let first = next;
    let last = first + 19;
    for req in first..=last {
        w.send(&Request::Search {
            req,
            query: "latest".into(),
            limit: 20,
        });
    }

    let mut answered = Vec::new();
    while answered.len() < 20 {
        if let Reply::Result { req, hits } = w.recv() {
            assert_eq!(
                !hits.is_empty(),
                req == last,
                "request {req}: only the last search runs"
            );
            answered.push(req);
        }
    }
    answered.sort_unstable();
    assert_eq!(answered, (first..=last).collect::<Vec<_>>());
}

/// No note, name or query text reaches stderr, not even on a protocol error (spec §6.2,
/// §12.5).
#[test]
fn stderr_has_no_note_text() {
    const MARKER: &str = "ZQXMARKERQZX";
    let mut w = Worker::start();
    w.hello(model_paths());
    let first = upsert_marked(&mut w, 200, MARKER, 1);
    for (req, i) in (first..).zip(0..20) {
        search(&mut w, req, &format!("{MARKER} {i}"));
    }
    // A search with the marker inside a malformed message.
    let mut body = b"\xa4\x61t\x66search\x63req\x6c".to_vec();
    body.extend_from_slice(MARKER.as_bytes());
    w.send_body(&body);

    let (status, stderr) = w.wait();
    assert_eq!(status.code(), Some(3));
    assert!(!stderr.contains(MARKER), "stderr: {stderr}");
    assert!(!stderr.contains("ZQXMARKER"), "stderr: {stderr}");
}

/// An `upsert` without an embedding record gets one (`embedded` with its `rev`); sending the
/// record back with the same text computes nothing.
#[test]
#[ignore = "needs SMARTNOTE_MODEL_DIR and SMARTNOTE_ORT_LIB: task model:fetch, task ort:fetch"]
fn embedded_roundtrip() {
    let mut w = Worker::start();
    assert_eq!(
        ready_semantic(&w.hello(Some(required_model_paths()))),
        State::Loading
    );
    w.recv_until(|r| match r {
        Reply::Status { semantic, .. } => {
            assert_ne!(semantic, State::Unavailable, "the model loads");
            (semantic == State::Ready).then_some(())
        }
        _ => None,
    });

    let mut n = note(
        1,
        5,
        "Отпуск",
        "Взять палатку\n\nКупить билеты на поезд до Казани",
    );
    w.send(&Request::Upsert {
        req: 1,
        notes: vec![n.clone()],
    });
    let emb = w.recv_until(|r| match r {
        Reply::Embedded { id, rev, emb } => {
            assert_eq!((id.as_str(), rev), (n.id.as_str(), 5));
            Some(emb)
        }
        _ => None,
    });
    let queued = w.recv_until(|r| match r {
        Reply::Status { queued, .. } => Some(queued),
        _ => None,
    });
    assert_eq!(queued, 0, "status after embedded");

    n.rev = 6;
    n.emb = Some(emb);
    w.send(&Request::Upsert {
        req: 2,
        notes: vec![n],
    });
    assert!(matches!(w.recv(), Reply::Ok { req: 2 }));
    match w.recv() {
        Reply::Status { notes, queued, .. } => assert_eq!((notes, queued), (1, 0)),
        other => panic!("expected status, got {}", kind(&other)),
    }
    assert!(
        w.try_recv(Duration::from_millis(500)).is_none(),
        "no new embedded"
    );
    let (status, _) = w.finish();
    assert_eq!(status.code(), Some(0));
}

/// A Russian recipe is found first by an English query (spec §6.7).
#[test]
#[ignore = "needs SMARTNOTE_MODEL_DIR and SMARTNOTE_ORT_LIB: task model:fetch, task ort:fetch"]
fn semantic_cross_lingual() {
    let mut w = Worker::start();
    w.hello(Some(required_model_paths()));
    let notes = vec![
        note(
            1,
            1,
            "Блины",
            "Pancakes: взбить 2 яйца с 500 мл молока, добавить 250 г муки и щепотку соли.\nЖарить на горячей сковороде с двух сторон.",
        ),
        note(
            2,
            1,
            "Встреча",
            "Обсудить бюджет на третий квартал и план найма",
        ),
        note(3, 1, "Машина", "Заменить масло и воздушный фильтр"),
        note(4, 1, "Код", "if (a == b) { return x; }"),
    ];
    w.send(&Request::Upsert { req: 1, notes });
    w.wait_ok(1);
    let mut embedded = 0;
    while embedded < 4 {
        if let Reply::Embedded { .. } = w.recv() {
            embedded += 1;
        }
    }

    let hits = search(&mut w, 2, "recipe for pancakes");
    assert!(!hits.is_empty(), "the recipe is found");
    assert_eq!(hits[0].note, note(1, 0, "", "").id);
    assert!(hits[0].sources.contains(&Source::Semantic));
    let (status, _) = w.finish();
    assert_eq!(status.code(), Some(0));
}

/// ONNX Runtime messages below errors never reach stderr, whatever `ORT_LOG` says.
#[test]
#[ignore = "needs SMARTNOTE_MODEL_DIR and SMARTNOTE_ORT_LIB: task model:fetch, task ort:fetch"]
fn ort_log_stays_off_stderr() {
    let mut cmd = std::process::Command::new(common::WORKER);
    cmd.env("ORT_LOG", "verbose");
    let mut w = Worker::spawn(cmd, false);
    w.hello(Some(required_model_paths()));
    w.send(&Request::Upsert {
        req: 1,
        notes: vec![note(1, 1, "Заметка", "строка для эмбеддинга")],
    });
    w.recv_until(|r| matches!(r, Reply::Embedded { .. }).then_some(()));
    search(&mut w, 2, "строка для поиска");

    let (status, stderr) = w.finish();
    assert_eq!(status.code(), Some(0));
    // The worker's own timing line is the only one.
    let foreign: Vec<&str> = stderr
        .lines()
        .filter(|l| !l.starts_with("model loaded in "))
        .collect();
    assert!(foreign.is_empty(), "stderr: {foreign:#?}");
}

/// An ONNX Runtime library with the pinned digest that the loader still cannot load (a
/// dependency of it is missing or broken, as `onnxruntime.dll` without the Visual C++
/// Redistributable on Windows) leaves semantic search unavailable with the reason in the log,
/// and the worker keeps serving full-text search. Here a broken `libstdc++.so.6` lies beside
/// the copy of the library, where its `RUNPATH` (`$ORIGIN`) finds it first.
#[cfg(target_os = "linux")]
#[test]
#[ignore = "needs SMARTNOTE_MODEL_DIR and SMARTNOTE_ORT_LIB: task model:fetch, task ort:fetch"]
fn ort_load_failure_keeps_full_text_search() {
    let (model_dir, ort_lib) = required_model_paths();
    let dir = common::TempDir::new("ort-broken-dependency");
    let lib = dir
        .path()
        .join(ort_lib.file_name().expect("library file name"));
    std::fs::copy(&ort_lib, &lib).expect("copy the library");
    std::fs::write(dir.path().join("libstdc++.so.6"), b"not a library").expect("write");

    let mut w = Worker::start();
    assert_eq!(
        ready_semantic(&w.hello(Some((model_dir, lib)))),
        State::Unavailable
    );
    w.send(&Request::Upsert {
        req: 1,
        notes: vec![note(1, 1, "Код", "if (a == b) {}")],
    });
    w.wait_ok(1);
    let hits = search(&mut w, 2, "==");
    assert_eq!(hits.len(), 1, "full-text search works");

    let (status, stderr) = w.finish();
    assert_eq!(status.code(), Some(0));
    assert!(
        stderr.contains("semantic search unavailable: loading the ONNX Runtime library failed"),
        "stderr: {stderr}"
    );
}
