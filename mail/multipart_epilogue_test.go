package mail_test

import (
	"io"
	"io/ioutil"
	"strings"
	"testing"

	"github.com/emersion/go-message/mail"
)

func TestReader_multipartFinalBoundaryEpilogue(t *testing.T) {
	for _, test := range []struct {
		name, suffix string
	}{
		{"separate line", ""},
		{"same line with space", " inner epilogue"},
		{"same line without space", "inner epilogue"},
	} {
		t.Run(test.name, func(t *testing.T) {
			// The inner close delimiter must finish only the alternative part:
			// its epilogue is ignored, while the outer attachment remains readable.
			// Similar-looking lines inside the text part are ordinary body data.
			raw := strings.Join([]string{
				"MIME-Version: 1.0",
				"Content-Type: multipart/mixed; boundary=outer",
				"",
				"--outer",
				"Content-Type: multipart/alternative; boundary=inner",
				"",
				"--inner",
				"Content-Type: text/plain; charset=utf-8",
				"Content-Transfer-Encoding: quoted-printable",
				"",
				"caf=C3=A9",
				"--inner-extra",
				"--innerX",
				"text --inner-- is not a delimiter",
				"--inner",
				"Content-Type: text/html; charset=utf-8",
				"",
				"<p>café</p>",
				"--inner--" + test.suffix,
				"ignored inner epilogue",
				"--outer",
				"Content-Type: application/octet-stream",
				"Content-Disposition: attachment; filename=note.bin",
				"Content-Transfer-Encoding: base64",
				"",
				"AAECaGVsbG8K",
				"--outer-- outer epilogue",
				"ignored outer epilogue",
				"",
			}, "\r\n")

			mr, err := mail.CreateReader(strings.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			defer mr.Close()

			for i, want := range []struct {
				body       string
				attachment bool
			}{
				{"café\r\n--inner-extra\r\n--innerX\r\ntext --inner-- is not a delimiter", false},
				{"<p>café</p>", false},
				{"\x00\x01\x02hello\n", true},
			} {
				part, err := mr.NextPart()
				if err != nil {
					t.Fatalf("part %d: NextPart() = %v", i, err)
				}
				if want.attachment {
					h, ok := part.Header.(*mail.AttachmentHeader)
					if !ok {
						t.Fatalf("part %d: header is %T, want attachment", i, part.Header)
					}
					if filename, err := h.Filename(); err != nil || filename != "note.bin" {
						t.Errorf("attachment filename = %q, %v; want note.bin", filename, err)
					}
				} else if _, ok := part.Header.(*mail.InlineHeader); !ok {
					t.Errorf("part %d: header is %T, want inline", i, part.Header)
				}
				body, err := ioutil.ReadAll(part.Body)
				if err != nil || string(body) != want.body {
					t.Fatalf("part %d: body = %q, %v; want %q", i, body, err, want.body)
				}
			}
			if part, err := mr.NextPart(); err != io.EOF {
				t.Fatalf("NextPart() = %v, %v; want io.EOF", part, err)
			}
		})
	}
}
