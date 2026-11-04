package searchworker

import (
	"bytes"
	"encoding/binary"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

// goldenDir holds the protocol samples Rust writes (spec §12.2).
const goldenDir = "../../../search/testdata/protocol"

// requestGoldens are the samples of messages to the worker; the others are
// replies.
var requestGoldens = map[string]bool{"hello": true, "upsert_with_emb": true, "remove": true, "search": true}

func TestProtocolGoldenFiles(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(goldenDir, "*.cbor"))
	if err != nil {
		t.Fatal(err)
	}

	if len(files) < 11 {
		t.Fatalf("found %d samples in %s, want at least 11", len(files), goldenDir)
	}

	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".cbor")

		t.Run(name, func(t *testing.T) {
			stored, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}

			var msg any
			if requestGoldens[name] {
				msg, err = decodeRequest(stored)
			} else {
				msg, err = decodeReply(stored)
			}

			if err != nil {
				t.Fatalf("decode: %v", err)
			}

			reencoded, err := encodeMessage(msg)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}

			if !bytes.Equal(reencoded, stored) {
				t.Fatalf("re-encoded bytes differ:\n got % x\nwant % x", reencoded, stored)
			}
		})
	}
}

func TestProtocolGoldenValues(t *testing.T) {
	read := func(name string) []byte {
		t.Helper()

		body, err := os.ReadFile(filepath.Join(goldenDir, name+".cbor"))
		if err != nil {
			t.Fatal(err)
		}

		return body
	}

	msg, err := decodeReply(read("result_f16"))
	if err != nil {
		t.Fatal(err)
	}

	result, ok := msg.(*resultMsg)
	if !ok || len(result.Hits) != 1 || result.Hits[0].Score != 0.5 || result.Hits[0].Text != "==" {
		t.Fatalf("result_f16 = %#v", msg)
	}

	msg, err = decodeReply(read("error_no_req"))
	if err != nil {
		t.Fatal(err)
	}

	if e, ok := msg.(*errorMsg); !ok || e.Req != nil || e.Code != "bad_request" {
		t.Fatalf("error_no_req = %#v", msg)
	}

	msg, err = decodeRequest(read("upsert_with_emb"))
	if err != nil {
		t.Fatal(err)
	}

	if u, ok := msg.(*upsertMsg); !ok || len(u.Notes) != 2 || len(u.Notes[0].Emb) != 40 || u.Notes[1].Emb != nil {
		t.Fatalf("upsert_with_emb = %#v", msg)
	}
}

func TestEncodeNilSlicesAsEmptyArrays(t *testing.T) {
	body, err := encodeMessage(&upsertMsg{T: "upsert", Req: 1})
	if err != nil {
		t.Fatal(err)
	}

	// {"t":"upsert","req":1,"notes":[]}: never null, which Rust rejects for a Vec.
	want := []byte("\xa3\x61t\x66upsert\x63req\x01\x65notes\x80")
	if !bytes.Equal(body, want) {
		t.Fatalf("got % x, want % x", body, want)
	}
}

func TestDecodeReplyRejects(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "unknown type", body: "\xa1\x61t\x64nope"},
		{name: "unknown field", body: "\xa3\x61t\x62ok\x63req\x01\x61x\x00"},
		{name: "duplicate field", body: "\xa3\x61t\x62ok\x63req\x01\x63req\x02"},
		{name: "field name in other case", body: "\xa2\x61t\x62ok\x63REQ\x01"},
		{name: "trailing bytes", body: "\xa2\x61t\x62ok\x63req\x01\x00"},
		{name: "indefinite length", body: "\xbf\x61t\x62ok\x63req\x01\xff"},
		{name: "truncated", body: "\xa2\x61t\x62ok\x63re"},
		{name: "not a map", body: "\x01"},
		{name: "unknown state", body: "\xa5\x61t\x66status\x65notes\x00\x66queued\x00\x67lexical\x65ready\x68semantic\x64busy"},
		{name: "unknown source", body: "\xa3\x61t\x66result\x63req\x01\x64hits\x81" +
			"\xa9\x64note\x60\x64line\x00\x65start\x00\x63end\x00\x64text\x60\x67snippet\x60\x65marks\x80\x67sources\x81\x63bad\x65score\xf9\x00\x00"},
		{name: "a request", body: "\xa2\x61t\x65reset\x63req\x01"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if msg, err := decodeReply([]byte(tt.body)); err == nil {
				t.Fatalf("decoded %#v, want an error", msg)
			}
		})
	}
}

func TestDecodeErrorsDoNotQuoteContent(t *testing.T) {
	// {"t":"ok","req":"СЕКРЕТ"}: a mistyped field with text.
	body := append([]byte("\xa2\x61t\x62ok\x63req\x6c"), "СЕКРЕТ"...)

	_, err := decodeReply(body)
	if err == nil {
		t.Fatal("decoded a mistyped req")
	}

	if strings.Contains(err.Error(), "СЕКРЕТ") {
		t.Fatalf("error quotes the content: %v", err)
	}
}

