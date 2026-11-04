//! Global allocator that zeroes every block before it returns to the system allocator
//! (spec §6.9), so freed note text does not stay in the heap or in a memory dump.
#![allow(
    unsafe_code,
    reason = "GlobalAlloc is an unsafe trait; every block is commented"
)]

use std::alloc::{GlobalAlloc, Layout, System};
use std::sync::atomic::{Ordering, compiler_fence};

/// Wrapper over [`System`] whose `dealloc` zeroes the block first.
///
/// `realloc` keeps the trait's default — allocate, copy, deallocate — so the old block
/// also passes through `dealloc` and is zeroed (an in-place `realloc` of the system
/// allocator could release the tail of a shrunk block without zeroing it).
#[derive(Debug, Clone, Copy, Default)]
pub struct Zeroizing;

// SAFETY: every block comes from `System` and goes back to `System` with the same layout;
// `dealloc` only writes inside the block before releasing it.
unsafe impl GlobalAlloc for Zeroizing {
    unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
        // SAFETY: the caller upholds the `GlobalAlloc::alloc` contract (non-zero size).
        unsafe { System.alloc(layout) }
    }

    unsafe fn alloc_zeroed(&self, layout: Layout) -> *mut u8 {
        // SAFETY: the caller upholds the `GlobalAlloc::alloc_zeroed` contract.
        unsafe { System.alloc_zeroed(layout) }
    }

    unsafe fn dealloc(&self, ptr: *mut u8, layout: Layout) {
        // SAFETY: the caller guarantees `ptr` is a live block of this allocator allocated
        // with `layout`, so it is valid for writes of `layout.size()` bytes.
        unsafe { wipe(ptr, layout.size()) };
        // SAFETY: `ptr` came from `System` (see `alloc`) with the same `layout`.
        unsafe { System.dealloc(ptr, layout) }
    }
}

/// Machine word: the unit of the bulk of the zeroing writes.
const WORD: usize = size_of::<usize>();

/// Zeroes `len` bytes at `ptr`, allocation-free.
///
/// The writes are volatile, so the compiler may not drop them as dead stores to a block
/// that is freed right after (a plain `write_bytes` is dropped this way in release builds).
/// The bytes up to the first word boundary and after the last whole word are written one
/// by one, the rest a word at a time. The fence keeps later code, such as the `free` of the
/// block, from being reordered before the writes by the compiler.
///
/// # Safety
///
/// `ptr` must be valid for writes of `len` bytes.
unsafe fn wipe(ptr: *mut u8, len: usize) {
    // `align_offset` may report `usize::MAX` ("cannot align"); the whole range is then
    // written byte by byte.
    let head = ptr.align_offset(WORD).min(len);
    let words = (len - head) / WORD;
    let tail = head + words * WORD;
    for i in 0..head {
        // SAFETY: `i < head <= len`, so the byte lies inside the range the caller vouches for.
        unsafe { ptr.add(i).write_volatile(0) };
    }
    // SAFETY: `head <= len`, so the offset stays inside the range or one past its end.
    #[allow(
        clippy::cast_ptr_alignment,
        reason = "the pointer is word-aligned when words > 0: head is then align_offset(WORD)"
    )]
    let word_ptr = unsafe { ptr.add(head) }.cast::<usize>();
    for i in 0..words {
        // SAFETY: `words > 0` implies `head < len`, so `head` is the unclamped offset to a
        // word boundary (`align_offset` did not report `usize::MAX`), and
        // `word_ptr` is aligned for `usize`; word `i` covers bytes
        // `head + i * WORD .. head + (i + 1) * WORD <= tail <= len` of the range.
        unsafe { word_ptr.add(i).write_volatile(0) };
    }
    for i in tail..len {
        // SAFETY: `i < len`, so the byte lies inside the range the caller vouches for.
        unsafe { ptr.add(i).write_volatile(0) };
    }
    compiler_fence(Ordering::SeqCst);
}

#[cfg(test)]
mod tests {
    use super::wipe;

    #[test]
    fn wipe_zeroes_every_byte() {
        let mut block = vec![0xA5_u8; 4099];
        // SAFETY: `block` owns `block.len()` initialised bytes.
        unsafe { wipe(block.as_mut_ptr(), block.len()) };
        assert!(block.iter().all(|&b| b == 0));
    }

    #[test]
    fn wipe_zeroes_exactly_the_range_at_any_alignment() {
        for offset in 0..16 {
            for len in 0..48 {
                let mut buf = [0xA5_u8; 80];
                // SAFETY: `offset + len` <= 63 < `buf.len()`, so the range lies inside `buf`.
                unsafe { wipe(buf.as_mut_ptr().add(offset), len) };
                for (i, &b) in buf.iter().enumerate() {
                    let inside = (offset..offset + len).contains(&i);
                    assert_eq!(b == 0, inside, "offset {offset}, len {len}, byte {i}");
                }
            }
        }
    }

    #[test]
    fn wipe_of_zero_bytes_is_a_no_op() {
        let mut block = [0x5A_u8; 4];
        // SAFETY: a zero-length write is valid for any non-null aligned pointer.
        unsafe { wipe(block.as_mut_ptr(), 0) };
        assert_eq!(block, [0x5A; 4]);
    }
}
