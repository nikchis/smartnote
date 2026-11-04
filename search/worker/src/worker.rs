//! The worker process (spec §6.2, §7, §10.2 p. 1).
//!
//! `hello` is read on the calling thread while the process is still single-threaded; then, in
//! the order of spec §10.2 p. 1: version check, hardening with the parent pid, reading and
//! verifying the model files and the ONNX Runtime library, the sandbox, the AVX2 check, loading
//! ONNX Runtime. Only after the sandbox are the engine (its word index starts writer threads)
//! and the threads of the worker created, so every thread runs sandboxed:
//!
//! - the calling thread becomes the **reader**: it decodes frames and forwards requests to the
//!   index thread over an unbounded channel, so it never blocks on the other threads;
//! - the **index** thread owns the [`Engine`] and the queue of notes waiting for embeddings;
//!   it applies `reset`/`upsert`/`remove` with one commit each, answers searches in their
//!   order with the changes (only the latest of the pending ones runs, in its place) and hands
//!   one note at a time to the embed thread;
//! - the **embed** thread owns the [`Model`]: it embeds one chunk per model call and answers a
//!   query vector request before the next chunk;
//! - the **writer** thread owns `output` and writes the replies of all threads.
//!
//! Nothing here prints messages, notes or queries: stderr gets codes, counts and timings only.
//!
//! Smartnote logs a stderr line only when it matches one of the worker's forms, listed in
//! `internal/adapter/searchworker/stderr.go`; any other line it logs with its length only. A
//! new `eprintln!` needs its form there and a new source file its entry in `workerSourceFiles`
//! (the Go test `TestStderrFormsCoverWorker` checks this crate's sources), and a new `msg` of an
//! error reply needs its text in `knownErrorTexts` of `internal/adapter/searchworker/protocol.go`.
//! Smartnote logs all of it within a small budget per run of the application.

use std::collections::{HashSet, VecDeque};
use std::hint::black_box;
use std::io::{BufReader, Read, Write};
use std::path::Path;
use std::process::{self, ExitCode};
use std::sync::mpsc::{self, Receiver, RecvTimeoutError, Sender, SyncSender, TryRecvError};
use std::thread::{self, JoinHandle};
use std::time::{Duration, Instant};

use serde_bytes::ByteBuf;

use crate::embed::{
    ChunkParams, ChunkSpan, ChunkVec, DIM, Manifest, Model, ModelError, ModelFiles, OrtLibrary,
    chunk_note, has_avx2, index_id, load_ort,
};
use crate::engine::{Engine, Upserted};
use crate::harden;
use crate::protocol::{
    ErrorCode, FrameError, NoteIn, PROTO, RemovalIn, Reply, Request, State, decode, encode,
    read_frame, write_frame,
};
use crate::sandbox::{self, Applied, SandboxLevel};
use crate::text::parse_query;

/// Exit code when `hello` names another protocol version.
const EXIT_UNSUPPORTED_PROTO: u8 = 2;
/// Exit code after a malformed frame or message, or a message out of place.
const EXIT_BAD_REQUEST: u8 = 3;

/// How long a search waits for its query vector: the embed thread finishes at most one chunk
/// first (spec §6.2); Smartnote gives a search 2 s (spec §8.1).
const QUERY_VECTOR_WAIT: Duration = Duration::from_secs(1);

/// Stack overwritten after each decoded frame: ciborium keeps a 4 KiB scratch buffer on the
/// stack, which may hold message text.
const SCRUB_BYTES: usize = 32 << 10;

