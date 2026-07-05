package textproto

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

var from = "Mitsuha Miyamizu <mitsuha.miyamizu@example.com>"
var to = "Taki Tachibana <taki.tachibana@example.org>"
var received2 = "from example.com by example.org"

func newTestHeader() Header {
	var h Header
	h.Add("From", from)
	h.Add("To", to)
	h.Add("Received", "from localhost by example.com")
	h.Add("Received", received2)
	return h
}

func collectHeaderFields(fields HeaderFields) []string {
	var l []string
	for fields.Next() {
		l = append(l, fields.Key()+": "+fields.Value())
	}
	return l
}

func TestHeader(t *testing.T) {
	h := newTestHeader()

	if got := h.Get("From"); got != from {
		t.Errorf("Get(\"From\") = %#v, want %#v", got, from)
	}
	if got := h.Get("Received"); got != received2 {
		t.Errorf("Get(\"Received\") = %#v, want %#v", got, received2)
	}
	if got := h.Get("X-I-Dont-Exist"); got != "" {
		t.Errorf("Get(non-existing) = %#v, want \"\"", got)
	}

	if !h.Has("From") {
		t.Errorf("Has(\"From\") = false, want true")
	}
	if h.Has("X-I-Dont-Exist") {
		t.Errorf("Has(non-existing) = true, want false")
	}

	if got := h.FieldsByKey("Received").Len(); got != 2 {
		t.Errorf("FieldsByKey(\"Received\").Len() = %v, want %v", got, 2)
	}
	if got := h.FieldsByKey("X-I-Dont-Exist").Len(); got != 0 {
		t.Errorf("FieldsByKey(non-existing).Len() = %v, want %v", got, 0)
	}

	l := collectHeaderFields(h.Fields())
	want := []string{
		"Received: from example.com by example.org",
		"Received: from localhost by example.com",
		"To: Taki Tachibana <taki.tachibana@example.org>",
		"From: Mitsuha Miyamizu <mitsuha.miyamizu@example.com>",
	}
	if !reflect.DeepEqual(l, want) {
		t.Errorf("Fields() reported incorrect values: got \n%#v\n but want \n%#v", l, want)
	}

	l = collectHeaderFields(h.FieldsByKey("Received"))
	want = []string{
		"Received: from example.com by example.org",
		"Received: from localhost by example.com",
	}
	if !reflect.DeepEqual(l, want) {
		t.Errorf("FieldsByKey(\"Received\") reported incorrect values: got \n%#v\n but want \n%#v", l, want)
	}

	if h.FieldsByKey("X-I-Dont-Exist").Next() {
		t.Errorf("FieldsByKey(non-existing).Next() returned true, want false")
	}

	want = []string{
		"from example.com by example.org",
		"from localhost by example.com",
	}
	if l := h.Values("Received"); !reflect.DeepEqual(l, want) {
		t.Errorf("Values(\"Received\") reported incorrect values: got \n%#v\n but want \n%#v", l, want)
	}
}

func TestHeader_Set(t *testing.T) {
	h := newTestHeader()

	h.Set("From", to)
	if got := h.Get("From"); got != to {
		t.Errorf("Get(\"From\") = %#v after Set(), want %#v", got, to)
	}
	l := collectHeaderFields(h.FieldsByKey("From"))
	want := []string{"From: Taki Tachibana <taki.tachibana@example.org>"}
	if !reflect.DeepEqual(l, want) {
		t.Errorf("FieldsByKey(\"From\") reported incorrect values after Set(): got \n%#v\n but want \n%#v", l, want)
	}
}

func TestHeader_Del(t *testing.T) {
	h := newTestHeader()

	h.Del("Received")
	if h.Has("Received") {
		t.Errorf("Has(\"Received\") = true after Del(), want false")
	}
	l := collectHeaderFields(h.FieldsByKey("Received"))
	var want []string = nil
	if !reflect.DeepEqual(l, want) {
		t.Errorf("FieldsByKey(\"Received\") reported incorrect values after Del(): got \n%#v\n but want \n%#v", l, want)
	}
}