func TestDecodeLimits(t *testing.T) {
	var before runtime.MemStats

	runtime.ReadMemStats(&before)

	t.Run("nesting 17", func(t *testing.T) {
		// {"t":"status","notes":[[[...]]]}: the map and 16 arrays are 17 levels.
		body := append([]byte("\xa2\x61t\x66status\x65notes"), bytes.Repeat([]byte{0x81}, 16)...)
		body = append(body, 0x00)

		_, err := decodeReply(body)
		if _, ok := errors.AsType[*cbor.MaxNestedLevelError](err); !ok {
			t.Fatalf("err = %v, want the nesting limit", err)
		}
	})

	t.Run("array of 65537", func(t *testing.T) {
		// {"t":"result","req":1,"hits":[0, 0, ...]}
		body := []byte("\xa3\x61t\x66result\x63req\x01\x64hits\x9a\x00\x01\x00\x01")
		body = append(body, make([]byte, 65537)...)

		_, err := decodeReply(body)
		if _, ok := errors.AsType[*cbor.MaxArrayElementsError](err); !ok {
			t.Fatalf("err = %v, want the array limit", err)
		}
	})

	t.Run("map of 1025 pairs", func(t *testing.T) {
		body := []byte("\xb9\x04\x01")
		for i := range 1025 {
			body = binary.BigEndian.AppendUint16(append(body, 0x19), uint16(i))
			body = append(body, 0x00)
		}

		_, err := decodeReply(body)
		if _, ok := errors.AsType[*cbor.MaxMapPairsError](err); !ok {
			t.Fatalf("err = %v, want the map limit", err)
		}
	})

	t.Run("frame over 16 MiB", func(t *testing.T) {
		header := binary.LittleEndian.AppendUint32(nil, maxFrame+1)

		_, err := readFrame(bytes.NewReader(header))
		if !errors.Is(err, errFrameTooLarge) {
			t.Fatalf("err = %v, want errFrameTooLarge", err)
		}
	})

	t.Run("byte string longer than the frame", func(t *testing.T) {
		// {"t":"embedded","id":"","rev":0,"emb":h'...'} with a length of 2^62.
		body := []byte("\xa4\x61t\x68embedded\x62id\x60\x63rev\x00\x63emb\x5b\x40\x00\x00\x00\x00\x00\x00\x00")

		if _, err := decodeReply(body); err == nil {
			t.Fatal("decoded a byte string longer than the body")
		}
	})

	var after runtime.MemStats

	runtime.ReadMemStats(&after)

	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 32<<20 {
		t.Fatalf("allocated %d bytes, want at most 32 MiB", allocated)
	}
}

func TestFrameRoundTrip(t *testing.T) {
	var stream bytes.Buffer

	frame, err := encodeFrame(&resetMsg{T: "reset", Req: 3})
	if err != nil {
		t.Fatal(err)
	}

	stream.Write(frame)
	stream.Write(binary.LittleEndian.AppendUint32(nil, 0)) // an empty body

	body, err := readFrame(&stream)
	if err != nil {
		t.Fatal(err)
	}

	if want := "\xa2\x61t\x65reset\x63req\x03"; string(body) != want {
		t.Fatalf("body % x, want % x", body, want)
	}

	if body, err = readFrame(&stream); err != nil || len(body) != 0 {
		t.Fatalf("empty frame: % x, %v", body, err)
	}

	if _, err = readFrame(&stream); !errors.Is(err, errStreamEnd) {
		t.Fatalf("at the end: %v, want errStreamEnd", err)
	}

	if _, err = readFrame(bytes.NewReader([]byte{5, 0, 0, 0, 1})); err == nil || errors.Is(err, errStreamEnd) {
		t.Fatalf("truncated body: %v", err)
	}
}

// The text of an error reply is shown only when it is one of the worker's
// own fixed texts: the worker never puts note text there, and a compromised
// one must not get it into the log (spec §7.2, §10.2).
func TestErrorTextOnlyKnown(t *testing.T) {
	for msg, want := range map[string]string{
		"unexpected hello":     "bad_request: unexpected hello",
		"unsupported protocol": "bad_request: unsupported protocol",
		"note text " + marker:  "bad_request",
		"":                     "bad_request",
	} {
		body, err := encodeMessage(&errorMsg{T: tError, Code: "bad_request", Msg: msg})
		if err != nil {
			t.Fatal(err)
		}

		reply, err := decodeReply(body)
		if err != nil {
			t.Fatal(err)
		}

		e, ok := reply.(*errorMsg)
		out := newWorkerOutput(slog.New(slog.DiscardHandler), workerStderrForms)

		if got := out.errorText(e); !ok || got != want {
			t.Errorf("error with msg %q decoded as %#v, shown as %q, want %q", msg, reply, got, want)
		}
	}
}
