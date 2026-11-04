package searchworker

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"reflect"

	"github.com/fxamacker/cbor/v2"
)

// Protocol of the worker (spec §7): frames of a u32 little-endian body length
// and a body, a CBOR map with the message type in "t" and the fields of the
// message. The structs below declare the fields in the order of the Rust
// definitions in search/worker/src/protocol/messages.rs, "t" first: the
// encoder keeps that order, so both sides write the same bytes
// (search/testdata/protocol).

// protoVersion is the protocol version; Smartnote and the worker ship
// together and require an exact match.
const protoVersion = 1

// maxFrame is the largest frame body, inclusive: 16 MiB.
const maxFrame = 16 << 20

// frameHeader is the size of the length prefix.
const frameHeader = 4

var (
	// errFrameTooLarge means a declared or given body is over maxFrame.
	errFrameTooLarge = errors.New("frame body exceeds 16 MiB")
	// errStreamEnd means the stream ended cleanly before a frame.
	errStreamEnd = errors.New("stream ended")
	// errMessage means a body is no valid message. Its text never quotes the
	// body: decoder messages may contain note or query text.
	errMessage = errors.New("invalid message")
)

// Message types.
const (
	tHello    = "hello"
	tReset    = "reset"
	tUpsert   = "upsert"
	tRemove   = "remove"
	tSearch   = "search"
	tReady    = "ready"
	tOK       = "ok"
	tResult   = "result"
	tEmbedded = "embedded"
	tStatus   = "status"
	tError    = "error"
)

// helloMsg is the first message: version check and worker settings.
type helloMsg struct {
	T         string `cbor:"t"`
	Proto     uint32 `cbor:"proto"`
	ModelDir  string `cbor:"model_dir"`
	OrtLib    string `cbor:"ort_lib"`
	Threads   uint16 `cbor:"threads"`
	ParentPID uint32 `cbor:"parent_pid"`
}

type resetMsg struct {
	T   string `cbor:"t"`
	Req uint64 `cbor:"req"`
}

type upsertMsg struct {
	T     string   `cbor:"t"`
	Req   uint64   `cbor:"req"`
	Notes []noteIn `cbor:"notes"`
}

// noteIn is a note to index; Emb is omitted when there is none.
type noteIn struct {
	ID   string `cbor:"id"`
	Rev  uint64 `cbor:"rev"`
	Name string `cbor:"name"`
	Info string `cbor:"info"`
	Emb  []byte `cbor:"emb,omitempty"`
}

type removeMsg struct {
	T     string      `cbor:"t"`
	Req   uint64      `cbor:"req"`
	Items []removalIn `cbor:"items"`
}

type removalIn struct {
	ID  string `cbor:"id"`
	Rev uint64 `cbor:"rev"`
}

type searchMsg struct {
	T     string `cbor:"t"`
	Req   uint64 `cbor:"req"`
	Query string `cbor:"query"`
	Limit uint16 `cbor:"limit"`
}

// readyMsg answers hello.
type readyMsg struct {
	T        string `cbor:"t"`
	Worker   string `cbor:"worker"`
	Index    []byte `cbor:"index"`
	Lexical  string `cbor:"lexical"`
	Semantic string `cbor:"semantic"`
	Sandbox  string `cbor:"sandbox"`
}

// okMsg answers reset, upsert and remove.
type okMsg struct {
	T   string `cbor:"t"`
	Req uint64 `cbor:"req"`
}

// resultMsg answers search.
type resultMsg struct {
	T    string   `cbor:"t"`
	Req  uint64   `cbor:"req"`
	Hits []hitMsg `cbor:"hits"`
}

type hitMsg struct {
	Note    string      `cbor:"note"`
	Line    uint32      `cbor:"line"`
	Start   uint32      `cbor:"start"`
	End     uint32      `cbor:"end"`
	Text    string      `cbor:"text"`
	Snippet string      `cbor:"snippet"`
	Marks   [][2]uint32 `cbor:"marks"`
	Sources []string    `cbor:"sources"`
	Score   float32     `cbor:"score"`
}

