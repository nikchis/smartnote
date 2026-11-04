//! Process hardening of the worker (spec §10.1, §8.1) and its lowered priority (spec §6.2).
#![allow(
    unsafe_code,
    reason = "system calls through libc / windows-sys; every block is commented"
)]

use std::io;

/// Hardens the current process for the target OS and lowers its priority.
///
/// Must run at the start of `main`, before any thread is created: on Linux the priority and
/// the parent-death signal apply to the calling thread and are inherited by new threads.
/// `parent_pid` is the pid of the Smartnote process; on Linux the worker gets an error when
/// its actual parent differs (the parent died before `PR_SET_PDEATHSIG` was set) and must
/// exit. Other systems ignore it.
///
/// # Errors
///
/// Returns the first failed system call, or an error when the parent pid does not match.
pub fn harden_process(parent_pid: Option<u32>) -> io::Result<()> {
    os::harden(parent_pid)
}

/// Converts the `-1` failure return of a libc call into the `errno` error.
#[cfg(unix)]
fn check(rc: libc::c_int) -> io::Result<()> {
    if rc == -1 {
        Err(io::Error::last_os_error())
    } else {
        Ok(())
    }
}

#[cfg(unix)]
mod unix {
    use std::io;

    use super::check;

    /// The worker's nice value (spec §6.2).
    pub(super) const WORKER_NICE: libc::c_int = 10;

    /// Sets the nice value to [`WORKER_NICE`] unless the process already runs nicer: an
    /// unprivileged process may raise its nice value but not lower it.
    pub(super) fn lower_priority() -> io::Result<()> {
        if nice()? >= WORKER_NICE {
            return Ok(());
        }
        // SAFETY: `setpriority` takes plain integers and touches no memory of the process.
        check(unsafe { libc::setpriority(libc::PRIO_PROCESS, 0, WORKER_NICE) })
    }

    /// The nice value of the calling thread (Linux) or process (macOS).
    pub(super) fn nice() -> io::Result<libc::c_int> {
        // `getpriority` may return -1 as a valid value; only `errno` tells an error apart.
        clear_errno();
        // SAFETY: `getpriority` takes plain integers and touches no memory of the process.
        let value = unsafe { libc::getpriority(libc::PRIO_PROCESS, 0) };
        let err = io::Error::last_os_error();
        if value == -1 && err.raw_os_error() != Some(0) {
            return Err(err);
        }
        Ok(value)
    }

    fn clear_errno() {
        // SAFETY: the libc function returns the address of this thread's `errno`, valid
        // for writes for the lifetime of the thread.
        unsafe {
            #[cfg(target_os = "linux")]
            let errno = libc::__errno_location();
            #[cfg(target_os = "macos")]
            let errno = libc::__error();
            *errno = 0;
        }
    }

    /// `RLIMIT_CORE` = 0, soft and hard: no core dump of this process.
    pub(super) fn disable_core_dumps() -> io::Result<()> {
        let zero = libc::rlimit {
            rlim_cur: 0,
            rlim_max: 0,
        };
        // SAFETY: `zero` is a valid `rlimit` that outlives the call; it is only read.
        check(unsafe { libc::setrlimit(libc::RLIMIT_CORE, &raw const zero) })
    }
}

#[cfg(target_os = "linux")]
mod os {
    use std::io;

    use super::{check, unix};

    pub(super) fn harden(parent_pid: Option<u32>) -> io::Result<()> {
        unix::lower_priority()?;
        // Not dumpable: no core dump, no ptrace and no /proc/<pid>/mem for the same user.
        prctl(libc::PR_SET_DUMPABLE, 0)?;
        unix::disable_core_dumps()?;
        prctl(libc::PR_SET_NO_NEW_PRIVS, 1)?;
        prctl(libc::PR_SET_PDEATHSIG, libc::SIGKILL.unsigned_abs().into())?;
        // A parent that died before PR_SET_PDEATHSIG sends no signal; the worker has been
        // reparented by then (spec §8.1).
        if let Some(expected) = parent_pid {
            // SAFETY: `getppid` takes no arguments and always succeeds.
            let actual = unsafe { libc::getppid() };
            if u32::try_from(actual).ok() != Some(expected) {
                return Err(io::Error::other("parent process has changed"));
            }
        }
        Ok(())
    }

    /// `prctl(option, arg, 0, 0, 0)` for options whose argument is a plain integer.
    fn prctl(option: libc::c_int, arg: libc::c_ulong) -> io::Result<()> {
        let zero: libc::c_ulong = 0;
        // SAFETY: the options used here read only the integer `arg`; the unused arguments
        // are passed as zero as the kernel requires.
        check(unsafe { libc::prctl(option, arg, zero, zero, zero) })
    }
}

#[cfg(target_os = "macos")]
mod os {
    use std::io;

    use super::{check, unix};

    pub(super) fn harden(_parent_pid: Option<u32>) -> io::Result<()> {
        unix::lower_priority()?;
        unix::disable_core_dumps()?;
        // SAFETY: PT_DENY_ATTACH ignores `pid`, `addr` and `data`; the null `addr` is never
        // dereferenced.
        check(unsafe { libc::ptrace(libc::PT_DENY_ATTACH, 0, std::ptr::null_mut(), 0) })
    }
}

#[cfg(windows)]
mod os {
    use std::ffi::c_void;
    use std::io;
    use std::mem::size_of;

