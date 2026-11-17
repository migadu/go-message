package message

import (
	"bytes"
	"io"
	"io/ioutil"
	"reflect"
	"strings"
	"testing"

	"github.com/emersion/go-message/textproto"
	"golang.org/x/text/encoding/charmap"
)

const digestMessage = "MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/digest; boundary=d\r\n\r\n" +
	"--d\r\nContent-Transfer-Encoding: 8bit\r\n\r\n" +
	"From: sender@example.org\r\nSubject: embedded\r\n" +
	"Content-Type: text/html\r\nContent-Transfer-Encoding: base64\r\n\r\n" +
	"PHA+aGVsbG88L3A+\r\n--d--\r\n"

func TestMultipartDigestDefault(t *testing.T) {
	e, err := Read(strings.NewReader(digestMessage))
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.MultipartReader().NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if p.mediaType != "message/rfc822" {
		t.Errorf("effective media type = %q, want message/rfc822", p.mediaType)
	}
	copied := p.Header.Copy()
	if p.Header.Has("Content-Type") || copied.Has("Content-Type") {
		t.Error("default Content-Type was inserted into the original header or its copy")
	}
	if typ, _, err := p.Header.ContentType(); err != nil || typ != "text/plain" {
		t.Errorf("context-free header default = %q, %v; want text/plain, nil", typ, err)
	}
	var header bytes.Buffer
	if err := textproto.WriteHeader(&header, p.Header.Copy().Header); err != nil {
		t.Fatal(err)
	}
	if want := "Content-Transfer-Encoding: 8bit\r\n\r\n"; header.String() != want {
		t.Errorf("written child header = %q, want %q", header.String(), want)
	}
	body, err := ioutil.ReadAll(p.Body)
	if err != nil {
		t.Fatal(err)
	}
	if want := "From: sender@example.org\r\nSubject: embedded\r\n" +
		"Content-Type: text/html\r\nContent-Transfer-Encoding: base64\r\n\r\nPHA+aGVsbG88L3A+"; string(body) != want {
		t.Errorf("encapsulated message = %q, want %q", body, want)
	}

	// Reading and writing the complete digest must retain the implicit child
	// type and the original bytes, without synthesizing a Content-Type field.
	e, err = Read(strings.NewReader(digestMessage))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := e.WriteTo(&out); err != nil {
		t.Fatal(err)
	}
	if out.String() != digestMessage {
		t.Errorf("digest roundtrip changed bytes:\n%s", out.String())
	}
}

func TestMultipartDigestDefaultScope(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		want      []string
	}{
		{"top-level", "\r\nplain", []string{"text/plain"}},
		{"explicit child", "Content-Type: multipart/digest; boundary=d\r\n\r\n--d\r\nContent-Type: text/html\r\n\r\nhtml\r\n--d--\r\n", []string{"multipart/digest", "text/html"}},
		{"explicit empty child", "Content-Type: multipart/digest; boundary=d\r\n\r\n--d\r\nContent-Type: \r\n\r\nplain\r\n--d--\r\n", []string{"multipart/digest", "text/plain"}},
		{"nested mixed resets default", "Content-Type: multipart/digest; boundary=d\r\n\r\n--d\r\nContent-Type: multipart/mixed; boundary=m\r\n\r\n--m\r\n\r\nplain\r\n--m--\r\n--d\r\n\r\nFrom: sender@example.org\r\n\r\nembedded\r\n--d--\r\n", []string{"multipart/digest", "multipart/mixed", "text/plain", "message/rfc822"}},
		{"following mixed sibling resets default", "Content-Type: multipart/mixed; boundary=m\r\n\r\n--m\r\nContent-Type: multipart/digest; boundary=d\r\n\r\n--d\r\n\r\nFrom: sender@example.org\r\n\r\nembedded\r\n--d--\r\n--m\r\n\r\nplain\r\n--m--\r\n", []string{"multipart/mixed", "multipart/digest", "message/rfc822", "text/plain"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, err := Read(strings.NewReader(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			if err := e.Walk(func(_ []int, part *Entity, err error) error {
				got = append(got, part.mediaType)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("effective media types = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMultipartDigestExplicitCharset(t *testing.T) {
	old := CharsetReader
	defer func() { CharsetReader = old }()
	CharsetReader = func(_ string, r io.Reader) (io.Reader, error) {
		return charmap.ISO8859_1.NewDecoder().Reader(r), nil
	}
	e, err := Read(strings.NewReader("Content-Type: multipart/digest; boundary=d\r\n\r\n--d\r\nContent-Type: text/plain; charset=iso-8859-1\r\nContent-Transfer-Encoding: base64\r\n\r\nY2Fm6Q==\r\n--d--\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.MultipartReader().NextPart()
	if err != nil {
		t.Fatal(err)
	}
	body, err := ioutil.ReadAll(p.Body)
	if err != nil || string(body) != "café" {
		t.Fatalf("explicit text body = %q, %v; want café, nil", body, err)
	}
}

func TestEntityContentTypeSnapshot(t *testing.T) {
	// Assert the new API at runtime so the test-first commit also exercises
	// the old parser's behavior rather than failing to compile.
	type contentTyper interface {
		ContentType() (string, map[string]string, error)
	}
	e, err := Read(strings.NewReader(digestMessage))
	if err != nil {
		t.Fatal(err)
	}
	typed, ok := interface{}(e).(contentTyper)
	if !ok {
		t.Fatal("Entity has no ContentType method for the effective MIME type")
	}
	typ, params, err := typed.ContentType()
	if err != nil || typ != "multipart/digest" || params["boundary"] != "d" {
		t.Fatalf("effective type = %q, %v, %v", typ, params, err)
	}
	params["boundary"] = "wrong"
	e.Header.Set("Content-Type", "text/html")
	typ, params, err = typed.ContentType()
	if err != nil || typ != "multipart/digest" || params["boundary"] != "d" {
		t.Fatalf("creation-time effective type was mutated: %q, %v, %v", typ, params, err)
	}
	p, err := e.MultipartReader().NextPart()
	if err != nil {
		t.Fatal(err)
	}
	typ, _, err = interface{}(p).(contentTyper).ContentType()
	if err != nil || typ != "message/rfc822" {
		t.Fatalf("effective child type = %q, %v; want message/rfc822, nil", typ, err)
	}
}
