//! Sandbox of the worker on Linux (spec §10.2): once it holds the notes, the worker can neither
//! write to the file system nor open network connections. Other systems have no sandbox in this
//! scope (spec §14).
#![allow(unsafe_code, reason = "prctl through libc; every block is commented")]

use std::io;

use serde::{Deserialize, Serialize};

/// How much of the sandbox the kernel enforces; reported in `ready.sandbox` (spec §7.2).
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum SandboxLevel {
    /// On every thread of the process: Landlock restricts every file-system change and TCP
    /// bind and connect, and seccomp denies new sockets, `io_uring_setup` and the `setxattr`
    /// family (extended attributes are a write to the file system that Landlock does not cover).
    Full,
    /// The kernel enforces only part of it (an older kernel or a failed layer).
    Partial,
    /// Nothing is enforced (not Linux, or neither Landlock nor seccomp available).
    None,
}

/// A part of the sandbox the kernel does not enforce. [`Gap::code`] names it for the log.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Gap {
    /// `PR_SET_NO_NEW_PRIVS` failed, so nothing else could be enforced.
    NoNewPrivs,
    /// Landlock does not handle every file-system right (unavailable, or ABI below 5).
    LandlockFs,
    /// Landlock does not handle TCP bind and connect (unavailable, or ABI below 4).
    LandlockNet,
    /// The seccomp filter has no backend for this architecture.
    SeccompArch,
    /// Installing the seccomp filter failed.
    Seccomp,
    /// Other threads ran during [`apply`] and Landlock could not be synchronised to them.
    Threads,
    /// This system has no sandbox in this scope (not Linux).
    Unsupported,
}

impl Gap {
    /// Short stable code of the gap, for the log.
    #[must_use]
    pub fn code(self) -> &'static str {
        match self {
            Self::NoNewPrivs => "no_new_privs",
            Self::LandlockFs => "landlock_fs",
            Self::LandlockNet => "landlock_net",
            Self::SeccompArch => "seccomp_arch",
            Self::Seccomp => "seccomp",
            Self::Threads => "threads",
            Self::Unsupported => "unsupported_os",
        }
    }
}

/// Result of [`apply`]: the level and, below [`SandboxLevel::Full`], what is missing.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Applied {
    pub level: SandboxLevel,
    /// Empty exactly when `level` is [`SandboxLevel::Full`].
    pub gaps: Vec<Gap>,
}

/// Restricts the current process irreversibly (spec §10.2 pp. 2–4): `PR_SET_NO_NEW_PRIVS`,
/// then Landlock (no file-system changes anywhere, no TCP bind or connect, abstract Unix
/// sockets and signals scoped to the process), then seccomp (`socket`, `socketpair`,
/// `io_uring_setup` and the `setxattr` family fail with `EPERM`). Reading files and mapping
/// them stays allowed.
///
/// Requires the model files to be read and closed already: descriptors opened before stay
/// usable (check them with [`extra_open_fds`]). Call it before any other thread exists: the
/// calling thread and the threads it creates later are always restricted, threads that
/// already run only where the kernel can synchronise them (seccomp always, Landlock since
/// ABI 8); otherwise the result is at most [`SandboxLevel::Partial`]. Applies whatever the
/// kernel supports and never fails; the result tells how much and what is missing. Not Linux:
/// [`SandboxLevel::None`].
#[must_use]
pub fn apply() -> Applied {
    os::apply()
}

/// Open descriptors of the process other than 0, 1 and 2, read from `/proc/self/fd`; empty on
/// systems other than Linux.
///
/// # Errors
///
/// The error of reading `/proc/self/fd`: then nothing is known about open descriptors.
pub fn extra_open_fds() -> io::Result<Vec<i32>> {
    os::extra_open_fds()
}

/// How many of `fds` are open for writing (`O_WRONLY` or `O_RDWR`); a descriptor that is not
/// open counts as not writable. Always 0 on systems other than Linux, where
/// [`extra_open_fds`] lists nothing.
#[must_use]
pub fn writable_fds(fds: &[i32]) -> usize {
    os::writable_fds(fds)
}

#[cfg(target_os = "linux")]
mod os {
    use std::fs;
    use std::io;
    use std::path::Path;

    use landlock::{
        ABI, Access, AccessFs, AccessNet, BitFlags, CompatLevel, Compatible, LandlockStatus,
        RestrictSelfAttr, RestrictionStatus, Ruleset, RulesetAttr, RulesetError, RulesetStatus,
        Scope,
    };
    use seccompiler::{BpfProgram, SeccompAction, SeccompFilter};

