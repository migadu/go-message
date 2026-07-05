package textproto

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"net/textproto"
	"sort"
	"strings"
)

type headerField struct {
	b []byte // Raw header field, including whitespace

	k string
	v string
}

func newHeaderField(k, v string, b []byte) *headerField {
	return &headerField{k: textproto.CanonicalMIMEHeaderKey(k), v: v, b: b}
}

func (f *headerField) raw() ([]byte, error) {
	if f.b != nil {
		return f.b, nil
	} else {
		// An empty field name is malformed (RFC 5322 section 2.2). ReadHeader
		// preserves such fields via their raw bytes for round-trip fidelity,
		// but refuse to freshly generate an illegal ": value" line.
		if f.k == "" {
			return nil, fmt.Errorf("field name is empty")
		}

		for pos, ch := range f.k {
			// check if character is a printable US-ASCII except ':'
			if !(ch >= '!' && ch < ':' || ch > ':' && ch <= '~') {
				return nil, fmt.Errorf("field name contains incorrect symbols (\\x%x at %v)", ch, pos)
			}
		}

		if pos := strings.IndexAny(f.v, "\r\n"); pos != -1 {
			return nil, fmt.Errorf("field value contains \\r\\n (at %v)", pos)
		}

		return []byte(formatHeaderField(f.k, f.v)), nil
	}
}

// A Header represents the key-value pairs in a message header.
//
// The header representation is idempotent: if the header can be read and
// written, the result will be exactly the same as the original (including
// whitespace and header field ordering). This is required for e.g. DKIM.
//
// Byte-exact idempotency requires standard CRLF line endings. Input using
// bare-LF line endings (which violates RFC 5322) is normalized to CRLF on
// write.
//
// Mutating the header is restricted: the only two allowed operations are
// inserting a new header field at the top and deleting a header field. This is
// again necessary for DKIM.
type Header struct {
	// Fields are in reverse order so that inserting a new field at the top is
	// cheap.
	l []*headerField
	m map[string][]*headerField
}

func makeHeaderMap(fs []*headerField) map[string][]*headerField {
	if len(fs) == 0 {
		return nil
	}

	m := make(map[string][]*headerField, len(fs))
	for i, f := range fs {
		m[f.k] = append(m[f.k], fs[i])
	}
	return m
}

func newHeader(fs []*headerField) Header {
	// Reverse order
	for i := len(fs)/2 - 1; i >= 0; i-- {
		opp := len(fs) - 1 - i
		fs[i], fs[opp] = fs[opp], fs[i]
	}

	return Header{l: fs, m: makeHeaderMap(fs)}
}

// HeaderFromMap creates a header from a map of header fields.
//
// This function is provided for interoperability with the standard library.
// If possible, ReadHeader should be used instead to avoid loosing information.
// The map representation looses the ordering of the fields, the capitalization
// of the header keys, and the whitespace of the original header.
func HeaderFromMap(m map[string][]string) Header {
	fs := make([]*headerField, 0, len(m))
	for k, vs := range m {
		for _, v := range vs {
			fs = append(fs, newHeaderField(k, v, nil))
		}
	}

	sort.SliceStable(fs, func(i, j int) bool {
		return fs[i].k < fs[j].k
	})

	return newHeader(fs)
}

// AddRaw adds the raw key, value pair to the header.
//
// The supplied byte slice must be a single complete field in the "Key: Value"
// form including a trailing newline, with the colon on the first physical
// line. Interior line breaks are only permitted as folding (a newline
// immediately followed by a space or tab). AddRaw panics if there is no colon,
// or if the field is otherwise malformed (e.g. it contains an embedded
// unfolded newline that would smuggle an additional field or a premature blank
// line). A bare CR is accepted as an ordinary value byte, consistent with
// ReadHeader, so any field obtained from HeaderFields.Raw can be added back.
// No changes are made to kv contents and it will be copied into WriteHeader
// output as is.
//
// kv is directly added to the underlying structure and therefore should not be
// modified after the AddRaw call.
func (h *Header) AddRaw(kv []byte) {
	colon := bytes.IndexByte(kv, ':')
	if colon == -1 {
		panic("textproto: Header.AddRaw: missing colon")
	}
	if !isValidRawField(kv) {
		panic("textproto: Header.AddRaw: malformed raw field")
	}
	k := textproto.CanonicalMIMEHeaderKey(string(trim(kv[:colon])))
	v := trimAroundNewlines(kv[colon+1:])

	if h.m == nil {
		h.m = make(map[string][]*headerField)
	}

	f := newHeaderField(k, v, kv)
	h.l = append(h.l, f)
	h.m[k] = append(h.m[k], f)
}

