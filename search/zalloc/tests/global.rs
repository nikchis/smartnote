//! `Zeroizing` as the global allocator of a whole test binary: growth and shrinking
//! (realloc), over-aligned blocks, zero-initialised blocks and several threads.
//! The run under the address sanitizer (spec §12.1) checks the same paths for out-of-bounds writes.

#![allow(unsafe_code, reason = "calls the global allocator directly")]

use std::alloc::{Layout, alloc_zeroed, dealloc};
use std::collections::HashMap;
use std::thread;

#[global_allocator]
static ALLOC: zalloc::Zeroizing = zalloc::Zeroizing;

#[test]
fn growing_and_shrinking_keeps_contents() {
    let mut v: Vec<u32> = Vec::new();
    for i in 0..100_000 {
        v.push(i);
    }
    v.truncate(10);
    v.shrink_to_fit();
    assert_eq!(v, (0..10).collect::<Vec<_>>());

    let mut s = String::from("заметка");
    s.reserve(1 << 16);
    s.push_str(" — строка");
    s.shrink_to_fit();
    assert_eq!(s, "заметка — строка");
}

#[test]
fn over_aligned_blocks_keep_alignment_and_contents() {
    #[repr(align(4096))]
    struct Page([u8; 4096]);

    let mut pages: Vec<Page> = Vec::with_capacity(1);
    pages.push(Page([7; 4096]));
    for _ in 0..8 {
        pages.push(Page([7; 4096]));
    }
    assert_eq!(pages.as_ptr().addr() % 4096, 0);
    assert!(pages.iter().all(|p| p.0.iter().all(|&b| b == 7)));
}

#[test]
fn alloc_zeroed_returns_zeroed_memory() {
    let layout = Layout::from_size_align(1 << 20, 64).expect("valid layout");
    // SAFETY: the layout has a non-zero size.
    let ptr = unsafe { alloc_zeroed(layout) };
    assert!(!ptr.is_null());
    // SAFETY: `ptr` is a live, initialised block of `layout.size()` bytes.
    let block = unsafe { std::slice::from_raw_parts(ptr, layout.size()) };
    assert!(block.iter().all(|&b| b == 0));
    // SAFETY: `ptr` was allocated above with `layout` and is not used afterwards.
    unsafe { dealloc(ptr, layout) };
}

#[test]
fn threads_allocate_and_free_concurrently() {
    let handles: Vec<_> = (0..8_u32)
        .map(|t| {
            thread::spawn(move || {
                let mut map = HashMap::new();
                for i in 0..5_000_u32 {
                    map.insert(i, format!("{t}-{i}"));
                }
                map.retain(|k, _| k % 2 == 0);
                map.len()
            })
        })
        .collect();
    for h in handles {
        assert_eq!(h.join().expect("thread finished"), 2_500);
    }
}
