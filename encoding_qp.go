package message

import (
	"bufio"
	"fmt"
	"io"
)

// qpLineReader inserts soft breaks between complete quoted-printable tokens
// before handing them to the standard decoder, whose fixed line buffer rejects
// long physical lines. It follows lenientQPReader, retaining that reader's
// handling of malformed equals sequences.
//
// Interior whitespace is encoded before inserting breaks, while whitespace at
// a physical line ending or EOF is discarded just as the standard decoder
// would discard it. Deciding between those cases requires lookahead: keep at
// most 4096 consecutive unencoded whitespace bytes, then report an error.
// Such a run is already outside valid QP's 76-byte physical line limit and was
// rejected by the standard decoder. Never buffer an arbitrary physical line.
type qpLineReader struct {
	wrapped *bufio.Reader
	column  int
	err     error

	output     [6]byte // one soft break followed by one three-byte token
	outputNext int
	outputEnd  int

	whitespace     [4096]byte
	whitespaceNext int
	whitespaceEnd  int
}

func newQPLineReader(r io.Reader) *qpLineReader {
	return &qpLineReader{wrapped: bufio.NewReader(r)}
}

func (r *qpLineReader) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		if r.outputNext < r.outputEnd {
			written := copy(p[n:], r.output[r.outputNext:r.outputEnd])
			n += written
			r.outputNext += written
			continue
		}
		if r.err != nil {
			return n, r.err
		}

		token, size, lineEnd, err := r.nextToken()
		r.err = err
		r.outputNext, r.outputEnd = 0, 0
		if size == 0 {
			continue
		}
		// Reserve one column for a soft-break '='. Complete =XX escapes and
		// original line endings are never split by an inserted break.
		if !lineEnd && r.column+size > 75 {
			r.outputEnd = copy(r.output[:], "=\r\n")
			r.column = 0
		}
		r.outputEnd += copy(r.output[r.outputEnd:], token[:size])
		if lineEnd {
			r.column = 0
		} else {
			r.column += size
		}
	}
	return n, nil
}

// nextToken returns a complete encoded byte or a hard/soft physical line
// ending. It only encodes literal whitespace once a following non-whitespace
// byte establishes that it belongs to the body, rather than line padding.
func (r *qpLineReader) nextToken() (token [3]byte, size int, lineEnd bool, err error) {
	if r.whitespaceNext < r.whitespaceEnd {
		c := r.whitespace[r.whitespaceNext]
		r.whitespaceNext++
		if r.whitespaceNext == r.whitespaceEnd {
			r.whitespaceNext, r.whitespaceEnd = 0, 0
		}
		const hex = "0123456789ABCDEF"
		return [3]byte{'=', hex[c>>4], hex[c&15]}, 3, false, nil
	}

	c, err := r.wrapped.ReadByte()
	if err != nil {
		return token, 0, false, err
	}
	switch c {
	case ' ', '\t', '\r':
		r.whitespace[0], r.whitespaceEnd = c, 1
		for {
			next, err := r.wrapped.Peek(1)
			if err != nil {
				// Padding at EOF (or before a source error) is discarded by
				// the standard decoder. Preserve the original read error.
				r.whitespaceEnd = 0
				return token, 0, false, err
			}
			switch next[0] {
			case ' ', '\t', '\r':
				if r.whitespaceEnd == len(r.whitespace) {
					return token, 0, false, fmt.Errorf("quotedprintable: unencoded whitespace run exceeds %d bytes", len(r.whitespace))
				}
				r.whitespace[r.whitespaceEnd] = next[0]
				r.whitespaceEnd++
				r.wrapped.Discard(1)
			case '\n':
				// Only an immediately preceding CR makes this CRLF;
				// other trailing CR/SP/HT bytes are discarded.
				hasCR := r.whitespace[r.whitespaceEnd-1] == '\r'
				r.whitespaceEnd = 0
				r.wrapped.Discard(1)
				if hasCR {
					return [3]byte{'\r', '\n'}, 2, true, nil
				}
				return [3]byte{'\n'}, 1, true, nil
			default:
				return r.nextToken()
			}
		}
	case '\n':
		return [3]byte{'\n'}, 1, true, nil
	case '=':
		next, _ := r.wrapped.Peek(2)
		if len(next) >= 1 && next[0] == '\n' {
			r.wrapped.Discard(1)
			return [3]byte{'=', '\n'}, 2, true, nil
		}
		if len(next) == 2 {
			if next[0] == '\r' && next[1] == '\n' {
				r.wrapped.Discard(2)
				return [3]byte{'=', '\r', '\n'}, 3, true, nil
			}
			if isHex(next[0]) && isHex(next[1]) {
				r.wrapped.Discard(2)
				return [3]byte{'=', next[0], next[1]}, 3, false, nil
			}
		}
		// Normally lenientQPReader has already escaped a literal '='.
		// An interrupted source can leave a partial escape; preserve the
		// literal byte and let the buffered source error surface afterward.
		return [3]byte{'=', '3', 'D'}, 3, false, nil
	default:
		return [3]byte{c}, 1, false, nil
	}
}
