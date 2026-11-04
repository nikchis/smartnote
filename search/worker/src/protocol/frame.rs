//! Frames of the protocol (spec §7.1): the body length as `u32` little-endian, then the body.

use std::io::{self, Read, Write};

/// Upper limit of a frame body, inclusive: 16 MiB.
pub const MAX_FRAME: usize = 16 << 20;

/// Size of the length prefix.
const HEADER: usize = 4;

/// Failure to read or write a frame. Messages carry no frame content.
#[derive(Debug, thiserror::Error)]
pub enum FrameError {
    /// The stream failed.
    #[error("frame i/o failed")]
    Io(#[from] io::Error),
    /// The declared (or given) body length exceeds [`MAX_FRAME`].
    #[error("frame body of {0} bytes exceeds the limit")]
    TooLarge(u32),
    /// The stream ended inside a frame.
    #[error("stream ended inside a frame")]
    Truncated,
}

/// Reads one frame body.
///
/// Returns `Ok(None)` when the stream ends cleanly before a frame. Allocates at most the declared
/// length, and only after checking it against [`MAX_FRAME`].
///
/// # Errors
///
/// [`FrameError::Truncated`] when the stream ends inside the length prefix or the body,
/// [`FrameError::TooLarge`] when the declared length exceeds [`MAX_FRAME`],
/// [`FrameError::Io`] when reading fails.
pub fn read_frame(r: &mut impl Read) -> Result<Option<Vec<u8>>, FrameError> {
    let mut header = [0; HEADER];
    let mut filled = 0;
    while filled < HEADER {
        match r.read(&mut header[filled..]) {
            Ok(0) if filled == 0 => return Ok(None),
            Ok(0) => return Err(FrameError::Truncated),
            Ok(n) => filled += n,
            Err(err) if err.kind() == io::ErrorKind::Interrupted => {}
            Err(err) => return Err(err.into()),
        }
    }
    let declared = u32::from_le_bytes(header);
    let len = usize::try_from(declared).map_err(|_| FrameError::TooLarge(declared))?;
    if len > MAX_FRAME {
        return Err(FrameError::TooLarge(declared));
    }
    let mut body = vec![0; len];
    r.read_exact(&mut body).map_err(|err| match err.kind() {
        io::ErrorKind::UnexpectedEof => FrameError::Truncated,
        _ => err.into(),
    })?;
    Ok(Some(body))
}

/// Writes one frame and flushes the stream.
///
/// # Errors
///
/// [`FrameError::TooLarge`] when `body` exceeds [`MAX_FRAME`] (nothing is written),
/// [`FrameError::Io`] when writing fails.
pub fn write_frame(w: &mut impl Write, body: &[u8]) -> Result<(), FrameError> {
    let len = u32::try_from(body.len()).unwrap_or(u32::MAX);
    if body.len() > MAX_FRAME {
        return Err(FrameError::TooLarge(len));
    }
    w.write_all(&len.to_le_bytes())?;
    w.write_all(body)?;
    w.flush()?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use std::io::{self, Cursor, Read};

    use super::{FrameError, MAX_FRAME, read_frame, write_frame};

    #[test]
    fn frame_roundtrip() {
        let mut stream = Vec::new();
        write_frame(&mut stream, b"\xa1at").expect("first frame");
        write_frame(&mut stream, b"").expect("empty frame");
        assert_eq!(&stream[..4], &3u32.to_le_bytes());

        let mut r = Cursor::new(stream);
        assert_eq!(
            read_frame(&mut r).expect("first").as_deref(),
            Some(&b"\xa1at"[..])
        );
        assert_eq!(
            read_frame(&mut r).expect("second").as_deref(),
            Some(&b""[..])
        );
        assert!(read_frame(&mut r).expect("end").is_none());
    }

    #[test]
    fn frame_at_limit_is_accepted() {
        let mut stream = u32::try_from(MAX_FRAME).unwrap().to_le_bytes().to_vec();
        stream.resize(4 + MAX_FRAME, 0x5a);
        let body = read_frame(&mut Cursor::new(stream)).expect("frame at the limit");
        assert_eq!(body.map(|b| b.len()), Some(MAX_FRAME));
    }

    #[test]
    fn frame_too_large() {
        let mut r = Cursor::new(0x0100_0001u32.to_le_bytes().to_vec());
        assert!(matches!(
            read_frame(&mut r),
            Err(FrameError::TooLarge(0x0100_0001))
        ));

        let mut out = Vec::new();
        let body = vec![0; MAX_FRAME + 1];
        assert!(matches!(
            write_frame(&mut out, &body),
            Err(FrameError::TooLarge(0x0100_0001))
        ));
        assert!(out.is_empty(), "nothing is written for an oversized body");
    }

    #[test]
    fn frame_truncated() {
        // Inside the length prefix.
        assert!(matches!(
            read_frame(&mut Cursor::new(vec![5, 0])),
            Err(FrameError::Truncated)
        ));
        // Inside the body.
        let mut stream = 5u32.to_le_bytes().to_vec();
        stream.extend_from_slice(b"abc");
        assert!(matches!(
            read_frame(&mut Cursor::new(stream)),
            Err(FrameError::Truncated)
        ));
    }

    #[test]
    fn frame_eof_clean() {
        assert!(
            read_frame(&mut Cursor::new(Vec::new()))
                .expect("clean EOF")
                .is_none()
        );
    }

    /// A reader that yields one byte per call and an `Interrupted` error before each byte.
    struct Trickle {
        data: Vec<u8>,
        pos: usize,
        interrupt: bool,
    }

    impl Read for Trickle {
        fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
            self.interrupt = !self.interrupt;
            if self.interrupt {
                return Err(io::ErrorKind::Interrupted.into());
            }
            let Some(&b) = self.data.get(self.pos) else {
                return Ok(0);
            };
            if buf.is_empty() {
                return Ok(0);
            }
            buf[0] = b;
            self.pos += 1;
            Ok(1)
        }
    }

    #[test]
    fn frame_short_reads_and_interrupts() {
        let mut stream = Vec::new();
        write_frame(&mut stream, "ёж".as_bytes()).expect("frame");
        let mut r = Trickle {
            data: stream,
            pos: 0,
            interrupt: false,
        };
        assert_eq!(
            read_frame(&mut r).expect("frame").as_deref(),
            Some("ёж".as_bytes())
        );
        assert!(read_frame(&mut r).expect("end").is_none());
    }

    #[test]
    fn frame_read_error_is_io() {
        struct Broken;
        impl Read for Broken {
            fn read(&mut self, _: &mut [u8]) -> io::Result<usize> {
                Err(io::ErrorKind::BrokenPipe.into())
            }
        }
        assert!(matches!(read_frame(&mut Broken), Err(FrameError::Io(_))));
    }
}
