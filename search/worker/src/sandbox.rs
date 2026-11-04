//! Sandbox of the worker on Linux (spec §10.2): once it holds the notes, the worker can neither
//! write to the file system nor open network connections. Other systems have no sandbox in this
//! scope (spec §14).
#![allow(unsafe_code, reason = "prctl through libc; every block is commented")]

use serde::Serialize;

/// How much of the sandbox the kernel enforces; reported in `ready.sandbox` (spec §7.2).
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum SandboxLevel {
    /// Landlock restricts every file-system change and TCP, seccomp denies new sockets.
    Full,
    /// The kernel enforces only part of it (an older kernel or a failed layer).
    Partial,
    /// Nothing is enforced (not Linux, or neither Landlock nor seccomp available).
    None,
}

/// Restricts the current process irreversibly (spec §10.2 pp. 2–4): `PR_SET_NO_NEW_PRIVS`,
/// then Landlock (no file-system changes anywhere, no TCP bind or connect, abstract Unix
/// sockets and signals scoped to the process), then seccomp (`socket`, `socketpair` and
/// `io_uring_setup` fail with `EPERM`). Reading files and mapping them stays allowed.
///
/// Requires the model files to be read and closed already: descriptors opened before stay
/// usable (check them with [`extra_open_fds`]). Restricts the calling thread and the threads
/// it creates later, so call it before any other thread exists. Applies whatever the kernel
/// supports and never fails; the result tells how much. Not Linux: [`SandboxLevel::None`].
#[must_use]
pub fn apply() -> SandboxLevel {
    os::apply()
}

/// Open descriptors of the process other than 0, 1 and 2, read from `/proc/self/fd`.
///
/// Empty when `/proc/self/fd` cannot be read, and on systems other than Linux.
#[must_use]
pub fn extra_open_fds() -> Vec<i32> {
    os::extra_open_fds()
}

#[cfg(target_os = "linux")]
mod os {
    use std::fs;

    use landlock::{
        ABI, Access, AccessFs, AccessNet, BitFlags, CompatLevel, Compatible, LandlockStatus,
        RestrictionStatus, Ruleset, RulesetAttr, RulesetError, RulesetStatus, Scope,
    };
    use seccompiler::{BpfProgram, SeccompAction, SeccompFilter};

    use super::SandboxLevel;

    /// The Landlock ABI the ruleset is written for; older kernels get what they support.
    const LANDLOCK_ABI: ABI = ABI::V6;

    /// System calls that fail with `EPERM` under seccomp: Landlock does not restrict UDP and
    /// other socket families, and `io_uring` would bypass seccomp for socket operations.
    const DENIED_SYSCALLS: [libc::c_long; 3] = [
        libc::SYS_socket,
        libc::SYS_socketpair,
        libc::SYS_io_uring_setup,
    ];

    pub(super) fn apply() -> SandboxLevel {
        // Without no_new_privs an unprivileged process can enforce neither Landlock nor seccomp.
        if !set_no_new_privs() {
            return SandboxLevel::None;
        }
        let landlock_abi = restrict_landlock();
        Enforced {
            fs: coverage(fs_rights(), landlock_abi),
            net: coverage(net_rights(), landlock_abi),
            seccomp: deny_syscalls().is_ok(),
        }
        .level()
    }

    pub(super) fn extra_open_fds() -> Vec<i32> {
        let Ok(entries) = fs::read_dir("/proc/self/fd") else {
            return Vec::new();
        };
        let listed: Vec<i32> = entries
            .filter_map(|entry| entry.ok()?.file_name().to_str()?.parse().ok())
            .filter(|&fd| fd > 2)
            .collect();
        // The listing held the descriptor of the directory itself, closed by now.
        listed
            .into_iter()
            .filter(|fd| fs::symlink_metadata(format!("/proc/self/fd/{fd}")).is_ok())
            .collect()
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

    /// Enforces the Landlock ruleset and returns the ABI it was enforced with, `None` when
    /// Landlock enforces nothing.
    fn restrict_landlock() -> Option<ABI> {
        let restrict = || -> Result<RestrictionStatus, RulesetError> {
            Ruleset::default()
                .set_compatibility(CompatLevel::BestEffort)
                .handle_access(fs_rights())?
                .handle_access(net_rights())?
                .scope(Scope::AbstractUnixSocket | Scope::Signal)?
                .create()?
                .restrict_self()
        };
        let status = restrict().ok()?;
        match (status.ruleset, status.landlock) {
            (RulesetStatus::NotEnforced, _) => None,
            (_, LandlockStatus::Available { effective_abi, .. }) => Some(effective_abi),
            (_, _) => None,
        }
    }

    fn deny_syscalls() -> seccompiler::Result<()> {
        #[allow(clippy::useless_conversion, reason = "c_long is i32 on 32-bit targets")]
        let rules = DENIED_SYSCALLS
            .iter()
            .map(|&nr| (i64::from(nr), Vec::new()))
            .collect();
        let filter = SeccompFilter::new(
            rules,
            SeccompAction::Allow,
            SeccompAction::Errno(libc::EPERM.unsigned_abs()),
            std::env::consts::ARCH.try_into()?,
        )?;
        let program: BpfProgram = filter.try_into()?;
        seccompiler::apply_filter(&program)
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
        seccomp: bool,
    }

    impl Enforced {
        /// `Full` when the file-system rights, the network rights and seccomp all apply
        /// (the Landlock scopes do not count), `None` when nothing does.
        fn level(self) -> SandboxLevel {
            match (self.fs, self.net, self.seccomp) {
                (Coverage::Full, Coverage::Full, true) => SandboxLevel::Full,
                (Coverage::None, Coverage::None, false) => SandboxLevel::None,
                _ => SandboxLevel::Partial,
            }
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
            let cases = [
                (Full, Full, true, SandboxLevel::Full),
                (Full, Full, false, SandboxLevel::Partial),
                (Partial, Full, true, SandboxLevel::Partial),
                (Full, None, true, SandboxLevel::Partial),
                (None, None, true, SandboxLevel::Partial),
                (Partial, None, false, SandboxLevel::Partial),
                (None, None, false, SandboxLevel::None),
            ];
            for (fs, net, seccomp, level) in cases {
                let enforced = Enforced { fs, net, seccomp };
                assert_eq!(enforced.level(), level, "{enforced:?}");
            }
        }

        #[test]
        fn extra_open_fds_lists_open_descriptors_only() {
            let file = File::open("/proc/self/stat").expect("open a file");
            let fds = extra_open_fds();
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
    use super::SandboxLevel;

    pub(super) fn apply() -> SandboxLevel {
        SandboxLevel::None
    }

    pub(super) fn extra_open_fds() -> Vec<i32> {
        Vec::new()
    }
}
