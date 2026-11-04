//! Message decoder on arbitrary bodies: no panic, and a decoded message re-encodes to bytes that
//! decode and encode to themselves.
#![no_main]

use libfuzzer_sys::fuzz_target;
use serde::Serialize;
use serde::de::DeserializeOwned;
use worker::protocol::{Reply, Request, decode, encode};

fn check<T: Serialize + DeserializeOwned>(data: &[u8]) {
    if let Ok(msg) = decode::<T>(data) {
        let body = encode(&msg).expect("a decoded message encodes");
        let again: T = decode(&body).expect("an encoded message decodes");
        assert_eq!(encode(&again).expect("encodes again"), body);
    }
}

fuzz_target!(|data: &[u8]| {
    check::<Request>(data);
    check::<Reply>(data);
});
