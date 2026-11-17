package mail_test

import (
	"errors"
	"io"
	"io/ioutil"
	"strings"
	"testing"

	"github.com/emersion/go-message/mail"
)

func TestReader_truncatedMultipartError(t *testing.T) {
	const raw = "Content-Type: multipart/mixed; boundary=b\r\n\r\n" +
		"--b\r\nContent-Type: text/plain\r\n\r\nretained text"
	mr, err := mail.CreateReader(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()
	p, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if body, err := ioutil.ReadAll(p.Body); string(body) != "retained text" || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("body = %q, %v; want retained text and unexpected EOF", body, err)
	}
	if p, err := mr.NextPart(); p != nil || !errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		t.Fatalf("NextPart() = %v, %v; want nil and wrapped unexpected EOF", p, err)
	}
}

func TestReader_truncatedPartHeaderError(t *testing.T) {
	const raw = "Content-Type: multipart/mixed; boundary=b\r\n\r\n" +
		"--b\r\nContent-Type: text/plain"
	mr, err := mail.CreateReader(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()
	p, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if body, err := ioutil.ReadAll(p.Body); len(body) != 0 || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("body = %q, %v; want empty body and unexpected EOF", body, err)
	}
	if p, err := mr.NextPart(); p != nil || !errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		t.Fatalf("NextPart() = %v, %v; want nil and unexpected EOF", p, err)
	}
}