/// Serves Smartnote over `input` (stdin) and `output` (stdout) until EOF.
///
/// Exit codes: 0 after EOF, 2 when `hello` names another protocol version (after
/// `error{unsupported_proto}`), 3 after a malformed frame or message (after `error{bad_request}`),
/// 1 when the process cannot be hardened or a thread cannot start. Call it on a single-threaded
/// process: the sandbox must reach every thread (see [`sandbox::apply`]).
pub fn run(input: impl Read + Send + 'static, mut output: impl Write + Send + 'static) -> ExitCode {
    let mut input = BufReader::new(input);
    let hello = match read_request(&mut input) {
        Ok(None) => return ExitCode::SUCCESS,
        Ok(Some(Request::Hello {
            proto,
            model_dir,
            ort_lib,
            threads,
            parent_pid,
        })) => {
            if proto != PROTO {
                eprintln!("unsupported protocol version {proto}, expected {PROTO}");
                let reply = error_reply(ErrorCode::UnsupportedProto, "unsupported protocol");
                // The exit code tells the same if the reply cannot be written.
                let _ = write_reply(&mut output, &reply);
                return ExitCode::from(EXIT_UNSUPPORTED_PROTO);
            }
            Hello {
                model_dir,
                ort_lib,
                threads,
                parent_pid,
            }
        }
        Ok(Some(_)) => return refuse(&mut output, BadRequest::ExpectedHello),
        Err(Failure::BadRequest(bad)) => return refuse(&mut output, bad),
        Err(Failure::Input) => return ExitCode::FAILURE,
    };
    serve(input, output, &hello)
}

/// The settings of `hello`.
struct Hello {
    model_dir: String,
    ort_lib: String,
    threads: u16,
    parent_pid: u32,
}

/// What ends the reader with a failure.
enum Failure {
    /// The input is no valid frame or message: answered with `error{bad_request}`.
    BadRequest(BadRequest),
    /// Reading the input failed.
    Input,
}

/// Category of a bad request; the only content of its error reply and log line.
#[derive(Clone, Copy)]
enum BadRequest {
    Frame,
    Message,
    ExpectedHello,
    UnexpectedHello,
}

impl BadRequest {
    fn code(self) -> &'static str {
        match self {
            Self::Frame => "malformed frame",
            Self::Message => "malformed message",
            Self::ExpectedHello => "expected hello",
            Self::UnexpectedHello => "unexpected hello",
        }
    }
}

/// Writes `error{bad_request}` to `output` before any thread exists and returns exit code 3.
fn refuse(output: &mut impl Write, bad: BadRequest) -> ExitCode {
    eprintln!("bad request: {}", bad.code());
    // The exit code tells the same if the reply cannot be written.
    let _ = write_reply(output, &error_reply(ErrorCode::BadRequest, bad.code()));
    ExitCode::from(EXIT_BAD_REQUEST)
}

/// `msg` is a fixed text: Smartnote shows only the texts listed in its `knownErrorTexts`.
fn error_reply(code: ErrorCode, msg: &str) -> Reply {
    Reply::Error {
        req: None,
        code,
        msg: msg.into(),
    }
}

/// Reads and decodes one request; `Ok(None)` at a clean EOF.
fn read_request(input: &mut impl Read) -> Result<Option<Request>, Failure> {
    let body = match read_frame(input) {
        Ok(Some(body)) => body,
        Ok(None) => return Ok(None),
        Err(FrameError::Io(_)) => {
            eprintln!("stdin read failed");
            return Err(Failure::Input);
        }
        Err(FrameError::TooLarge(_) | FrameError::Truncated) => {
            return Err(Failure::BadRequest(BadRequest::Frame));
        }
    };
    let decoded = decode::<Request>(&body);
    scrub_stack();
    // Every decoding error is a bad message; its details may quote the message.
    decoded
        .map(Some)
        .map_err(|_| Failure::BadRequest(BadRequest::Message))
}

/// Best effort: overwrites [`SCRUB_BYTES`] of the stack below the caller, where the decoder's
/// frames and its scratch buffer were. Not inlined, so the area starts where the decoder's
/// frames started.
#[inline(never)]
#[allow(
    unsafe_code,
    reason = "volatile writes keep the zeroing from being optimized away"
)]
fn scrub_stack() {
    #[allow(
        clippy::large_stack_arrays,
        reason = "the array must lie on the stack: it overwrites the stack"
    )]
    let mut area = [0_u8; SCRUB_BYTES];
    for byte in &mut area {
        // SAFETY: `byte` is a valid, aligned and exclusive reference to a byte of a local array.
        unsafe { std::ptr::write_volatile(byte, 0) };
    }
    black_box(&mut area);
}