// Add adds the key, value pair to the header. It prepends to any existing
// fields associated with key.
//
// Key and value should obey character requirements of RFC 6532.
// If you need to format or fold lines manually, use AddRaw.
func (h *Header) Add(k, v string) {
	k = textproto.CanonicalMIMEHeaderKey(k)

	if h.m == nil {
		h.m = make(map[string][]*headerField)
	}

	f := newHeaderField(k, v, nil)
	h.l = append(h.l, f)
	h.m[k] = append(h.m[k], f)
}

// Get gets the first value associated with the given key. If there are no
// values associated with the key, Get returns "".
func (h *Header) Get(k string) string {
	fields := h.m[textproto.CanonicalMIMEHeaderKey(k)]
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1].v
}

// Raw gets the first raw header field associated with the given key.
//
// The returned bytes contain a complete field in the "Key: value" form,
// including trailing CRLF.
//
// The returned slice should not be modified and becomes invalid when the
// header is updated.
//
// An error is returned if the header field contains incorrect characters (see
// RFC 6532).
func (h *Header) Raw(k string) ([]byte, error) {
	fields := h.m[textproto.CanonicalMIMEHeaderKey(k)]
	if len(fields) == 0 {
		return nil, nil
	}
	return fields[len(fields)-1].raw()
}

// Values returns all values associated with the given key.
//
// The returned slice should not be modified and becomes invalid when the
// header is updated.
func (h *Header) Values(k string) []string {
	fields := h.m[textproto.CanonicalMIMEHeaderKey(k)]
	if len(fields) == 0 {
		return nil
	}
	l := make([]string, len(fields))
	for i, field := range fields {
		l[len(fields)-i-1] = field.v
	}
	return l
}

// Set sets the header fields associated with key to the single field value.
// It replaces any existing values associated with key.
func (h *Header) Set(k, v string) {
	h.Del(k)
	h.Add(k, v)
}

// Del deletes the values associated with key.
func (h *Header) Del(k string) {
	k = textproto.CanonicalMIMEHeaderKey(k)

	delete(h.m, k)

	// Delete existing keys
	for i := len(h.l) - 1; i >= 0; i-- {
		if h.l[i].k == k {
			h.l = append(h.l[:i], h.l[i+1:]...)
		}
	}
}

// Has checks whether the header has a field with the specified key.
func (h *Header) Has(k string) bool {
	_, ok := h.m[textproto.CanonicalMIMEHeaderKey(k)]
	return ok
}

// Copy creates an independent copy of the header.
func (h *Header) Copy() Header {
	l := make([]*headerField, len(h.l))
	copy(l, h.l)
	m := makeHeaderMap(l)
	return Header{l: l, m: m}
}

// Len returns the number of fields in the header.
func (h *Header) Len() int {
	return len(h.l)
}

// Map returns all header fields as a map.
//
// This function is provided for interoperability with the standard library.
// If possible, Fields should be used instead to avoid loosing information.
// The map representation looses the ordering of the fields, the capitalization
// of the header keys, and the whitespace of the original header.
func (h *Header) Map() map[string][]string {
	m := make(map[string][]string, h.Len())
	fields := h.Fields()
	for fields.Next() {
		m[fields.Key()] = append(m[fields.Key()], fields.Value())
	}
	return m
}

