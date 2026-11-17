package textproto

import (
	"bufio"
	"errors"
	"io"
	"io/ioutil"
	"strings"
	"testing"
	"testing/iotest"
)

// RFC 2046 section 5.1.1 ends a multipart with a close-delimiter. Running
// out of input before it is truncation, not a successful end of the parts.
func TestMultipartReader_truncationError(t *testing.T) {
	for _, newline := range []struct{ name, value string }{{"CRLF", "\r\n"}, {"LF", "\n"}} {
		for _, test := range []struct {
			name, body, wantBody string
			part                 bool
		}{
			{"empty", "", "", false},
			{"preamble", "preamble\n", "", false},
			{"incomplete initial delimiter", "--b-", "", false},
			{"missing part header", "--b\n", "", true},
			{"incomplete part header", "--b\nContent-Type: text/plain", "", true},
			{"missing close", "--b\n\ntext", "text", true},
			{"incomplete close", "--b\n\ntext\n--b-", "text\n--b-", true},
		} {
			for _, bytewise := range []bool{false, true} {
				for _, readPart := range []bool{false, true} {
					if !test.part && readPart {
						continue
					}
					name := newline.name + "/" + test.name
					if bytewise {
						name += "/bytewise"
					}
					if readPart {
						name += "/read"
					} else {
						name += "/skip"
					}
					t.Run(name, func(t *testing.T) {
						var input io.Reader = strings.NewReader(strings.ReplaceAll(test.body, "\n", newline.value))
						if bytewise {
							input = iotest.OneByteReader(input)
						}
						mr := NewMultipartReader(input, "b")
						if test.part {
							p, err := mr.NextPart()
							if err != nil {
								t.Fatal(err)
							}
							if readPart {
								body, err := ioutil.ReadAll(p)
								want := strings.ReplaceAll(test.wantBody, "\n", newline.value)
								if string(body) != want || !errors.Is(err, io.ErrUnexpectedEOF) {
									t.Fatalf("part body = %q, %v; want %q and unexpected EOF", body, err, want)
								}
							}
						}
						p, err := mr.NextPart()
						if p != nil || !errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
							t.Fatalf("NextPart() = %v, %v; want nil and an error wrapping io.ErrUnexpectedEOF, not io.EOF", p, err)
						}
					})
				}
			}
		}
	}
}

func TestMultipartReader_wrapReadError(t *testing.T) {
	want := errors.New("source failed")
	for _, prefix := range []string{"", "preamble\r\n", "--b\r\n\r\ntext\r\n--b-"} {
		mr := NewMultipartReader(io.MultiReader(strings.NewReader(prefix), multipartFailReader{want}), "b")
		if strings.HasPrefix(prefix, "--b\r\n") {
			if _, err := mr.NextPart(); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := mr.NextPart(); !errors.Is(err, want) || errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("prefix %q: NextPart() = %v, want wrapped source error", prefix, err)
		}
	}

	// Other failures must remain distinguishable from truncated input too.
	mr := NewMultipartReader(strings.NewReader(strings.Repeat("x", 8192)), "b")
	if _, err := mr.NextPart(); !errors.Is(err, bufio.ErrBufferFull) {
		t.Errorf("long preamble: NextPart() = %v, want wrapped bufio.ErrBufferFull", err)
	}
}

func TestMultipartReader_completeCloseEOF(t *testing.T) {
	for _, close := range []string{"--b--", "--b--\r\n", "--b--epilogue"} {
		mr := NewMultipartReader(strings.NewReader("--b\r\n\r\ntext\r\n"+close), "b")
		p, err := mr.NextPart()
		if err != nil {
			t.Fatal(err)
		}
		if body, err := ioutil.ReadAll(p); string(body) != "text" || err != nil {
			t.Fatalf("body = %q, %v; want text, nil", body, err)
		}
		if p, err := mr.NextPart(); p != nil || err != io.EOF {
			t.Errorf("close %q: NextPart() = %v, %v; want nil, io.EOF", close, p, err)
		}
	}
}

type multipartFailReader struct{ err error }

func (r multipartFailReader) Read(p []byte) (int, error) { return 0, r.err }
