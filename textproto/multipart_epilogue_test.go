package textproto

import (
	"errors"
	"io"
	"io/ioutil"
	"strings"
	"testing"
	"testing/iotest"
)

func TestMultipartFinalBoundaryStaysEOF(t *testing.T) {
	for _, newline := range []string{"\r\n", "\n"} {
		for _, suffix := range []string{"", " epilogue", "epilogue"} {
			for _, empty := range []bool{false, true} {
				name := "CRLF"
				if newline == "\n" {
					name = "LF"
				}
				if empty {
					name += "/empty"
				} else {
					name += "/one-part"
				}
				t.Run(name+"/"+suffix, func(t *testing.T) {
					body := ""
					if !empty {
						body = "--b" + newline + newline + "A" + newline
					}
					body += "--b--" + suffix + newline +
						"--b" + newline +
						"X-Epilogue: must-not-be-a-part" + newline + newline +
						"fake part" + newline + "--b--" + newline
					mr := NewMultipartReader(strings.NewReader(body), "b")
					if !empty {
						readEpilogueTestPart(t, mr)
					}
					for call := 0; call < 3; call++ {
						if p, err := mr.NextPart(); p != nil || err != io.EOF {
							t.Fatalf("NextPart after final delimiter, call %d = %v, %v; want nil, io.EOF", call+1, p, err)
						}
					}
				})
			}
		}
	}
}

func TestMultipartFinalBoundaryLongEpilogue(t *testing.T) {
	readers := []struct {
		name string
		wrap func(io.Reader) io.Reader
	}{
		{"buffered", func(r io.Reader) io.Reader { return r }},
		{"bytewise", iotest.OneByteReader},
	}
	for _, reader := range readers {
		for _, ending := range []string{"", "\r\n"} {
			name := reader.name + "/no-newline"
			if ending != "" {
				name = reader.name + "/newline"
			}
			t.Run(name, func(t *testing.T) {
				body := "--b\r\n\r\nA\r\n--b--" + strings.Repeat("epilogue", 8192) + ending
				mr := NewMultipartReader(reader.wrap(strings.NewReader(body)), "b")
				readEpilogueTestPart(t, mr)
				for call := 0; call < 2; call++ {
					if p, err := mr.NextPart(); p != nil || err != io.EOF {
						t.Fatalf("NextPart after long epilogue, call %d = %v, %v; want nil, io.EOF", call+1, p, err)
					}
				}
			})
		}
	}
}

// An epilogue can come from an unfinished stream. Once the closing delimiter
// has arrived, finding a newline or an EOF in that stream must not be needed
// to finish the multipart message.
func TestMultipartFinalBoundaryDoesNotDrainEpilogue(t *testing.T) {
	for _, bytewise := range []bool{false, true} {
		name := "buffered"
		if bytewise {
			name = "bytewise"
		}
		t.Run(name, func(t *testing.T) {
			r := &epilogueUnreadTail{
				prefix: "--b\r\n\r\nA\r\n--b--",
			}
			var input io.Reader = r
			if bytewise {
				input = iotest.OneByteReader(input)
			}
			mr := NewMultipartReader(input, "b")
			readEpilogueTestPart(t, mr)
			for call := 0; call < 2; call++ {
				if p, err := mr.NextPart(); p != nil || err != io.EOF {
					t.Fatalf("NextPart after complete final marker, call %d = %v, %v; want nil, io.EOF", call+1, p, err)
				}
			}
			if r.tailReads != 0 {
				t.Fatalf("read from epilogue %d times after final marker", r.tailReads)
			}
		})
	}
}

func readEpilogueTestPart(t *testing.T, mr *MultipartReader) {
	t.Helper()
	p, err := mr.NextPart()
	if err != nil {
		t.Fatalf("NextPart() = %v; want first part", err)
	}
	body, err := ioutil.ReadAll(p)
	if err != nil || string(body) != "A" {
		t.Fatalf("part body = %q, %v; want A, nil", body, err)
	}
}

type epilogueUnreadTail struct {
	prefix    string
	tailReads int
}

func (r *epilogueUnreadTail) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.prefix == "" {
		r.tailReads++
		return 0, errors.New("epilogue is not available yet")
	}
	n := copy(p, r.prefix)
	r.prefix = r.prefix[n:]
	return n, nil
}