    use super::{Applied, Gap, SandboxLevel};

    /// The Landlock ABI the ruleset is written for; older kernels get what they support.
    const LANDLOCK_ABI: ABI = ABI::V6;

    /// `setxattrat` (Linux 6.13): the same number on every architecture; libc 0.2.191 has no
    /// constant for it.
    const SYS_SETXATTRAT: libc::c_long = 463;

    /// System calls that fail with `EPERM` under seccomp: Landlock does not restrict UDP and
    /// other socket families, `io_uring` would bypass seccomp for socket operations, and
    /// Landlock does not restrict extended attributes, which could carry note text to disk.
    const DENIED_SYSCALLS: [libc::c_long; 7] = [
        libc::SYS_socket,
        libc::SYS_socketpair,
        libc::SYS_io_uring_setup,
        libc::SYS_setxattr,
        libc::SYS_lsetxattr,
        libc::SYS_fsetxattr,
        SYS_SETXATTRAT,
    ];

    /// `__X32_SYSCALL_BIT`: x32 system calls pass the `AUDIT_ARCH_X86_64` check of the filter
    /// under their own numbers (`0x4000_0000 | nr` for every call of [`DENIED_SYSCALLS`]).
    #[cfg(target_arch = "x86_64")]
    const X32_SYSCALL_BIT: i64 = 0x4000_0000;

    pub(super) fn apply() -> Applied {
        // Without no_new_privs an unprivileged process can enforce neither Landlock nor seccomp.
        if !set_no_new_privs() {
            return Applied {
                level: SandboxLevel::None,
                gaps: vec![Gap::NoNewPrivs],
            };
        }
        let landlock = restrict_landlock();
        let seccomp = seccomp_outcome(&deny_syscalls());
        let abi = landlock.map(|l| l.abi);
        let enforced = Enforced {
            fs: coverage(fs_rights(), abi),
            net: coverage(net_rights(), abi),
            seccomp,
            // Counted after the restrictions: a single thread now means that every later
            // thread descends from the restricted one.
            every_thread: landlock.is_some_and(|l| l.all_threads) || thread_count() == Some(1),
        };
        Applied {
            level: enforced.level(),
            gaps: enforced.gaps(),
        }
    }

    /// Threads of the process, `None` when `/proc/self/task` cannot be read.
    fn thread_count() -> Option<usize> {
        Some(fs::read_dir("/proc/self/task").ok()?.count())
    }

    pub(super) fn extra_open_fds() -> io::Result<Vec<i32>> {
        list_fds(Path::new("/proc/self/fd"))
    }

    pub(super) fn writable_fds(fds: &[i32]) -> usize {
        fds.iter()
            .filter(|&&fd| {
                // SAFETY: F_GETFL reads the status flags of a plain descriptor number; one that
                // is not open fails with EBADF.
                let flags = unsafe { libc::fcntl(fd, libc::F_GETFL) };
                flags != -1 && matches!(flags & libc::O_ACCMODE, libc::O_WRONLY | libc::O_RDWR)
            })
            .count()
    }

    /// Descriptors above 2 listed in `dir` (a `/proc/<pid>/fd` directory) that are still open.
    fn list_fds(dir: &Path) -> io::Result<Vec<i32>> {
        let listed: Vec<i32> = fs::read_dir(dir)?
            .filter_map(|entry| entry.ok()?.file_name().to_str()?.parse().ok())
            .filter(|&fd| fd > 2)
            .collect();
        // The listing held the descriptor of the directory itself, closed by now.
        Ok(listed
            .into_iter()
            .filter(|fd| fs::symlink_metadata(dir.join(fd.to_string())).is_ok())
            .collect())
    }

    fn set_no_new_privs() -> bool {
        let (on, zero): (libc::c_ulong, libc::c_ulong) = (1, 0);
        // SAFETY: PR_SET_NO_NEW_PRIVS reads only the integer argument; the unused arguments
        // are zero as the kernel requires.
        unsafe { libc::prctl(libc::PR_SET_NO_NEW_PRIVS, on, zero, zero, zero) == 0 }
    }

    /// Every file-system right that changes something; no path is granted any of them.
    /// Reading and executing (`mmap` of a library) are not handled, so they stay allowed.
    fn fs_rights() -> BitFlags<AccessFs> {
        AccessFs::from_write(LANDLOCK_ABI)
            | AccessFs::Truncate
            | AccessFs::IoctlDev
            | AccessFs::Refer
    }

