package message

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestRead_longQuotedPrintable(t *testing.T) {
	for _, length := range []int{4093, 4094, 4095, 4096, 4097, 8192, 256 << 10} {
		t.Run(fmt.Sprint(length), func(t *testing.T) {
			body := strings.Repeat("x", length) + "=20tail\r\nlast line\r\n"
			raw := "Content-Type: text/plain; charset=utf-8\r\n" +
				"Content-Transfer-Encoding: quoted-printable\r\n\r\n" + body
			entity, err := Read(strings.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(entity.Body)
			want := strings.Repeat("x", length) + " tail\r\nlast line\r\n"
			if err != nil || string(got) != want {
				t.Fatalf("decoded %d/%d bytes; error = %v", len(got), len(want), err)
			}
		})
	}
}

func TestDecode_longQuotedPrintable(t *testing.T) {
	long := strings.Repeat("x", 8192)
	tests := []struct{ name, encoded, decoded string }{
		{"split hex escape", strings.Repeat("x", 4095) + "=C3=A9\r\nTAIL", strings.Repeat("x", 4095) + "é\r\nTAIL"},
		{"many hex escapes", strings.Repeat("word=20", 20000) + "TAIL", strings.Repeat("word ", 20000) + "TAIL"},
		{"soft break", long + "=\r\nTAIL", long + "TAIL"},
		{"LF soft break", long + "=\nTAIL", long + "TAIL"},
		{"malformed soft break", long + "= \t\r\nTAIL", long + "TAIL"},
		{"space before soft break", long + " \t=\r\nTAIL", long + " \tTAIL"},
		{"trailing whitespace", long + " \t\r\nTAIL \t", long + "\r\nTAIL"},
		{"encoded trailing whitespace", long + "=20=09\r\nTAIL", long + " \t\r\nTAIL"},
		{"malformed literals", long + "=ZZ = 5 =\rX\r\nTAIL", long + "=ZZ = 5 =\rX\r\nTAIL"},
		{"bare CR", long + "\rX\r\nTAIL", long + "\rX\r\nTAIL"},
		{"EOF literal equals", long + "=", long + "="},
		{"LF hard break", long + "\nTAIL", long + "\nTAIL"},
	}
	for _, test := range tests {
		for _, bytewise := range []bool{false, true} {
			name := test.name + "/buffered"
			if bytewise {
				name = test.name + "/bytewise"
			}
			t.Run(name, func(t *testing.T) {
				var source io.Reader = strings.NewReader(test.encoded)
				if bytewise {
					source = iotest.OneByteReader(source)
				}
				reader, err := encodingReader("quoted-printable", source)
				if err != nil {
					t.Fatal(err)
				}
				got, err := io.ReadAll(iotest.HalfReader(reader))
				if err != nil || string(got) != test.decoded {
					t.Fatalf("decoded %d/%d bytes; error = %v", len(got), len(test.decoded), err)
				}
			})
		}
	}
}

func TestDecode_longQuotedPrintableSourceError(t *testing.T) {
	wantErr := errors.New("source read failed")
	for _, length := range []int{8, 8192} {
		for _, together := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/data-and-error-%v", length, together), func(t *testing.T) {
				want := strings.Repeat("x", length) + " tail"
				source := &longQPErrorReader{data: strings.Repeat("x", length) + "=20tail", err: wantErr, together: together}
				reader, err := encodingReader("quoted-printable", source)
				if err != nil {
					t.Fatal(err)
				}
				got, err := io.ReadAll(reader)
				if !errors.Is(err, wantErr) || string(got) != want {
					t.Fatalf("decoded %d/%d bytes; error = %v, want source error", len(got), len(want), err)
				}
			})
		}
	}
}

// Unencoded whitespace must be withheld until its following byte determines
// whether it is interior text or trailing whitespace to discard. An excessive
// ambiguous run must fail, rather than silently drop bytes or buffer a whole
// arbitrarily long physical line.
func TestDecode_longQuotedPrintableWhitespaceLimit(t *testing.T) {
	input := "text" + strings.Repeat(" \t", 64<<10) + "TAIL\r\n"
	source := &longQPCountReader{Reader: strings.NewReader(input)}
	reader, err := encodingReader("quoted-printable", source)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(reader)
	if err == nil {
		t.Fatal("excessive ambiguous whitespace run was silently accepted")
	}
	if source.n == len(input) {
		t.Fatal("consumed the entire excessive whitespace run before rejecting it")
	}
}

func TestDecode_longQuotedPrintableInvalidByte(t *testing.T) {
	reader, err := encodingReader("quoted-printable", strings.NewReader(strings.Repeat("x", 8192)+"\x00TAIL"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(reader)
	if err == nil || !strings.Contains(err.Error(), "invalid unescaped byte") {
		t.Fatalf("error = %v; want invalid unescaped byte", err)
	}
}

type longQPErrorReader struct {
	data     string
	err      error
	together bool
}

func (r *longQPErrorReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 && r.together {
		return n, r.err
	}
	return n, nil
}

type longQPCountReader struct {
	io.Reader
	n int
}

func (r *longQPCountReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.n += n
	return n, err
}
