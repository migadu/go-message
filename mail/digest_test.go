package mail_test

import (
	"io"
	"io/ioutil"
	"strings"
	"testing"

	"github.com/emersion/go-message/mail"
)

func TestReaderMultipartDigestDefault(t *testing.T) {
	const embedded = "From: sender@example.org\r\nSubject: embedded\r\nContent-Type: text/html\r\nContent-Transfer-Encoding: base64\r\n\r\nPHA+aGVsbG88L3A+"
	raw := "Content-Type: multipart/digest; boundary=d\r\n\r\n" +
		"--d\r\n\r\n" + embedded + "\r\n" +
		"--d\r\nContent-Type: text/plain\r\n\r\nexplicit text\r\n" +
		"--d\r\nContent-Type: \r\n\r\nempty type\r\n" +
		"--d--\r\n"
	r, err := mail.CreateReader(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for i, want := range []struct {
		body       string
		attachment bool
	}{
		{embedded, true},
		{"explicit text", false},
		{"empty type", false},
	} {
		p, err := r.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		if want.attachment {
			if _, ok := p.Header.(*mail.AttachmentHeader); !ok {
				t.Errorf("part %d header = %T, want attachment for implicit message/rfc822", i, p.Header)
			}
			if p.Header.Get("Content-Type") != "" {
				t.Error("implicit Content-Type was inserted into mail part headers")
			}
		} else if _, ok := p.Header.(*mail.InlineHeader); !ok {
			t.Errorf("part %d header = %T, want inline", i, p.Header)
		}
		body, err := ioutil.ReadAll(p.Body)
		if err != nil || string(body) != want.body {
			t.Errorf("part %d body = %q, %v; want %q", i, body, err, want.body)
		}
	}
	if _, err := r.NextPart(); err != io.EOF {
		t.Fatalf("final NextPart = %v; want io.EOF", err)
	}
}