func TestHeader_Fields_Del_multiple(t *testing.T) {
	h := newTestHeader()

	ok := false
	fields := h.Fields()
	for fields.Next() {
		if fields.Key() == "Received" {
			fields.Del()
			ok = true
			break
		}
	}
	if !ok {
		t.Fatal("Fields() didn't yield \"Received\"")
	}

	l := collectHeaderFields(h.FieldsByKey("Received"))
	want := []string{"Received: from localhost by example.com"}
	if !reflect.DeepEqual(l, want) {
		t.Errorf("FieldsByKey(\"Received\") reported incorrect values after HeaderFields.Del(): got \n%#v\n but want \n%#v", l, want)
	}
}

func TestHeader_Fields_Del_single(t *testing.T) {
	h := newTestHeader()

	ok := false
	fields := h.Fields()
	for fields.Next() {
		if fields.Key() == "To" {
			fields.Del()
			ok = true
			break
		}
	}
	if !ok {
		t.Fatal("Fields() didn't yield \"To\"")
	}

	if h.FieldsByKey("To").Next() {
		t.Errorf("FieldsByKey(\"To\") returned a non-empty set")
	}
}

func TestHeader_Fields_Del_all(t *testing.T) {
	h := newTestHeader()

	fields := h.Fields()
	for fields.Next() {
		fields.Del()
	}

	if h.Fields().Next() {
		t.Errorf("Fields() returned a non-empty set")
	}
}

func TestHeader_FieldsByKey_Del(t *testing.T) {
	h := newTestHeader()

	fields := h.FieldsByKey("Received")
	if !fields.Next() {
		t.Fatal("FieldsByKey(\"Received\").Next() = false, want true")
	}
	fields.Del()

	l := collectHeaderFields(h.FieldsByKey("Received"))
	want := []string{"Received: from localhost by example.com"}
	if !reflect.DeepEqual(l, want) {
		t.Errorf("FieldsByKey(\"Received\") reported incorrect values after HeaderFields.Del(): got \n%#v\n but want \n%#v", l, want)
	}
}

const testHeader = "Received: from example.com by example.org\r\n" +
	"Received: from localhost by example.com\r\n" +
	"To: Taki Tachibana <taki.tachibana@example.org>\r\n" +
	"From: Mitsuha Miyamizu <mitsuha.miyamizu@example.com>\r\n\r\n"

func TestReadHeader(t *testing.T) {
	r := bufio.NewReader(strings.NewReader(testHeader))
	h, err := ReadHeader(r)
	if err != nil {
		t.Fatalf("readHeader() returned error: %v", err)
	}

	l := collectHeaderFields(h.Fields())
	want := []string{
		"Received: from example.com by example.org",
		"Received: from localhost by example.com",
		"To: Taki Tachibana <taki.tachibana@example.org>",
		"From: Mitsuha Miyamizu <mitsuha.miyamizu@example.com>",
	}
	if !reflect.DeepEqual(l, want) {
		t.Errorf("Fields() reported incorrect values: got \n%#v\n but want \n%#v", l, want)
	}

	b := make([]byte, 1)
	if _, err := r.Read(b); err != io.EOF {
		t.Errorf("Read() didn't return EOF: %v", err)
	}
}

const testInvalidHeader = "not valid: example\r\n"

func TestInvalidHeader(t *testing.T) {
	r := bufio.NewReader(strings.NewReader(testInvalidHeader))
	_, err := ReadHeader(r)
	if err == nil {
		t.Errorf("No error thrown")
	}
}

const testHeaderWithoutBody = "Received: from example.com by example.org\r\n" +
	"Received: from localhost by example.com\r\n" +
	"To: Taki Tachibana <taki.tachibana@example.org>\r\n" +
	"From: Mitsuha Miyamizu <mitsuha.miyamizu@example.com>\r\n"