/// Encodes and writes one reply; `Err` when either fails.
fn write_reply(output: &mut impl Write, reply: &Reply) -> Result<(), ()> {
    let body = encode(reply).map_err(drop)?;
    write_frame(output, &body).map_err(drop)
}

/// Everything after a valid `hello`.
fn serve(
    mut input: BufReader<impl Read>,
    output: impl Write + Send + 'static,
    hello: &Hello,
) -> ExitCode {
    if let Err(err) = harden::harden_process(Some(hello.parent_pid)) {
        eprintln!("harden failed: {err}");
        return ExitCode::FAILURE;
    }
    let verified = verify_model(&hello.model_dir, &hello.ort_lib);
    let applied = sandbox::apply();
    report_sandbox(&applied);
    let model_files = verified.and_then(load_runtime);

    let index = index_id(&Manifest::pinned(), &ChunkParams::default());
    let engine = Engine::new(index);
    let semantic = if model_files.is_some() {
        State::Loading
    } else {
        State::Unavailable
    };
    let ready = Reply::Ready {
        worker: env!("CARGO_PKG_VERSION").into(),
        index: ByteBuf::from(index.to_vec()),
        lexical: ready_or_unavailable(engine.lexical_ready()),
        semantic,
        sandbox: applied.level,
    };

    let (out_tx, out_rx) = mpsc::channel();
    // The receiver is alive: `ready` is the first frame the writer writes.
    let _ = out_tx.send(ready);
    let Ok(writer) = spawn("writer", move || write_replies(output, &out_rx)) else {
        return ExitCode::FAILURE;
    };
    let (index_tx, index_rx) = mpsc::channel();
    let mut embed = None;
    let mut embed_tx = None;
    if let Some(files) = model_files {
        let (tx, rx) = mpsc::channel();
        let to_index = index_tx.clone();
        let threads = hello.threads;
        let Ok(handle) = spawn("embed", move || embed_loop(files, threads, &rx, &to_index)) else {
            return ExitCode::FAILURE;
        };
        embed = Some(handle);
        embed_tx = Some(tx);
    }
    let index_out = out_tx.clone();
    let index_thread = spawn("index", move || {
        IndexThread::new(engine, index_out, embed_tx, semantic).run(&index_rx);
    });
    let code = match index_thread {
        Ok(_) => read_requests(&mut input, &index_tx, &out_tx),
        Err(()) => ExitCode::FAILURE,
    };

    // The index thread stops at `Shutdown` and drops the embed channel, which stops the embed
    // thread after its current chunk; the writer stops when every reply sender is gone.
    let _ = index_tx.send(IndexMsg::Shutdown);
    drop(index_tx);
    drop(out_tx);
    if let Ok(handle) = index_thread {
        join(handle);
    }
    if let Some(handle) = embed {
        join(handle);
    }
    join(writer);
    code
}

fn ready_or_unavailable(ready: bool) -> State {
    if ready {
        State::Ready
    } else {
        State::Unavailable
    }
}

/// Spawns a named thread; logs the failure.
fn spawn(name: &str, f: impl FnOnce() + Send + 'static) -> Result<JoinHandle<()>, ()> {
    thread::Builder::new()
        .name(name.into())
        .spawn(f)
        .map_err(|_| eprintln!("starting the {name} thread failed"))
}

fn join(handle: JoinHandle<()>) {
    // Panics abort the process (`panic = "abort"`): a join never sees one.
    let _ = handle.join();
}