// HeaderFields iterates over header fields. Its cursor starts before the first
// field of the header. Use Next to advance from field to field.
type HeaderFields interface {
	// Next advances to the next header field. It returns true on success, or
	// false if there is no next field.
	Next() (more bool)
	// Key returns the key of the current field.
	Key() string
	// Value returns the value of the current field.
	Value() string
	// Raw returns the raw current header field. See Header.Raw.
	Raw() ([]byte, error)
	// Del deletes the current field.
	Del()
	// Len returns the amount of header fields in the subset of header iterated
	// by this HeaderFields instance.
	//
	// For Fields(), it will return the amount of fields in the whole header section.
	// For FieldsByKey(), it will return the amount of fields with certain key.
	Len() int

	headerFields()
}

type headerFields struct {
	h   *Header
	cur int
}

func (*headerFields) headerFields() {}

func (fs *headerFields) Next() bool {
	fs.cur++
	return fs.cur < len(fs.h.l)
}

func (fs *headerFields) index() int {
	if fs.cur < 0 {
		panic("message: HeaderFields method called before Next")
	}
	if fs.cur >= len(fs.h.l) {
		panic("message: HeaderFields method called after Next returned false")
	}
	return len(fs.h.l) - fs.cur - 1
}

func (fs *headerFields) field() *headerField {
	return fs.h.l[fs.index()]
}

func (fs *headerFields) Key() string {
	return fs.field().k
}

func (fs *headerFields) Value() string {
	return fs.field().v
}

func (fs *headerFields) Raw() ([]byte, error) {
	return fs.field().raw()
}

func (fs *headerFields) Del() {
	f := fs.field()

	ok := false
	for i, ff := range fs.h.m[f.k] {
		if ff == f {
			ok = true
			fs.h.m[f.k] = append(fs.h.m[f.k][:i], fs.h.m[f.k][i+1:]...)
			if len(fs.h.m[f.k]) == 0 {
				delete(fs.h.m, f.k)
			}
			break
		}
	}
	if !ok {
		panic("message: field not found in Header.m")
	}

	fs.h.l = append(fs.h.l[:fs.index()], fs.h.l[fs.index()+1:]...)
	fs.cur--
}

func (fs *headerFields) Len() int {
	return len(fs.h.l)
}

// Fields iterates over all the header fields.
//
// The header may not be mutated while iterating, except using HeaderFields.Del.
func (h *Header) Fields() HeaderFields {
	return &headerFields{h, -1}
}

type headerFieldsByKey struct {
	h   *Header
	k   string
	cur int
}

func (*headerFieldsByKey) headerFields() {}

func (fs *headerFieldsByKey) Next() bool {
	fs.cur++
	return fs.cur < len(fs.h.m[fs.k])
}

func (fs *headerFieldsByKey) index() int {
	if fs.cur < 0 {
		panic("message: headerfields.key or value called before next")
	}
	if fs.cur >= len(fs.h.m[fs.k]) {
		panic("message: headerfields.key or value called after next returned false")
	}
	return len(fs.h.m[fs.k]) - fs.cur - 1
}

func (fs *headerFieldsByKey) field() *headerField {
	return fs.h.m[fs.k][fs.index()]
}

func (fs *headerFieldsByKey) Key() string {
	return fs.field().k
}

func (fs *headerFieldsByKey) Value() string {
	return fs.field().v
}

func (fs *headerFieldsByKey) Raw() ([]byte, error) {
	return fs.field().raw()
}

func (fs *headerFieldsByKey) Del() {
	f := fs.field()

	ok := false
	for i := range fs.h.l {
		if f == fs.h.l[i] {
			ok = true
			fs.h.l = append(fs.h.l[:i], fs.h.l[i+1:]...)
			break
		}
	}
	if !ok {
		panic("message: field not found in Header.l")
	}

	fs.h.m[fs.k] = append(fs.h.m[fs.k][:fs.index()], fs.h.m[fs.k][fs.index()+1:]...)
	if len(fs.h.m[fs.k]) == 0 {
		delete(fs.h.m, fs.k)
	}
	fs.cur--
}

func (fs *headerFieldsByKey) Len() int {
	return len(fs.h.m[fs.k])
}