func TestReadHeaderWithoutBody(t *testing.T) {
	r := bufio.NewReader(strings.NewReader(testHeaderWithoutBody))
	h, err := ReadHeader(r)
	if err != nil {
		t.Fatalf("readHeader() returned error: %v", err)
	}

	l := collectHeaderFields(h.Fields())
	want := []string{
		"Received: from example.com by example.org",
		"Received: from localhost by example.com",
		"To: Taki Tachibana <taki.tachibana@example.org>",
		"From: Mitsuha Miyamizu <mitsuha.miyamizu@example.com>",
	}
	if !reflect.DeepEqual(l, want) {
		t.Errorf("Fields() reported incorrect values: got \n%#v\n but want \n%#v", l, want)
	}

	b := make([]byte, 1)
	if _, err := r.Read(b); err != io.EOF {
		t.Errorf("Read() didn't return EOF: %v", err)
	}
}

const testLFHeader = `From: contact@example.org
To: contact@example.org
Subject: A little message, just for you
Date: Wed, 11 May 2016 14:31:59 +0000
Message-ID: <0000000@localhost/>
Content-Type: text/plain

`

func TestReadHeader_lf(t *testing.T) {
	r := bufio.NewReader(strings.NewReader(testLFHeader))
	h, err := ReadHeader(r)
	if err != nil {
		t.Fatalf("readHeader() returned error: %v", err)
	}

	l := collectHeaderFields(h.Fields())
	want := []string{
		"From: contact@example.org",
		"To: contact@example.org",
		"Subject: A little message, just for you",
		"Date: Wed, 11 May 2016 14:31:59 +0000",
		"Message-Id: <0000000@localhost/>",
		"Content-Type: text/plain",
	}
	if !reflect.DeepEqual(l, want) {
		t.Errorf("Fields() reported incorrect values: got \n%#v\n but want \n%#v", l, want)
	}

	b := make([]byte, 1)
	if _, err := r.Read(b); err != io.EOF {
		t.Errorf("Read() didn't return EOF: %v", err)
	}
}

// A line whose key is empty (a leading colon) must not be silently dropped:
// dropping it breaks the documented read/write idempotency (required for DKIM)
// and creates a parser differential with downstream agents.
func TestReadHeader_emptyKeyPreserved(t *testing.T) {
	const in = "From: a@example.com\r\n" +
		": dropped value\r\n" +
		"To: b@example.com\r\n\r\n"

	h, err := ReadHeader(bufio.NewReader(strings.NewReader(in)))
	if err != nil {
		t.Fatalf("ReadHeader() returned error: %v", err)
	}

	var b bytes.Buffer
	if err := WriteHeader(&b, h); err != nil {
		t.Fatalf("WriteHeader() returned error: %v", err)
	}
	if b.String() != in {
		t.Errorf("round-trip mismatch:\n got %q\nwant %q", b.String(), in)
	}
}

func TestHeader_AddRaw(t *testing.T) {
	dkimLine := `DKIM-Signature: a=rsa-sha256; bh=uI/rVH7mLBSWkJVvQYKz3TbpdI2BLZWTIMKcuo0KHO
 I=; c=simple/simple; d=example.org; h=Subject:To:From; s=default; t=1577562184; v=1; b=;` + "\r\n"
	valUnfolded := `a=rsa-sha256; bh=uI/rVH7mLBSWkJVvQYKz3TbpdI2BLZWTIMKcuo0KHO I=; c=simple/simple; d=example.org; h=Subject:To:From; s=default; t=1577562184; v=1; b=;`

	h := newTestHeader()
	h.AddRaw([]byte(dkimLine))

	// 1. It should be possible to get value using any key case.
	// 2. It should be un-folded.
	if v := h.Get("Dkim-Signature"); v != valUnfolded {
		t.Errorf("Get returned wrong value: got \n%v\n but want \n%v", v, valUnfolded)
	}

	var b bytes.Buffer
	if err := WriteHeader(&b, h); err != nil {
		t.Fatalf("WriteHeader() returned error: %v", err)
	}

	// 1. Header field name is not changed (to Dkim-Signature).
	// 2. No folding is done.
	wantHdr := dkimLine + testHeader

	if b.String() != wantHdr {
		t.Errorf("WriteHeader() wrote invalid data: got \n%v\n but want \n%v", b.String(), wantHdr)
	}
}