    /// TCP bind and connect; no port is granted.
    fn net_rights() -> BitFlags<AccessNet> {
        AccessNet::BindTcp | AccessNet::ConnectTcp
    }

    /// The enforced Landlock domain.
    #[derive(Clone, Copy, Debug)]
    struct Landlock {
        /// The ABI it was enforced with.
        abi: ABI,
        /// It applies to every thread of the process (`LANDLOCK_RESTRICT_SELF_TSYNC`, ABI 8),
        /// not only to the calling thread.
        all_threads: bool,
    }

    /// Enforces the Landlock ruleset on every thread where the kernel can (ABI 8), otherwise
    /// on the calling thread; `None` when Landlock enforces nothing.
    fn restrict_landlock() -> Option<Landlock> {
        let restrict = || -> Result<RestrictionStatus, RulesetError> {
            Ruleset::default()
                .set_compatibility(CompatLevel::BestEffort)
                .handle_access(fs_rights())?
                .handle_access(net_rights())?
                .scope(Scope::AbstractUnixSocket | Scope::Signal)?
                .create()?
                .all_threads(true)?
                .restrict_self()
        };
        let status = restrict().ok()?;
        match (status.ruleset, status.landlock) {
            (RulesetStatus::NotEnforced, _) => None,
            (_, LandlockStatus::Available { effective_abi, .. }) => Some(Landlock {
                abi: effective_abi,
                all_threads: status.all_threads,
            }),
            (_, _) => None,
        }
    }

    /// The numbers of [`DENIED_SYSCALLS`] for the filter, with their x32 aliases on `x86_64`.
    fn denied_syscall_numbers() -> Vec<i64> {
        #[allow(clippy::useless_conversion, reason = "c_long is i32 on 32-bit targets")]
        let native = DENIED_SYSCALLS.iter().map(|&nr| i64::from(nr));
        #[cfg(target_arch = "x86_64")]
        let native = native.flat_map(|nr| [nr, X32_SYSCALL_BIT | nr]);
        native.collect()
    }

    /// What installing the seccomp filter achieved.
    #[derive(Clone, Copy, Debug, PartialEq, Eq)]
    enum Seccomp {
        Applied,
        /// seccompiler has no backend for the architecture of the build.
        UnsupportedArch,
        Failed,
    }

    fn seccomp_outcome(result: &seccompiler::Result<()>) -> Seccomp {
        match result {
            Ok(()) => Seccomp::Applied,
            Err(seccompiler::Error::Backend(seccompiler::BackendError::InvalidTargetArch(_))) => {
                Seccomp::UnsupportedArch
            }
            Err(_) => Seccomp::Failed,
        }
    }

    /// Installs the seccomp filter on every thread of the process (TSYNC).
    fn deny_syscalls() -> seccompiler::Result<()> {
        let rules = denied_syscall_numbers()
            .into_iter()
            .map(|nr| (nr, Vec::new()))
            .collect();
        let filter = SeccompFilter::new(
            rules,
            SeccompAction::Allow,
            SeccompAction::Errno(libc::EPERM.unsigned_abs()),
            std::env::consts::ARCH.try_into()?,
        )?;
        let program: BpfProgram = filter.try_into()?;
        seccompiler::apply_filter_all_threads(&program)
    }

    /// How much of one Landlock layer is enforced.
    #[derive(Clone, Copy, Debug, PartialEq, Eq)]
    enum Coverage {
        None,
        Partial,
        Full,
    }

    /// The share of `requested` that a ruleset enforced with `abi` handles.
    fn coverage<A: Access>(requested: BitFlags<A>, abi: Option<ABI>) -> Coverage {
        let Some(abi) = abi else {
            return Coverage::None;
        };
        let handled = requested & A::from_all(abi);
        if handled == requested {
            Coverage::Full
        } else if handled.is_empty() {
            Coverage::None
        } else {
            Coverage::Partial
        }
    }

    /// What the kernel enforces after [`apply`].
    #[derive(Clone, Copy, Debug)]
    struct Enforced {
        fs: Coverage,
        net: Coverage,
        seccomp: Seccomp,
        /// Landlock restricts every thread of the process: it was synchronised to all of
        /// them, or the process had a single thread.
        every_thread: bool,
    }

