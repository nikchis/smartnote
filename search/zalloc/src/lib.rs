//! Global allocator that zeroes every block before it returns to the system allocator
//! (spec §6.9), so freed note text does not stay in the heap or in a memory dump.
#![allow(
    unsafe_code,
    reason = "GlobalAlloc is an unsafe trait; every block is commented"
)]

use std::alloc::{GlobalAlloc, Layout, System};
use std::ptr;
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

/// Zeroes `len` bytes at `ptr`.
///
/// The fence keeps the compiler from dropping the writes as dead stores to a block that is
/// freed right after: it may not move memory accesses across the fence.
///
/// # Safety
///
/// `ptr` must be valid for writes of `len` bytes.
unsafe fn wipe(ptr: *mut u8, len: usize) {
    // SAFETY: the caller guarantees `ptr` is valid for writes of `len` bytes; `u8` has no
    // alignment requirement and every bit pattern is a valid `u8`.
    unsafe { ptr::write_bytes(ptr, 0, len) };
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
    fn wipe_of_zero_bytes_is_a_no_op() {
        let mut block = [0x5A_u8; 4];
        // SAFETY: a zero-length write is valid for any non-null aligned pointer.
        unsafe { wipe(block.as_mut_ptr(), 0) };
        assert_eq!(block, [0x5A; 4]);
    }
}