/// Reads the model files and the ONNX Runtime library before the sandbox (spec §10.2 p. 1);
/// `None`, with a log line, when either is missing or differs from its pinned digest.
fn verify_model(model_dir: &str, ort_lib: &str) -> Option<(ModelFiles, OrtLibrary)> {
    let files = ModelFiles::read(Path::new(model_dir))
        .map_err(|err| eprintln!("semantic search unavailable: model files: {err}"));
    let lib = OrtLibrary::verify(Path::new(ort_lib))
        .map_err(|err| eprintln!("semantic search unavailable: onnxruntime: {err}"));
    Some((files.ok()?, lib.ok()?))
}

/// Logs a sandbox below `full` with the codes of what is missing, and descriptors left open.
fn report_sandbox(applied: &Applied) {
    if applied.level != SandboxLevel::Full {
        let level = match applied.level {
            SandboxLevel::Full => "full",
            SandboxLevel::Partial => "partial",
            SandboxLevel::None => "none",
        };
        let gaps: Vec<&str> = applied.gaps.iter().map(|g| g.code()).collect();
        eprintln!("sandbox {level}: {}", gaps.join(" "));
    }
    match sandbox::extra_open_fds() {
        Ok(fds) if fds.is_empty() => {}
        Ok(fds) => eprintln!(
            "sandbox: {} descriptors besides 0, 1, 2 are open, {} writable",
            fds.len(),
            sandbox::writable_fds(&fds)
        ),
        Err(_) => eprintln!("sandbox: open descriptors cannot be listed"),
    }
}

/// Checks AVX2 and loads ONNX Runtime, under the sandbox; the model files when both succeed.
fn load_runtime((files, lib): (ModelFiles, OrtLibrary)) -> Option<ModelFiles> {
    if !has_avx2() {
        eprintln!("semantic search unavailable: the CPU has no AVX2");
        return None;
    }
    match load_ort(&lib) {
        Ok(()) => Some(files),
        Err(err) => {
            eprintln!("semantic search unavailable: {err}");
            None
        }
    }
}

/// The reader loop: forwards requests to the index thread until EOF or a bad request.
fn read_requests(input: &mut impl Read, index: &Sender<IndexMsg>, out: &Sender<Reply>) -> ExitCode {
    let bad = loop {
        match read_request(input) {
            Ok(None) => return ExitCode::SUCCESS,
            Ok(Some(Request::Hello { .. })) => break BadRequest::UnexpectedHello,
            Ok(Some(request)) => {
                if index.send(IndexMsg::Request(request)).is_err() {
                    // The index thread is gone only if it failed to start.
                    return ExitCode::FAILURE;
                }
            }
            Err(Failure::BadRequest(bad)) => break bad,
            Err(Failure::Input) => return ExitCode::FAILURE,
        }
    };
    eprintln!("bad request: {}", bad.code());
    let _ = out.send(error_reply(ErrorCode::BadRequest, bad.code()));
    ExitCode::from(EXIT_BAD_REQUEST)
}

/// The writer loop: the only owner of `output`. A failed write means Smartnote is gone; the
/// process exits at once with code 1.
fn write_replies(mut output: impl Write, replies: &Receiver<Reply>) {
    for reply in replies {
        if write_reply(&mut output, &reply).is_err() {
            eprintln!("stdout write failed");
            process::exit(1);
        }
    }
}

/// Input of the index thread.
enum IndexMsg {
    /// A request of Smartnote other than `hello`.
    Request(Request),
    /// The model finished loading (`Ready`) or failed (`Unavailable`).
    Model(State),
    /// Embeddings of the note handed to the embed thread.
    Chunks(NoteChunks),
    /// The note handed to the embed thread could not be embedded at revision `rev`.
    ChunksFailed { id: String, rev: u64 },
    /// The reader stopped: the index thread stops after the current request.
    Shutdown,
}

/// Embeddings of a note computed for revision `rev`.
struct NoteChunks {
    id: String,
    rev: u64,
    chunks: Vec<ChunkVec>,
}