    impl Enforced {
        /// `Full` when the file-system rights, the network rights and seccomp all apply
        /// (the Landlock scopes do not count) to every thread, `None` when nothing applies.
        fn level(self) -> SandboxLevel {
            let seccomp = self.seccomp == Seccomp::Applied;
            match (self.fs, self.net, seccomp, self.every_thread) {
                (Coverage::Full, Coverage::Full, true, true) => SandboxLevel::Full,
                (Coverage::None, Coverage::None, false, _) => SandboxLevel::None,
                _ => SandboxLevel::Partial,
            }
        }

        /// What keeps the level below `Full`, in a fixed order.
        fn gaps(self) -> Vec<Gap> {
            let seccomp = match self.seccomp {
                Seccomp::Applied => None,
                Seccomp::UnsupportedArch => Some(Gap::SeccompArch),
                Seccomp::Failed => Some(Gap::Seccomp),
            };
            [
                (self.fs != Coverage::Full).then_some(Gap::LandlockFs),
                (self.net != Coverage::Full).then_some(Gap::LandlockNet),
                seccomp,
                (!self.every_thread).then_some(Gap::Threads),
            ]
            .into_iter()
            .flatten()
            .collect()
        }
    }

    #[cfg(test)]
    mod tests {
        use std::fs::File;
        use std::os::fd::AsRawFd;

        use super::*;

        #[test]
        fn coverage_follows_landlock_abi() {
            let cases = [
                (None, Coverage::None, Coverage::None),
                (Some(ABI::V1), Coverage::Partial, Coverage::None),
                (Some(ABI::V3), Coverage::Partial, Coverage::None),
                (Some(ABI::V4), Coverage::Partial, Coverage::Full),
                (Some(ABI::V5), Coverage::Full, Coverage::Full),
                (Some(ABI::V6), Coverage::Full, Coverage::Full),
                (Some(ABI::V8), Coverage::Full, Coverage::Full),
            ];
            for (abi, fs, net) in cases {
                assert_eq!(coverage(fs_rights(), abi), fs, "fs, {abi:?}");
                assert_eq!(coverage(net_rights(), abi), net, "net, {abi:?}");
            }
        }

        #[test]
        fn fs_rights_cover_every_change_and_no_read() {
            let rights = fs_rights();
            assert!(rights.contains(AccessFs::from_write(ABI::V6)));
            for right in [AccessFs::Truncate, AccessFs::IoctlDev, AccessFs::Refer] {
                assert!(rights.contains(right), "{right:?}");
            }
            assert!(
                !rights.intersects(AccessFs::from_read(ABI::V6)),
                "{rights:?}"
            );
        }

        #[test]
        fn level_is_full_only_with_every_layer() {
            use Coverage::{Full, None, Partial};
            let on = Seccomp::Applied;
            let off = Seccomp::Failed;
            let cases = [
                (Full, Full, on, true, SandboxLevel::Full),
                (Full, Full, off, true, SandboxLevel::Partial),
                (
                    Full,
                    Full,
                    Seccomp::UnsupportedArch,
                    true,
                    SandboxLevel::Partial,
                ),
                (Partial, Full, on, true, SandboxLevel::Partial),
                (Full, None, on, true, SandboxLevel::Partial),
                (None, None, on, true, SandboxLevel::Partial),
                (Partial, None, off, true, SandboxLevel::Partial),
                (None, None, off, true, SandboxLevel::None),
                // Other threads ran and Landlock could not be synchronised to them (ABI < 8).
                (Full, Full, on, false, SandboxLevel::Partial),
                (None, None, off, false, SandboxLevel::None),
            ];
            for (fs, net, seccomp, every_thread, level) in cases {
                let enforced = Enforced {
                    fs,
                    net,
                    seccomp,
                    every_thread,
                };
                assert_eq!(enforced.level(), level, "{enforced:?}");
            }
        }

        #[test]
        fn gaps_name_every_missing_layer() {
            use Coverage::{Full, None, Partial};
            let cases = [
                (Full, Full, Seccomp::Applied, true, vec![]),
                (
                    Partial,
                    None,
                    Seccomp::UnsupportedArch,
                    false,
                    vec![
                        Gap::LandlockFs,
                        Gap::LandlockNet,
                        Gap::SeccompArch,
                        Gap::Threads,
                    ],
                ),
                (
                    Full,
                    Partial,
                    Seccomp::Failed,
                    true,
                    vec![Gap::LandlockNet, Gap::Seccomp],
                ),
            ];
            for (fs, net, seccomp, every_thread, gaps) in cases {
                let enforced = Enforced {
                    fs,
                    net,
                    seccomp,
                    every_thread,
                };
                assert_eq!(enforced.gaps(), gaps, "{enforced:?}");
            }
        }