// A field name long enough that "key: " reaches or exceeds the preferred fold
// width previously produced a negative fold width and panicked in foldLine.
// Long field names are legal (RFC 5322 §2.2), so WriteHeader must not panic.
//
// The value contains no whitespace so that no legitimate fold point exists:
// key lengths that leave the value less room than it needs (in particular the
// 977-994 range, where the hard-limit width is small but positive) must not
// fold mid-value, which would inject whitespace and corrupt it.
func TestWriteHeader_longFieldName(t *testing.T) {
	const value = "value-without-spaces"
	for _, n := range []int{73, 74, 76, 200, 975, 980, 992, 1000} {
		k := "X-" + strings.Repeat("a", n) // stays canonical: "X-aaa..."
		var h Header
		h.Add(k, value)

		var b bytes.Buffer
		if err := WriteHeader(&b, h); err != nil {
			t.Errorf("keylen=%d: WriteHeader() returned error: %v", n, err)
			continue
		}

		// The written header must be valid and re-readable, and the value must
		// survive the round-trip unchanged.
		h2, err := ReadHeader(bufio.NewReader(strings.NewReader(b.String())))
		if err != nil {
			t.Errorf("keylen=%d: re-reading written header failed: %v (%q)", n, err, b.String())
			continue
		}
		if got := h2.Get(k); got != value {
			t.Errorf("keylen=%d: round-tripped value = %q, want %q", n, got, value)
		}
	}
}

// AddRaw must reject fields that would smuggle extra header fields, a premature
// blank line (body injection), or that are otherwise not a single well-formed
// field. Consistent with the existing missing-colon behaviour, it panics.
func TestHeader_AddRaw_rejectsInjection(t *testing.T) {
	bad := [][]byte{
		[]byte("X-Test: evil\r\nInjected: 1\r\n"),    // smuggled extra field
		[]byte("X-Test: v\r\n\r\nSMUGGLED BODY\r\n"), // premature blank line
		[]byte("X-Test: value"),                      // missing trailing newline
		[]byte("X-Test: a\nb\r\n"),                   // bare LF not a fold
		[]byte("X-Name\r\n : v\r\n"),                 // fold before the colon
	}
	for _, kv := range bad {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("AddRaw(%q) did not panic", kv)
				}
			}()
			var h Header
			h.AddRaw(kv)
		}()
	}
}

// AddRaw must still accept well-formed fields, including folded ones with
// either CRLF or bare-LF line endings, and fields containing a bare CR in the
// value: ReadHeader accepts and preserves such bytes, so AddRaw must too.
func TestHeader_AddRaw_acceptsValid(t *testing.T) {
	good := [][]byte{
		[]byte("X-Test: value\r\n"),
		[]byte("X-Test: value\n"),
		[]byte("Subject: a\r\n b\r\n"),
		[]byte("Subject: a\n b\n"),
		[]byte("X-Test: a\rb\r\n"), // bare CR is an ordinary value byte
	}
	for _, kv := range good {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("AddRaw(%q) panicked: %v", kv, r)
				}
			}()
			var h Header
			h.AddRaw(kv)
		}()
	}
}

// Copying a header field-by-field via Fields().Raw() and AddRaw must work for
// anything ReadHeader accepts — in particular values containing a bare CR.
// Previously AddRaw panicked on such fields, turning parsed network input into
// a process crash.
func TestHeader_AddRaw_roundTripFromReadHeader(t *testing.T) {
	const in = "Subject: a\rb\r\nOther: x\r\n\r\n"

	h, err := ReadHeader(bufio.NewReader(strings.NewReader(in)))
	if err != nil {
		t.Fatalf("ReadHeader() returned error: %v", err)
	}

	var h2 Header
	fields := h.Fields()
	for fields.Next() {
		raw, err := fields.Raw()
		if err != nil {
			t.Fatalf("Raw() returned error: %v", err)
		}
		h2.AddRaw(raw)
	}

	var b bytes.Buffer
	if err := WriteHeader(&b, h2); err != nil {
		t.Fatalf("WriteHeader() returned error: %v", err)
	}
	// AddRaw inserts at the top (the only insert the Header model allows), so
	// copying in iteration order reverses the fields; the raw bytes of each
	// field must be preserved verbatim.
	const want = "Other: x\r\nSubject: a\rb\r\n\r\n"
	if b.String() != want {
		t.Errorf("round-trip mismatch:\n got %q\nwant %q", b.String(), want)
	}
}