    use windows_sys::Win32::System::Diagnostics::Debug::{
        SEM_FAILCRITICALERRORS, SEM_NOGPFAULTERRORBOX, SetErrorMode,
    };
    use windows_sys::Win32::System::ErrorReporting::{
        WER_FAULT_REPORTING_FLAG_NOHEAP, WER_FAULT_REPORTING_NO_UI, WerSetFlags,
    };
    use windows_sys::Win32::System::SystemServices::{
        PROCESS_MITIGATION_EXTENSION_POINT_DISABLE_POLICY,
        PROCESS_MITIGATION_EXTENSION_POINT_DISABLE_POLICY_0, PROCESS_MITIGATION_IMAGE_LOAD_POLICY,
        PROCESS_MITIGATION_IMAGE_LOAD_POLICY_0,
    };
    use windows_sys::Win32::System::Threading::{
        BELOW_NORMAL_PRIORITY_CLASS, GetCurrentProcess, PROCESS_MITIGATION_POLICY,
        ProcessExtensionPointDisablePolicy, ProcessImageLoadPolicy, SetPriorityClass,
        SetProcessMitigationPolicy,
    };
    use windows_sys::core::BOOL;

    /// `PROCESS_MITIGATION_IMAGE_LOAD_POLICY` bits: `NoRemoteImages`, `NoLowMandatoryLabelImages`.
    const NO_REMOTE_IMAGES: u32 = 0x1;
    const NO_LOW_MANDATORY_LABEL_IMAGES: u32 = 0x2;
    /// `PROCESS_MITIGATION_EXTENSION_POINT_DISABLE_POLICY` bit: `DisableExtensionPoints`.
    const DISABLE_EXTENSION_POINTS: u32 = 0x1;

    pub(super) fn harden(_parent_pid: Option<u32>) -> io::Result<()> {
        // SAFETY: `GetCurrentProcess` returns a pseudo handle that needs no closing and stays
        // valid for the call.
        check(unsafe { SetPriorityClass(GetCurrentProcess(), BELOW_NORMAL_PRIORITY_CLASS) })?;
        // SAFETY: `SetErrorMode` takes and returns plain flags; the first call reads the
        // current mode so that the second keeps it.
        unsafe {
            let current = SetErrorMode(0);
            SetErrorMode(current | SEM_FAILCRITICALERRORS | SEM_NOGPFAULTERRORBOX);
        }
        // SAFETY: `WerSetFlags` takes plain flags.
        let hr =
            unsafe { WerSetFlags(WER_FAULT_REPORTING_FLAG_NOHEAP | WER_FAULT_REPORTING_NO_UI) };
        if hr < 0 {
            return Err(io::Error::from_raw_os_error(hr));
        }
        set_policy(&PROCESS_MITIGATION_IMAGE_LOAD_POLICY {
            Anonymous: PROCESS_MITIGATION_IMAGE_LOAD_POLICY_0 {
                Flags: NO_REMOTE_IMAGES | NO_LOW_MANDATORY_LABEL_IMAGES,
            },
        })?;
        set_policy(&PROCESS_MITIGATION_EXTENSION_POINT_DISABLE_POLICY {
            Anonymous: PROCESS_MITIGATION_EXTENSION_POINT_DISABLE_POLICY_0 {
                Flags: DISABLE_EXTENSION_POINTS,
            },
        })
    }

    /// A policy structure and the `PROCESS_MITIGATION_POLICY` value that expects it.
    trait MitigationPolicy {
        const KIND: PROCESS_MITIGATION_POLICY;
    }

    impl MitigationPolicy for PROCESS_MITIGATION_IMAGE_LOAD_POLICY {
        const KIND: PROCESS_MITIGATION_POLICY = ProcessImageLoadPolicy;
    }

    impl MitigationPolicy for PROCESS_MITIGATION_EXTENSION_POINT_DISABLE_POLICY {
        const KIND: PROCESS_MITIGATION_POLICY = ProcessExtensionPointDisablePolicy;
    }

    fn set_policy<P: MitigationPolicy>(policy: &P) -> io::Result<()> {
        // SAFETY: `policy` is a live structure of `size_of::<P>()` bytes of the type that
        // `P::KIND` expects (paired by `MitigationPolicy`); the call only reads it.
        check(unsafe {
            SetProcessMitigationPolicy(
                P::KIND,
                std::ptr::from_ref(policy).cast::<c_void>(),
                size_of::<P>(),
            )
        })
    }

    /// Converts a failed `BOOL` result into the `GetLastError` error.
    fn check(ok: BOOL) -> io::Result<()> {
        if ok == 0 {
            Err(io::Error::last_os_error())
        } else {
            Ok(())
        }
    }
}

#[cfg(not(any(target_os = "linux", target_os = "macos", windows)))]
compile_error!("process hardening is implemented for Linux, macOS and Windows only");

#[cfg(all(test, target_os = "linux"))]
mod tests {
    use super::unix::{WORKER_NICE, lower_priority, nice};

    // On Linux the nice value belongs to the calling thread; every test runs in its own
    // thread, so the rest of the test process keeps its priority.

    #[test]
    fn lower_priority_sets_worker_nice() {
        let expected = nice().expect("read the nice value").max(WORKER_NICE);
        lower_priority().expect("raise the nice value");
        assert_eq!(nice().expect("read the nice value"), expected);
        lower_priority().expect("a second call keeps the nice value");
        assert_eq!(nice().expect("read the nice value"), expected);
    }

    #[test]
    fn lower_priority_keeps_a_higher_nice_value() {
        // SAFETY: `setpriority` takes plain integers and touches no memory of the process.
        let rc = unsafe { libc::setpriority(libc::PRIO_PROCESS, 0, 19) };
        assert_eq!(rc, 0);
        lower_priority().expect("an unprivileged thread cannot lower its nice value");
        assert_eq!(nice().expect("read the nice value"), 19);
    }
}