/// Input of the embed thread.
enum EmbedMsg {
    /// A note to embed; the index thread sends the next one after the answer to this one.
    Note(NoteJob),
    /// A query to embed before the next chunk; the vector goes to `reply`.
    Query {
        text: String,
        reply: SyncSender<[f32; DIM]>,
    },
}

/// A note to embed. No `Debug`: it holds note text.
struct NoteJob {
    id: String,
    rev: u64,
    name: String,
    lines: Vec<String>,
}

/// The index thread: owner of the engine.
struct IndexThread {
    engine: Engine,
    out: Sender<Reply>,
    /// `None` when semantic search is unavailable.
    embed: Option<Sender<EmbedMsg>>,
    semantic: State,
    /// Notes waiting for embeddings, besides the one at the embed thread.
    pending: EmbedQueue,
    /// The embed thread has a note.
    embedding: bool,
    /// Requests received and not yet handled.
    requests: VecDeque<Request>,
}

impl IndexThread {
    fn new(
        engine: Engine,
        out: Sender<Reply>,
        embed: Option<Sender<EmbedMsg>>,
        semantic: State,
    ) -> Self {
        Self {
            engine,
            out,
            embed,
            semantic,
            pending: EmbedQueue::default(),
            embedding: false,
            requests: VecDeque::new(),
        }
    }

    /// Handles messages until `Shutdown` or until every sender is gone.
    fn run(mut self, input: &Receiver<IndexMsg>) {
        loop {
            if self.requests.is_empty() {
                let Ok(msg) = input.recv() else { return };
                if !self.accept(msg) {
                    return;
                }
            }
            // Everything that arrived meanwhile: searches pending behind each other coalesce.
            loop {
                match input.try_recv() {
                    Ok(msg) => {
                        if !self.accept(msg) {
                            return;
                        }
                    }
                    Err(TryRecvError::Empty) => break,
                    Err(TryRecvError::Disconnected) => return,
                }
            }
            self.handle_next_request();
        }
    }

    /// Queues a request or applies a message of the embed thread; `false` at `Shutdown`.
    fn accept(&mut self, msg: IndexMsg) -> bool {
        match msg {
            IndexMsg::Request(request) => self.requests.push_back(request),
            IndexMsg::Model(state) => {
                self.semantic = state;
                if state != State::Ready {
                    self.drop_semantic();
                }
                self.send_status();
                self.dispatch();
            }
            IndexMsg::Chunks(NoteChunks { id, rev, chunks }) => {
                self.embedding = false;
                if let Some(record) = self.engine.set_chunks(&id, rev, chunks) {
                    self.send(Reply::Embedded {
                        id,
                        rev,
                        emb: ByteBuf::from(record),
                    });
                    self.send_status();
                }
                self.dispatch();
            }
            IndexMsg::ChunksFailed { id, rev } => {
                self.embedding = false;
                // It no longer counts as queued, so `Indexing N of M` can end.
                self.engine.embedding_failed(&id, rev);
                self.send_status();
                self.dispatch();
            }
            IndexMsg::Shutdown => return false,
        }
        true
    }

    fn handle_next_request(&mut self) {
        for req in coalesce_searches(&mut self.requests) {
            self.send(Reply::Result {
                req,
                hits: Vec::new(),
            });
        }
        let Some(request) = self.requests.pop_front() else {
            return;
        };
        match request {
            Request::Reset { req } => {
                // `reset` commits the cleared word index itself.
                self.engine.reset();
                self.pending.clear();
                self.answer_change(req);
            }
            Request::Upsert { req, notes } => self.upsert(req, notes),
            Request::Remove { req, items } => self.remove(req, &items),
            Request::Search { req, query, limit } => self.search(req, &query, limit),
            #[cfg(feature = "test-probes")]
            Request::DebugPause { ms } => thread::sleep(Duration::from_millis(ms)),
            // The reader answers `hello` itself and never forwards it.
            Request::Hello { .. } => {}
        }
    }