// A header block must not retain memory far beyond the byte budget even when
// it is made of minimal fields, whose per-field overhead (preallocated
// buffers) dominates the wire bytes.
func TestReadHeader_budgetCountsFieldOverhead(t *testing.T) {
	// 1000 minimal fields are just 5 kB on the wire but retain >250 kB of
	// buffers; with a 100 kB budget the read must fail.
	in := strings.Repeat("a:\r\n", 1000) + "\r\n"
	_, err := readHeader(bufio.NewReader(strings.NewReader(in)), 100*1024)
	if !errors.Is(err, ErrHeaderTooBig) {
		t.Errorf("readHeader() = %v, want ErrHeaderTooBig", err)
	}
}

// The header must not be silently truncated when the byte budget runs out in
// the middle of a folded field's continuation lines: the error must surface
// on that read, not be deferred.
func TestReadHeader_budgetExceededMidContinuation(t *testing.T) {
	in := "X-Long: start\r\n folded-one\r\n folded-two\r\n\r\n"
	_, err := readHeader(bufio.NewReader(strings.NewReader(in)), 20)
	if !errors.Is(err, ErrHeaderTooBig) {
		t.Errorf("readHeader() = %v, want ErrHeaderTooBig", err)
	}
}

// The library preserves empty-key fields read from the wire (see
// TestReadHeader_emptyKeyPreserved), but must refuse to freshly generate an
// illegal ": value" line for a synthesized empty-key field.
func TestWriteHeader_rejectsSynthesizedEmptyKey(t *testing.T) {
	var h Header
	h.Add("", "injected")

	var b bytes.Buffer
	if err := WriteHeader(&b, h); err == nil {
		t.Errorf("WriteHeader() = nil, want error (wrote %q)", b.String())
	}
}

func TestWriteHeader(t *testing.T) {
	h := newTestHeader()

	var b bytes.Buffer
	if err := WriteHeader(&b, h); err != nil {
		t.Fatalf("writeHeader() returned error: %v", err)
	}

	if b.String() != testHeader {
		t.Errorf("writeHeader() wrote invalid data: got \n%v\n but want \n%v", b.String(), testHeader)
	}
}

// RFC says key shouldn't have trailing spaces, but those appear in the wild, so
// we need to handle them.
const testHeaderWithWhitespace = "Subject \t : \t Hey \r\n" +
	" \t there\r\n" +
	"From: Mitsuha Miyamizu <mitsuha.miyamizu@example.com>\r\n\r\n"

func TestHeaderWithWhitespace(t *testing.T) {
	h, err := ReadHeader(bufio.NewReader(strings.NewReader(testHeaderWithWhitespace)))
	if err != nil {
		t.Fatalf("readHeader() returned error: %v", err)
	}

	l := collectHeaderFields(h.Fields())
	want := []string{
		"Subject: Hey there",
		"From: Mitsuha Miyamizu <mitsuha.miyamizu@example.com>",
	}
	if !reflect.DeepEqual(l, want) {
		t.Errorf("Fields() reported incorrect values: got \n%#v\n but want \n%#v", l, want)
	}

	var b bytes.Buffer
	if err := WriteHeader(&b, h); err != nil {
		t.Fatalf("writeHeader() returned error: %v", err)
	}

	if b.String() != testHeaderWithWhitespace {
		t.Errorf("writeHeader() wrote invalid data: got \n%v\n but want \n%v", b.String(), testHeaderWithWhitespace)
	}
}