// FieldsByKey iterates over all fields having the specified key.
//
// The header may not be mutated while iterating, except using HeaderFields.Del.
func (h *Header) FieldsByKey(k string) HeaderFields {
	return &headerFieldsByKey{h, textproto.CanonicalMIMEHeaderKey(k), -1}
}

// ErrHeaderTooBig is returned when a header block exceeds the maximum
// permitted size. Callers can match it with errors.Is.
var ErrHeaderTooBig = errors.New("textproto: header exceeds maximum size")

// defaultMaxHeaderBytes bounds the size of a single header block (the top-level
// message header is bounded separately by the message package; this default is
// applied to MIME part headers, which are otherwise read from the raw stream
// with no limit). See MultipartReader.MaxHeaderBytes.
const defaultMaxHeaderBytes = 1 << 20 // 1 MB

// readLineSlice reads a logical line into line, charging budget for the bytes
// read, including the stripped line terminator (counted as CRLF; bare-LF input
// is over-charged by one byte, which is fine for a defensive cap). Once the
// budget would go negative, ErrHeaderTooBig is returned instead of buffering
// without bound.
func readLineSlice(r *bufio.Reader, line []byte, budget *int64) ([]byte, error) {
	for {
		l, more, err := r.ReadLine()
		n := int64(len(l))
		if !more {
			n += 2
		}
		*budget -= n
		if *budget < 0 {
			return line, ErrHeaderTooBig
		}
		line = append(line, l...)
		if err != nil {
			return line, err
		}

		if !more {
			break
		}
	}

	return line, nil
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t'
}

// isValidRawField reports whether kv is a single well-formed raw header field:
// it must end with a newline, the colon separating name and value must appear
// on the first physical line, and interior newlines are only permitted as
// folds (a newline immediately followed by SP or HT). This rejects raw fields
// that would smuggle additional header fields or a premature blank line (body
// injection) into the serialized output.
//
// A bare CR is accepted as an ordinary value byte: this parser does not treat
// it as a line ending, and ReadHeader itself preserves such bytes, so a field
// read from the wire can always be added back.
func isValidRawField(kv []byte) bool {
	if len(kv) == 0 || kv[len(kv)-1] != '\n' {
		return false
	}
	if colon := bytes.IndexByte(kv, ':'); colon == -1 || colon > bytes.IndexByte(kv, '\n') {
		return false
	}
	for i := 0; i < len(kv)-1; i++ {
		// A LF is allowed only as the final terminator (excluded above) or as
		// a fold, i.e. immediately followed by SP or HT (a continuation line).
		if kv[i] == '\n' && !isSpace(kv[i+1]) {
			return false
		}
	}
	return true
}

func validHeaderKeyByte(b byte) bool {
	c := int(b)
	return c >= 33 && c <= 126 && c != ':'
}

// trim returns s with leading and trailing spaces and tabs removed.
// It does not assume Unicode or UTF-8.
func trim(s []byte) []byte {
	i := 0
	for i < len(s) && isSpace(s[i]) {
		i++
	}
	n := len(s)
	for n > i && isSpace(s[n-1]) {
		n--
	}
	return s[i:n]
}

func hasContinuationLine(r *bufio.Reader) bool {
	c, err := r.ReadByte()
	if err != nil {
		return false // bufio will keep err until next read.
	}
	r.UnreadByte()
	return isSpace(c)
}