        #[test]
        fn seccomp_outcome_tells_unsupported_arch_apart() {
            let arch = seccompiler::Error::Backend(seccompiler::BackendError::InvalidTargetArch(
                "pdp11".into(),
            ));
            assert_eq!(seccomp_outcome(&Err(arch)), Seccomp::UnsupportedArch);
            assert_eq!(
                seccomp_outcome(&Err(seccompiler::Error::EmptyFilter)),
                Seccomp::Failed
            );
            assert_eq!(seccomp_outcome(&Ok(())), Seccomp::Applied);
        }

        #[test]
        fn gap_codes_are_distinct() {
            let gaps = [
                Gap::NoNewPrivs,
                Gap::LandlockFs,
                Gap::LandlockNet,
                Gap::SeccompArch,
                Gap::Seccomp,
                Gap::Threads,
                Gap::Unsupported,
            ];
            let codes: std::collections::HashSet<&str> = gaps.iter().map(|g| g.code()).collect();
            assert_eq!(codes.len(), gaps.len(), "{codes:?}");
        }

        #[test]
        fn listing_fds_of_a_missing_directory_fails() {
            assert!(list_fds(std::path::Path::new("/nonexistent/fd")).is_err());
        }

        #[test]
        fn deny_list_covers_sockets_io_uring_and_xattrs() {
            let numbers = denied_syscall_numbers();
            let native = [
                libc::SYS_socket,
                libc::SYS_socketpair,
                libc::SYS_io_uring_setup,
                libc::SYS_setxattr,
                libc::SYS_lsetxattr,
                libc::SYS_fsetxattr,
                SYS_SETXATTRAT,
            ];
            for nr in native {
                assert!(numbers.contains(&nr), "{nr}: {numbers:?}");
                #[cfg(target_arch = "x86_64")]
                assert!(
                    numbers.contains(&(0x4000_0000 | nr)),
                    "x32 {nr}: {numbers:?}"
                );
            }
            let per_call = if cfg!(target_arch = "x86_64") { 2 } else { 1 };
            assert_eq!(numbers.len(), native.len() * per_call, "{numbers:?}");
        }

        #[cfg(target_arch = "x86_64")]
        #[test]
        fn deny_list_matches_kernel_numbers() {
            // From arch/x86/entry/syscalls/syscall_64.tbl; x32 adds 0x4000_0000.
            assert_eq!(
                denied_syscall_numbers(),
                [
                    41,
                    0x4000_0029,
                    53,
                    0x4000_0035,
                    425,
                    0x4000_01a9,
                    188,
                    0x4000_00bc,
                    189,
                    0x4000_00bd,
                    190,
                    0x4000_00be,
                    463,
                    0x4000_01cf,
                ]
            );
        }

        #[test]
        fn writable_fds_counts_write_access() {
            let (reader, writer) = std::io::pipe().expect("pipe");
            let read_only = File::open("/proc/self/stat").expect("open a file");
            let fds = [
                reader.as_raw_fd(),
                writer.as_raw_fd(),
                read_only.as_raw_fd(),
            ];
            assert_eq!(writable_fds(&fds), 1);
            assert_eq!(
                writable_fds(&[-1]),
                0,
                "a closed descriptor is not writable"
            );
        }

        #[test]
        fn extra_open_fds_lists_open_descriptors_only() {
            let file = File::open("/proc/self/stat").expect("open a file");
            let fds = extra_open_fds().expect("/proc/self/fd is readable");
            assert!(fds.contains(&file.as_raw_fd()), "{fds:?}");
            for fd in fds {
                assert!(fd > 2, "{fd}");
                // The descriptor of the listing itself is closed and must not be reported.
                assert!(
                    fs::symlink_metadata(format!("/proc/self/fd/{fd}")).is_ok(),
                    "{fd} is not open"
                );
            }
        }
    }
}

#[cfg(not(target_os = "linux"))]
mod os {
    use std::io;

    use super::{Applied, Gap, SandboxLevel};

    pub(super) fn apply() -> Applied {
        Applied {
            level: SandboxLevel::None,
            gaps: vec![Gap::Unsupported],
        }
    }

    pub(super) fn extra_open_fds() -> io::Result<Vec<i32>> {
        Ok(Vec::new())
    }

    pub(super) fn writable_fds(_fds: &[i32]) -> usize {
        0
    }
}