var formatHeaderFieldTests = []struct {
	k, v      string
	formatted string
}{
	{
		k:         "From",
		v:         "Mitsuha Miyamizu <mitsuha.miyamizu@example.org>",
		formatted: "From: Mitsuha Miyamizu <mitsuha.miyamizu@example.org>\r\n",
	},
	{
		k:         "Subject",
		v:         "This is a very long subject, much longer than just the 76 characters limit that applies to message header fields",
		formatted: "Subject: This is a very long subject, much longer than just the 76\r\n characters limit that applies to message header fields\r\n",
	},
	{
		k:         "Subject",
		v:         "This is        yet          \t  another    subject          \t                   with many         whitespace      characters",
		formatted: "Subject: This is        yet          \t  another    subject          \t       \r\n            with many         whitespace      characters\r\n",
	},
	{
		k:         "In-Reply-To",
		v:         "<CAOzTU0ikmAnr1ebhLEfks2crdHcotR-cXeJ-7ySd4X4VJ-B2fg@mail.gmail.com>",
		formatted: "In-Reply-To: <CAOzTU0ikmAnr1ebhLEfks2crdHcotR-cXeJ-7ySd4X4VJ-B2fg@mail.gmail.com>\r\n",
	},
	{
		k:         "Subject",
		v:         "=?utf-8?q?=E2=80=9CDeveloper_reads_customer_requested_change.=E2=80=9D=0A?= =?utf-8?q?=0ACaravaggio=0A=0AOil_on...?=",
		formatted: "Subject: =?utf-8?q?=E2=80=9CDeveloper_reads_customer_requested_change.=E2=80=9D=0A?= =?utf-8?q?=0ACaravaggio=0A=0AOil_on...?=\r\n",
	},
	// Spaces should not appear in RFC2047-encoded string but it should be fine in case they do
	{
		k:         "Subject",
		v:         "=?utf-8?q?=E2=80=9CShort subject=E2=80=9D=0A?= =?utf-8?q?=0AAuthor=0A=0AOil_on...?=",
		formatted: "Subject: =?utf-8?q?=E2=80=9CShort subject=E2=80=9D=0A?=\r\n =?utf-8?q?=0AAuthor=0A=0AOil_on...?=\r\n",
	},
	{
		k:         "Subject",
		v:         "=?utf-8?q?=E2=80=9CVery long subject very long subject very long subject very long subject=E2=80=9D=0A?= =?utf-8?q?=0ALong second part of subject long second part of subject long second part of subject long subject=0A=0AOil_on...?=",
		formatted: "Subject: =?utf-8?q?=E2=80=9CVery long subject very long subject very long\r\n subject very long subject=E2=80=9D=0A?= =?utf-8?q?=0ALong second part of\r\n subject long second part of subject long second part of subject long\r\n subject=0A=0AOil_on...?=\r\n",
	},
	{
		k:         "Subject",
		v:         "InCaseOfVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryLongStringWeStillShouldComplyToTheHardLimitOf998Symbols",
		formatted: "Subject: InCaseOfVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryVeryLongStringWeStillSho\r\n uldComplyToTheHardLimitOf998Symbols\r\n",
	},
	{
		k:         "Bcc",
		v:         "",
		formatted: "Bcc: \r\n",
	},
	{
		k:         "Bcc",
		v:         " ",
		formatted: "Bcc:  \r\n",
	},
}

func TestWriteHeader_continued(t *testing.T) {
	for _, test := range formatHeaderFieldTests {
		var h Header
		h.Add(test.k, test.v)

		var b bytes.Buffer
		if err := WriteHeader(&b, h); err != nil {
			t.Fatalf("writeHeader() returned error: %v", err)
		}
		if b.String() != test.formatted+"\r\n" {
			t.Errorf("Expected formatted header to be \n%v\n but got \n%v", test.formatted+"\r\n", b.String())
		}
	}
}

var incorrectFormatHeaderFieldTests = []struct {
	k, v string
}{
	{
		k: "DKIM Signature",
		v: "v=1; h=From; d=example.org; b=AuUoFEfDxTDkHlLXSZEpZj79LICEps6eda7W3deTVFOk4yAUoqOB4nujc7YopdG5dWLSdNg6x NAZpOPr+kHxt1IrE+NahM6L/LbvaHutKVdkLLkpVaVVQPzeRDI009SO2Il5Lu7rDNH6mZckBdrI x0orEtZV4bmp/YzhwvcubU4=\r\n",
	},
	{
		// Unicode, Cyrillic
		k: "\u0417\u0430\u0433\u043e\u043b\u043e\u0432\u043e\u043a",
		v: "Value",
	},
	{
		k: "Header:",
		v: "Value",
	},
	{
		k: "DKIM-Signature",
		v: "v=1;\r\n h=From:To:Reply-To:Subject:Message-ID:References:In-Reply-To:MIME-Version;\r\n d=example.org\r\n",
	},
	{
		k: "DKIM-Signature",
		v: "v=1;\n h=From:To:Reply-To:Subject:Message-ID:References:In-Reply-To:MIME-Version; d=example.org",
	},
	{
		k: "DKIM-Signature",
		v: "v=1;\r h=From:To:Reply-To:Subject:Message-ID:References:In-Reply-To:MIME-Version; d=example.org",
	},
}