func readContinuedLineSlice(r *bufio.Reader, budget *int64) ([]byte, error) {
	// Read the first line. We preallocate slice that it enough
	// for most fields.
	line, err := readLineSlice(r, make([]byte, 0, 256), budget)
	if err == io.EOF && len(line) == 0 {
		// Header without a body
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	if len(line) == 0 { // blank line - no continuation
		return line, nil
	}

	line = append(line, '\r', '\n')

	// Read continuation lines.
	for hasContinuationLine(r) {
		line, err = readLineSlice(r, line, budget)
		if err == ErrHeaderTooBig {
			// Unlike real I/O errors, which bufio retains and returns again on
			// the next read, this error is synthesized here, so it must be
			// propagated immediately; deferring it would hand the caller a
			// silently truncated field.
			return nil, err
		} else if err != nil {
			break // bufio will keep err until next read.
		}

		line = append(line, '\r', '\n')
	}

	return line, nil
}

func writeContinued(b *strings.Builder, l []byte) {
	// Strip trailing \r, if any
	if len(l) > 0 && l[len(l)-1] == '\r' {
		l = l[:len(l)-1]
	}
	l = trim(l)
	if len(l) == 0 {
		return
	}
	if b.Len() > 0 {
		b.WriteByte(' ')
	}
	b.Write(l)
}

// Strip newlines and spaces around newlines.
func trimAroundNewlines(v []byte) string {
	var b strings.Builder
	b.Grow(len(v))
	for {
		i := bytes.IndexByte(v, '\n')
		if i < 0 {
			writeContinued(&b, v)
			break
		}
		writeContinued(&b, v[:i])
		v = v[i+1:]
	}

	return b.String()
}

// ReadHeader reads a MIME header from r. The header is a sequence of possibly
// continued "Key: Value" lines ending in a blank line.
//
// To avoid denial of service attacks, the provided bufio.Reader should be
// reading from an io.LimitedReader or a similar Reader to bound the size of
// headers.
func ReadHeader(r *bufio.Reader) (Header, error) {
	return readHeader(r, -1)
}

// readHeader reads a MIME header from r. If maxBytes >= 0, the header block is
// bounded to that many bytes and ErrHeaderTooBig is returned if it is exceeded.
// A negative maxBytes means no limit (the caller is expected to bound r
// itself, e.g. via an io.LimitedReader).
func readHeader(r *bufio.Reader, maxBytes int64) (Header, error) {
	fs := make([]*headerField, 0, 32)

	// A negative maxBytes means no limit; an effectively-infinite budget keeps
	// a single code path in the line-reading helpers.
	remaining := int64(math.MaxInt64)
	if maxBytes >= 0 {
		remaining = maxBytes
	}
	budget := &remaining

	// The first line cannot start with a leading space.
	if buf, err := r.Peek(1); err == nil && isSpace(buf[0]) {
		line, err := readLineSlice(r, nil, budget)
		if err != nil {
			return newHeader(fs), err
		}

		return newHeader(fs), fmt.Errorf("message: malformed MIME header initial line: %v", string(line))
	}

	for {
		kv, err := readContinuedLineSlice(r, budget)
		if len(kv) == 0 {
			return newHeader(fs), err
		}

		// Charge the slack capacity retained by the field's backing array (the
		// 256-byte preallocation dominates for tiny fields) so that a flood of
		// minimal fields cannot retain memory far beyond the budget.
		*budget -= int64(cap(kv) - len(kv))
		if *budget < 0 {
			return newHeader(fs), ErrHeaderTooBig
		}

		// Key ends at first colon; should not have trailing spaces but they
		// appear in the wild, violating specs, so we remove them if present.
		i := bytes.IndexByte(kv, ':')
		if i < 0 {
			return newHeader(fs), fmt.Errorf("message: malformed MIME header line: %v", string(kv))
		}

		keyBytes := trim(kv[:i])

		// Verify that there are no invalid characters in the header key.
		// See RFC 5322 Section 2.2
		for _, c := range keyBytes {
			if !validHeaderKeyByte(c) {
				return newHeader(fs), fmt.Errorf("message: malformed MIME header key: %v", string(keyBytes))
			}
		}

		key := textproto.CanonicalMIMEHeaderKey(string(keyBytes))

		// As per RFC 7230 a field-name is a token of one or more chars, so an
		// empty key (a leading colon) is malformed. We keep the field anyway,
		// preserving its raw bytes: dropping it would break the read/write
		// idempotency guarantee (required for DKIM) and hide the line from this
		// parser while a downstream parser might still see it.

		i++ // skip colon
		v := kv[i:]

		value := trimAroundNewlines(v)
		fs = append(fs, newHeaderField(key, value, kv))

		if err != nil {
			return newHeader(fs), err
		}
	}
}

func foldLine(v string, maxlen int) (line, next string, ok bool) {
	ok = true

	// Guard against a non-positive fold width, which would otherwise produce
	// negative slice indices below. This happens when the field name alone is
	// as long as (or longer than) the target line width. Callers retry with a
	// larger width and ultimately emit the remainder verbatim if no fold is
	// possible.
	if maxlen < 1 {
		maxlen = 1
	}

	// We'll need to fold before maxlen
	foldBefore := maxlen + 1
	foldAt := len(v)

	var folding string
	if foldBefore > len(v) {
		// We reached the end of the string
		if v[len(v)-1] != '\n' {
			// If there isn't already a trailing CRLF, insert one
			folding = "\r\n"
		}
	} else {
		// Find the closest whitespace before maxlen
		foldAt = strings.LastIndexAny(v[:foldBefore], " \t\n")

		if foldAt == 0 {
			// The whitespace we found was the previous folding WSP
			foldAt = foldBefore - 1
		} else if foldAt < 0 {
			// We didn't find any whitespace, we have to insert one
			foldAt = foldBefore - 2
		}

		switch v[foldAt] {
		case ' ', '\t':
			if v[foldAt-1] != '\n' {
				folding = "\r\n" // The next char will be a WSP, don't need to insert one
			}
		case '\n':
			folding = "" // There is already a CRLF, nothing to do
		default:
			// Another char, we need to insert CRLF + WSP. This will insert an
			// extra space in the string, so this should be avoided if
			// possible.
			folding = "\r\n "
			ok = false
		}
	}

	return v[:foldAt] + folding, v[foldAt:], ok
}

const (
	preferredHeaderLen = 76
	maxHeaderLen       = 998
)

// formatHeaderField formats a header field, ensuring each line is no longer
// than 76 characters. It tries to fold lines at whitespace characters if
// possible. If the header contains a word longer than this limit, it will be
// split.
func formatHeaderField(k, v string) string {
	s := k + ": "

	if v == "" {
		return s + "\r\n"
	}

	first := true
	for len(v) > 0 {
		// If this is the first line, substract the length of the key
		keylen := 0
		if first {
			keylen = len(s)
		}

		// First try with a soft limit. Skip it when the key already fills the
		// preferred width (a very long field name), which would leave no room
		// for the value on the first line.
		softTried := preferredHeaderLen-keylen >= 1
		var l, next string
		ok := false
		if softTried {
			l, next, ok = foldLine(v, preferredHeaderLen-keylen)
		}
		if !ok {
			// Folding failed to preserve the original header field value. Try
			// with a larger, hard limit.
			var hardOK bool
			l, next, hardOK = foldLine(v, maxHeaderLen-keylen)
			if !hardOK && !softTried {
				// The field name alone crowds the hard line limit, so folding
				// would split the value at an arbitrary point, inserting
				// whitespace that changes it. Emit the remainder verbatim
				// (overflowing the line-length limit) rather than corrupting
				// the value. For ordinary field names the whitespace insertion
				// is kept instead: there the 998-octet limit takes precedence
				// (RFC 5322 section 2.1.1).
				s += v
				if !strings.HasSuffix(v, "\n") {
					s += "\r\n"
				}
				break
			}
		}
		if next == v {
			// No progress is possible: the value cannot be folded onto a
			// shorter line. Emit the remainder verbatim (overflowing the
			// line-length limit) rather than looping forever.
			s += v
			if !strings.HasSuffix(v, "\n") {
				s += "\r\n"
			}
			break
		}
		v = next
		s += l
		first = false
	}

	return s
}

// WriteHeader writes a MIME header to w.
func WriteHeader(w io.Writer, h Header) error {
	for i := len(h.l) - 1; i >= 0; i-- {
		f := h.l[i]
		if rawField, err := f.raw(); err == nil {
			if _, err := w.Write(rawField); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("failed to write header field #%v (%q): %w", len(h.l)-i, f.k, err)
		}
	}

	_, err := w.Write([]byte{'\r', '\n'})
	return err
}
