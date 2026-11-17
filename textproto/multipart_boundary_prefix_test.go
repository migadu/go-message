package textproto

import (
	"io"
	"io/ioutil"
	"strings"
	"testing"
	"testing/iotest"
)

func TestMultipartSingleDashPrefix(t *testing.T) {
	for _, ending := range []struct{ name, value string }{{"CRLF", "\r\n"}, {"LF", "\n"}} {
		for _, input := range []struct {
			name string
			wrap func(io.Reader) io.Reader
		}{
			{"buffered", func(r io.Reader) io.Reader { return r }},
			{"bytewise", iotest.OneByteReader},
		} {
			for _, prefix := range []string{"--b-extra", "--b-"} {
				for _, position := range []string{"leading", "later"} {
					t.Run(ending.name+"/"+input.name+"/"+prefix+"/"+position, func(t *testing.T) {
						want := prefix + ending.value + "tail"
						if position == "later" {
							want = "intro" + ending.value + want
						}
						raw := strings.Join([]string{"--b", "", want, "--b", "", "second", "--b--", ""}, ending.value)
						mr := NewMultipartReader(input.wrap(strings.NewReader(raw)), "b")
						for i, body := range []string{want, "second"} {
							part, err := mr.NextPart()
							if err != nil {
								t.Fatalf("part %d: NextPart() = %v", i, err)
							}
							got, err := ioutil.ReadAll(part)
							if err != nil || string(got) != body {
								t.Errorf("part %d: body = %q, %v; want %q", i, got, err, body)
							}
						}
						if _, err := mr.NextPart(); err != io.EOF {
							t.Fatalf("NextPart() = %v, want io.EOF", err)
						}
					})
				}
			}

			t.Run(ending.name+"/"+input.name+"/truncated", func(t *testing.T) {
				want := "body" + ending.value + "--b-"
				raw := "--b" + ending.value + ending.value + want
				mr := NewMultipartReader(input.wrap(strings.NewReader(raw)), "b")
				part, err := mr.NextPart()
				if err != nil {
					t.Fatal(err)
				}
				got, err := ioutil.ReadAll(part)
				if string(got) != want || err != io.ErrUnexpectedEOF {
					t.Fatalf("body = %q, %v; want %q, io.ErrUnexpectedEOF", got, err, want)
				}
			})
		}
	}
}