// embeddedMsg carries a new embedding record of a note.
type embeddedMsg struct {
	T   string `cbor:"t"`
	ID  string `cbor:"id"`
	Rev uint64 `cbor:"rev"`
	Emb []byte `cbor:"emb"`
}

// statusMsg reports the progress of indexing.
type statusMsg struct {
	T        string `cbor:"t"`
	Notes    uint32 `cbor:"notes"`
	Queued   uint32 `cbor:"queued"`
	Lexical  string `cbor:"lexical"`
	Semantic string `cbor:"semantic"`
}

// errorMsg is the failure of request Req, or of the worker when Req is nil.
// Msg never contains note or query text.
type errorMsg struct {
	T    string  `cbor:"t"`
	Req  *uint64 `cbor:"req,omitempty"`
	Code string  `cbor:"code"`
	Msg  string  `cbor:"msg"`
}

// codeUnsupportedProto is the error code of a worker that speaks another
// protocol version.
const codeUnsupportedProto = "unsupported_proto"

// Values of the enumerations.
var (
	validStates   = map[string]bool{"loading": true, "ready": true, "unavailable": true}
	validSandbox  = map[string]bool{"full": true, "partial": true, "none": true}
	validSources  = map[string]bool{"words": true, "substring": true, "semantic": true}
	validErrCodes = map[string]bool{"bad_request": true, codeUnsupportedProto: true, "model_unavailable": true, "internal": true}
)

// Message types by tag.
var (
	requestTypes = map[string]reflect.Type{
		tHello:  reflect.TypeFor[helloMsg](),
		tReset:  reflect.TypeFor[resetMsg](),
		tUpsert: reflect.TypeFor[upsertMsg](),
		tRemove: reflect.TypeFor[removeMsg](),
		tSearch: reflect.TypeFor[searchMsg](),
	}
	replyTypes = map[string]reflect.Type{
		tReady:    reflect.TypeFor[readyMsg](),
		tOK:       reflect.TypeFor[okMsg](),
		tResult:   reflect.TypeFor[resultMsg](),
		tEmbedded: reflect.TypeFor[embeddedMsg](),
		tStatus:   reflect.TypeFor[statusMsg](),
		tError:    reflect.TypeFor[errorMsg](),
	}
)

var (
	encMode = mustEncMode()
	// decMode decodes a message strictly, like serde with deny_unknown_fields.
	decMode = mustDecMode(cbor.ExtraDecErrorUnknownField)
	// tagMode reads only "t" and skips the other fields.
	tagMode = mustDecMode(cbor.ExtraDecErrorNone)
)

func mustEncMode() cbor.EncMode {
	mode, err := cbor.EncOptions{
		Sort:          cbor.SortNone,        // declaration order, "t" first
		ShortestFloat: cbor.ShortestFloat16, // as ciborium: the shortest lossless float
		IndefLength:   cbor.IndefLengthForbidden,
		NilContainers: cbor.NilContainerAsEmpty, // Rust rejects null for a Vec
	}.EncMode()
	if err != nil {
		panic(fmt.Sprintf("searchworker: cbor encoder options: %v", err))
	}

	return mode
}

func mustDecMode(extra cbor.ExtraDecErrorCond) cbor.DecMode {
	mode, err := cbor.DecOptions{
		DupMapKey:         cbor.DupMapKeyEnforcedAPF,
		MaxNestedLevels:   16,
		MaxArrayElements:  65536,
		MaxMapPairs:       1024,
		IndefLength:       cbor.IndefLengthForbidden,
		TagsMd:            cbor.TagsForbidden,
		FieldNameMatching: cbor.FieldNameMatchingCaseSensitive,
		ExtraReturnErrors: extra,
	}.DecMode()
	if err != nil {
		panic(fmt.Sprintf("searchworker: cbor decoder options: %v", err))
	}

	return mode
}

