//! Frame reader on arbitrary input: no panic, bodies within the limit, each body decoded.
#![no_main]

use libfuzzer_sys::fuzz_target;
use worker::protocol::{MAX_FRAME, Request, decode, read_frame};

fuzz_target!(|data: &[u8]| {
    let mut stream = data;
    while let Ok(Some(body)) = read_frame(&mut stream) {
        assert!(body.len() <= MAX_FRAME);
        let _ = decode::<Request>(&body);
    }
});