func TestWriteHeader_failed(t *testing.T) {
	for _, test := range incorrectFormatHeaderFieldTests {
		var h Header
		h.Add(test.k, test.v)

		var b bytes.Buffer
		if err := WriteHeader(&b, h); err == nil {
			t.Errorf("Expected header \n%v: %v\n to be incorrect, but it was accepted", test.k, test.v)
		}
	}
}

var incorrectFormatMultipleHeaderFieldTests = []struct {
	k1, k2, v1, v2 string
}{
	{
		// Incorrect first
		k1: "DKIM Signature",
		v1: "v=1; h=From; d=example.org; b=AuUoFEfDxTDkHlLXSZEpZj79LICEps6eda7W3deTVFOk4yAUoqOB4nujc7YopdG5dWLSdNg6x NAZpOPr+kHxt1IrE+NahM6L/LbvaHutKVdkLLkpVaVVQPzeRDI009SO2Il5Lu7rDNH6mZckBdrI x0orEtZV4bmp/YzhwvcubU4=\r\n",
		k2: "From",
		v2: "alice@example.com",
	},
	{
		// Incorrect both
		k1: "\u0417\u0430\u0433\u043e\u043b\u043e\u0432\u043e\u043a",
		v1: "Value",
		k2: "Header:",
		v2: "Value",
	},
	{
		// Incorrect second
		k1: "DKIM-Signature",
		v1: "v=1; h=From:To:Reply-To:Subject:Message-ID:References:In-Reply-To:MIME-Version; d=example.org",
		k2: "DKIM-Signature",
		v2: "v=1;\r\n h=From:To:Reply-To:Subject:Message-ID:References:In-Reply-To:MIME-Version;\r\n d=example.org\r\n",
	},
}

func TestWriteHeader_failed_multiple(t *testing.T) {
	for _, test := range incorrectFormatMultipleHeaderFieldTests {
		var h Header
		h.Add(test.k1, test.v1)
		h.Add(test.k2, test.v2)

		var b bytes.Buffer
		if err := WriteHeader(&b, h); err == nil {
			t.Errorf("Expected headers \n%v: %v\n%v: %v\n to be incorrect, but it was accepted", test.k1, test.v2, test.k2, test.v2)
		}
	}
}

func TestHeaderFromMap(t *testing.T) {
	m := map[string][]string{
		"Received": []string{"from example.com by example.org", "from localhost by example.com"},
		"To":       []string{"Taki Tachibana <taki.tachibana@example.org>"},
		"From":     []string{"Mitsuha Miyamizu <mitsuha.miyamizu@example.com>"},
	}

	h := HeaderFromMap(m)

	l := collectHeaderFields(h.Fields())
	want := []string{
		"From: Mitsuha Miyamizu <mitsuha.miyamizu@example.com>",
		"Received: from example.com by example.org",
		"Received: from localhost by example.com",
		"To: Taki Tachibana <taki.tachibana@example.org>",
	}
	if !reflect.DeepEqual(l, want) {
		t.Errorf("Fields() reported incorrect values: got \n%#v\n but want \n%#v", l, want)
	}
}

func TestHeader_Map(t *testing.T) {
	want := map[string][]string{
		"Received": []string{"from example.com by example.org", "from localhost by example.com"},
		"To":       []string{"Taki Tachibana <taki.tachibana@example.org>"},
		"From":     []string{"Mitsuha Miyamizu <mitsuha.miyamizu@example.com>"},
	}

	h := newTestHeader()
	m := h.Map()
	if !reflect.DeepEqual(m, want) {
		t.Errorf("Fields(): got \n%#v\n but want \n%#v", m, want)
	}
}