    fn upsert(&mut self, req: u64, notes: Vec<NoteIn>) {
        for note in notes {
            let id = note.id.clone();
            let upserted = self.engine.upsert(note);
            if matches!(upserted, Some(Upserted::NeedsEmbedding { .. }))
                && self.semantic != State::Unavailable
            {
                self.pending.push(id);
            }
        }
        self.engine.commit();
        self.answer_change(req);
    }

    fn remove(&mut self, req: u64, items: &[RemovalIn]) {
        for item in items {
            self.engine.remove(&item.id, item.rev);
        }
        self.engine.commit();
        self.answer_change(req);
    }

    /// `ok{req}` and `status` after a change, then the next note to the embed thread.
    fn answer_change(&mut self, req: u64) {
        self.send(Reply::Ok { req });
        self.send_status();
        self.dispatch();
    }

    fn search(&mut self, req: u64, query: &str, limit: u16) {
        let query = parse_query(query);
        let qvec = query
            .semantic
            .as_deref()
            .and_then(|text| self.query_vector(text));
        let hits = self
            .engine
            .search(&query, qvec.as_ref(), usize::from(limit));
        self.send(Reply::Result { req, hits });
    }

    /// The query vector from the embed thread, within [`QUERY_VECTOR_WAIT`]; `None` when the
    /// model is not ready or the wait runs out (the search runs without the semantic list).
    fn query_vector(&self, text: &str) -> Option<[f32; DIM]> {
        if self.semantic != State::Ready {
            return None;
        }
        let embed = self.embed.as_ref()?;
        let (reply, vector) = mpsc::sync_channel(1);
        embed
            .send(EmbedMsg::Query {
                text: text.into(),
                reply,
            })
            .ok()?;
        match vector.recv_timeout(QUERY_VECTOR_WAIT) {
            Ok(v) => Some(v),
            Err(RecvTimeoutError::Timeout) => {
                eprintln!("query vector timed out after {QUERY_VECTOR_WAIT:?}");
                None
            }
            Err(RecvTimeoutError::Disconnected) => None,
        }
    }

    /// Hands the next queued note that still needs embeddings to an idle embed thread.
    fn dispatch(&mut self) {
        if self.embedding || self.semantic != State::Ready {
            return;
        }
        let Some(embed) = &self.embed else { return };
        while let Some(id) = self.pending.pop() {
            // Gone, or embedded meanwhile from a stored record: nothing to do.
            let Some((rev, name, lines)) = self.engine.note_for_embedding(&id) else {
                continue;
            };
            let job = NoteJob {
                id,
                rev,
                name,
                lines,
            };
            if embed.send(EmbedMsg::Note(job)).is_ok() {
                self.embedding = true;
            } else {
                self.semantic = State::Unavailable;
                self.drop_semantic();
                self.send_status();
            }
            return;
        }
    }

    /// Forgets the embed thread and its queue: semantic search is unavailable.
    fn drop_semantic(&mut self) {
        self.embed = None;
        self.pending.clear();
        self.embedding = false;
    }

    fn send_status(&self) {
        let (notes, without_embeddings) = self.engine.counts();
        // Without the model nothing is waiting: no embeddings will be computed.
        let queued = if self.semantic == State::Unavailable {
            0
        } else {
            without_embeddings
        };
        self.send(Reply::Status {
            notes,
            queued,
            lexical: ready_or_unavailable(self.engine.lexical_ready()),
            semantic: self.semantic,
        });
    }

    fn send(&self, reply: Reply) {
        // The writer stops only when the process exits.
        let _ = self.out.send(reply);
    }
}