// encodeMessage encodes msg, a pointer to one of the message structs.
func encodeMessage(msg any) ([]byte, error) {
	body, err := encMode.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("encode %T: %w", msg, err)
	}

	return body, nil
}

// encodeFrame encodes msg with its length prefix, ready for one write.
func encodeFrame(msg any) ([]byte, error) {
	body, err := encodeMessage(msg)
	if err != nil {
		return nil, err
	}

	if len(body) > maxFrame {
		return nil, fmt.Errorf("encode %T: %w", msg, errFrameTooLarge)
	}

	frame := binary.LittleEndian.AppendUint32(make([]byte, 0, frameHeader+len(body)), uint32(len(body))) //nolint:gosec // len(body) <= maxFrame
	frame = append(frame, body...)

	return frame, nil
}

// readFrame reads one frame body. It returns errStreamEnd when r ends before
// a frame and errFrameTooLarge, before allocating, when the declared length is
// over maxFrame.
func readFrame(r io.Reader) ([]byte, error) {
	var header [frameHeader]byte

	if _, err := io.ReadFull(r, header[:]); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errStreamEnd
		}

		return nil, fmt.Errorf("read frame header: %w", err)
	}

	n := binary.LittleEndian.Uint32(header[:])
	if n > maxFrame {
		return nil, fmt.Errorf("frame of %d bytes: %w", n, errFrameTooLarge)
	}

	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("read frame body: %w", err)
	}

	return body, nil
}

// decodeRequest decodes a message to the worker; the fake worker of the tests
// and the protocol samples use it.
func decodeRequest(body []byte) (any, error) {
	return decodeTagged(body, requestTypes)
}

// decodeReply decodes and validates a message from the worker into a pointer
// to its struct.
func decodeReply(body []byte) (any, error) {
	msg, err := decodeTagged(body, replyTypes)
	if err != nil {
		return nil, err
	}

	if err := validateReply(msg); err != nil {
		return nil, err
	}

	return msg, nil
}

// decodeTagged decodes body into the type that types gives for its tag. The
// whole body must be one message. Decoder limit errors are kept, so the
// caller sees which limit; other decoder errors are replaced, since their
// text may quote the body.
func decodeTagged(body []byte, types map[string]reflect.Type) (any, error) {
	var tag struct {
		T string `cbor:"t"`
	}

	if err := tagMode.Unmarshal(body, &tag); err != nil {
		return nil, decodeError(err)
	}

	typ, ok := types[tag.T]
	if !ok {
		return nil, fmt.Errorf("%w: unknown type", errMessage)
	}

	msg := reflect.New(typ).Interface()
	if err := decMode.Unmarshal(body, msg); err != nil {
		return nil, decodeError(err)
	}

	return msg, nil
}

// decodeError keeps the limit errors, which carry no content, and replaces
// the others.
func decodeError(err error) error {
	if _, ok := errors.AsType[*cbor.MaxNestedLevelError](err); ok {
		return err
	}

	if _, ok := errors.AsType[*cbor.MaxArrayElementsError](err); ok {
		return err
	}

	if _, ok := errors.AsType[*cbor.MaxMapPairsError](err); ok {
		return err
	}

	return errMessage
}

// validateReply checks the enumerations, which decode as plain strings.
func validateReply(msg any) error {
	var ok bool

	switch m := msg.(type) {
	case *readyMsg:
		ok = validStates[m.Lexical] && validStates[m.Semantic] && validSandbox[m.Sandbox]
	case *statusMsg:
		ok = validStates[m.Lexical] && validStates[m.Semantic]
	case *errorMsg:
		ok = validErrCodes[m.Code]
	case *resultMsg:
		ok = validHits(m.Hits)
	default:
		ok = true
	}

	if !ok {
		return fmt.Errorf("%w: unknown value of an enumeration", errMessage)
	}

	return nil
}

func validHits(hits []hitMsg) bool {
	for _, h := range hits {
		for _, s := range h.Sources {
			if !validSources[s] {
				return false
			}
		}
	}

	return true
}