/// The embed thread: loads the model, then embeds queries first and notes one chunk at a time
/// until the index thread drops its sender.
fn embed_loop(
    files: ModelFiles,
    threads: u16,
    input: &Receiver<EmbedMsg>,
    index: &Sender<IndexMsg>,
) {
    let started = Instant::now();
    let mut model = match Model::new(files, threads) {
        Ok(model) => model,
        Err(err) => {
            eprintln!("semantic search unavailable: model: {err}");
            let _ = index.send(IndexMsg::Model(State::Unavailable));
            return;
        }
    };
    eprintln!("model loaded in {} ms", started.elapsed().as_millis());
    if index.send(IndexMsg::Model(State::Ready)).is_err() {
        return;
    }
    let mut work: Option<NoteWork> = None;
    loop {
        // Queries come first: drain the channel, block only when there is no chunk to do.
        let msg = if work.is_some() {
            match input.try_recv() {
                Ok(msg) => Some(msg),
                Err(TryRecvError::Empty) => None,
                Err(TryRecvError::Disconnected) => return,
            }
        } else {
            match input.recv() {
                Ok(msg) => Some(msg),
                Err(_) => return,
            }
        };
        match msg {
            Some(EmbedMsg::Query { text, reply }) => match model.embed_query(&text) {
                // The search may have given up waiting: then nobody receives it.
                Ok(vector) => drop(reply.try_send(vector)),
                Err(err) => eprintln!("query embedding failed: {err}"),
            },
            // The index thread sends the next note only after the answer to the previous one.
            Some(EmbedMsg::Note(job)) => work = Some(NoteWork::new(&model, job)),
            None => {
                let Some(current) = work.as_mut() else {
                    continue;
                };
                match current.step(&mut model) {
                    Ok(false) => {}
                    Ok(true) => {
                        let done = work.take().map(NoteWork::finish);
                        if let Some(chunks) = done
                            && index.send(IndexMsg::Chunks(chunks)).is_err()
                        {
                            return;
                        }
                    }
                    Err(err) => {
                        eprintln!("note embedding failed: {err}");
                        let failed = work.take().map(|w| IndexMsg::ChunksFailed {
                            id: w.job.id,
                            rev: w.job.rev,
                        });
                        if let Some(msg) = failed
                            && index.send(msg).is_err()
                        {
                            return;
                        }
                    }
                }
            }
        }
    }
}

/// A note being embedded chunk by chunk.
struct NoteWork {
    job: NoteJob,
    spans: Vec<ChunkSpan>,
    chunks: Vec<ChunkVec>,
}

impl NoteWork {
    fn new(model: &Model, mut job: NoteJob) -> Self {
        let lines: Vec<&str> = job.lines.iter().map(String::as_str).collect();
        let note = chunk_note(
            &job.name,
            &lines,
            &|text| model.count_tokens(text),
            &ChunkParams::default(),
        );
        // Every chunk is embedded with the part of the name it was measured with.
        job.name.truncate(note.name_len);
        let chunks = Vec::with_capacity(note.spans.len());
        Self {
            job,
            spans: note.spans,
            chunks,
        }
    }

    /// Embeds the next chunk; `true` when every chunk is done (also for a note without any).
    fn step(&mut self, model: &mut Model) -> Result<bool, ModelError> {
        let Some(span) = self.spans.get(self.chunks.len()).copied() else {
            return Ok(true);
        };
        let first = span.first as usize - 1;
        let body = match span.window {
            Some((start, end)) => self.job.lines[first][start..end].to_owned(),
            None => self.job.lines[first..span.last as usize].join("\n"),
        };
        let vec = model.embed_passage(&self.job.name, &body)?;
        self.chunks.push(ChunkVec {
            first: span.first,
            last: span.last,
            vec,
        });
        Ok(self.chunks.len() == self.spans.len())
    }

    fn finish(self) -> NoteChunks {
        NoteChunks {
            id: self.job.id,
            rev: self.job.rev,
            chunks: self.chunks,
        }
    }
}

/// Takes every `search` but the last out of `queue` and returns their request numbers; they
/// get an empty result. The last search keeps its place, so it runs after every change that
/// came before it and before those that came after it; the other requests keep their order.
fn coalesce_searches(queue: &mut VecDeque<Request>) -> Vec<u64> {
    let Some(last) = queue
        .iter()
        .rposition(|r| matches!(r, Request::Search { .. }))
    else {
        return Vec::new();
    };
    let mut superseded = Vec::new();
    let mut at = 0;
    queue.retain(|request| {
        let keep = match request {
            Request::Search { req, .. } if at != last => {
                superseded.push(*req);
                false
            }
            _ => true,
        };
        at += 1;
        keep
    });
    superseded
}

/// Notes waiting for embeddings, each id once, oldest first. The revision is not kept: the
/// index thread reads the current one when it hands a note to the embed thread.
#[derive(Default)]
struct EmbedQueue {
    order: VecDeque<String>,
    queued: HashSet<String>,
}

impl EmbedQueue {
    fn push(&mut self, id: String) {
        if self.queued.insert(id.clone()) {
            self.order.push_back(id);
        }
    }

    fn pop(&mut self) -> Option<String> {
        let id = self.order.pop_front()?;
        self.queued.remove(&id);
        Some(id)
    }

    fn clear(&mut self) {
        self.order.clear();
        self.queued.clear();
    }
}

#[cfg(test)]
mod tests {
    use std::collections::VecDeque;

    use super::{EmbedQueue, coalesce_searches};
    use crate::protocol::Request;

    fn search(req: u64) -> Request {
        Request::Search {
            req,
            query: format!("q{req}"),
            limit: 10,
        }
    }

    fn upsert(req: u64) -> Request {
        Request::Upsert {
            req,
            notes: Vec::new(),
        }
    }

    #[test]
    fn coalesce_keeps_last_in_its_place() {
        let mut queue = VecDeque::from([search(1), upsert(10), search(2), search(3)]);

        let superseded = coalesce_searches(&mut queue);

        assert_eq!(superseded, [1, 2]);
        assert_eq!(queue, [upsert(10), search(3)]);
    }

    #[test]
    fn coalesce_never_moves_a_search_before_a_change() {
        let mut queue = VecDeque::from([search(1), upsert(10), search(2)]);

        let superseded = coalesce_searches(&mut queue);

        assert_eq!(superseded, [1]);
        assert_eq!(
            queue,
            [upsert(10), search(2)],
            "the upsert that came first is applied first"
        );
    }

    #[test]
    fn coalesce_keeps_changes_after_the_last_search() {
        let remove = Request::Remove {
            req: 11,
            items: Vec::new(),
        };
        let mut queue = VecDeque::from([search(1), search(2), remove.clone()]);

        let superseded = coalesce_searches(&mut queue);

        assert_eq!(superseded, [1]);
        assert_eq!(queue, [search(2), remove]);
    }

    #[test]
    fn coalesce_keeps_a_single_search_in_its_place() {
        let reset = Request::Reset { req: 4 };
        let mut queue = VecDeque::from([reset.clone(), search(5)]);

        assert_eq!(coalesce_searches(&mut queue), Vec::<u64>::new());
        assert_eq!(queue, [reset, search(5)]);
    }

    #[test]
    fn embed_queue_keeps_each_note_once_in_order() {
        let mut queue = EmbedQueue::default();
        for id in ["a", "b", "a", "c", "b"] {
            queue.push(id.into());
        }

        let order: Vec<String> = std::iter::from_fn(|| queue.pop()).collect();

        assert_eq!(order, ["a", "b", "c"]);
        queue.push("a".into());
        assert_eq!(
            queue.pop().as_deref(),
            Some("a"),
            "a popped id can come back"
        );
    }

    #[test]
    fn embed_queue_clear_forgets_everything() {
        let mut queue = EmbedQueue::default();
        queue.push("a".into());

        queue.clear();

        assert_eq!(queue.pop(), None);
        queue.push("a".into());
        assert_eq!(queue.pop().as_deref(), Some("a"));
    }
}
