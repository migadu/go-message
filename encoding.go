package message

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/quotedprintable"
	"strings"
)

type UnknownEncodingError struct {
	e error
}

func (u UnknownEncodingError) Unwrap() error { return u.e }

func (u UnknownEncodingError) Error() string {
	return "encoding error: " + u.e.Error()
}

// IsUnknownEncoding returns a boolean indicating whether the error is known to
// report that the encoding advertised by the entity is unknown.
func IsUnknownEncoding(err error) bool {
	return errors.As(err, new(UnknownEncodingError))
}

// encodingMechanism returns the mechanism a Content-Transfer-Encoding field
// value names, without the comments around it: MIME header fields "can
// include RFC 822 comments, which have no semantic content and should be
// ignored during MIME processing" (RFC 2045 section 1).
func encodingMechanism(enc string) string {
	if !strings.ContainsRune(enc, '(') {
		return enc
	}
	var b strings.Builder
	depth := 0
	for i := 0; i < len(enc); i++ {
		switch c := enc[i]; {
		case depth > 0 && c == '\\':
			i++ // quoted-pair
		case c == '(':
			depth++
			b.WriteByte(' ') // a comment separates tokens
		case depth > 0 && c == ')':
			depth--
		case depth == 0:
			b.WriteByte(c)
		}
	}
	return strings.TrimSpace(b.String())
}

func encodingReader(enc string, r io.Reader) (io.Reader, error) {
	var dec io.Reader
	switch strings.ToLower(encodingMechanism(enc)) {
	case "quoted-printable":
		// Wrap with lenient QP reader to handle malformed QP encoding
		// (e.g., "= " followed by whitespace which some encoders produce)
		wrapped := &lenientQPReader{wrapped: bufio.NewReader(r)}
		dec = quotedprintable.NewReader(wrapped)
	case "base64":
		wrapped := &whitespaceReplacingReader{wrapped: r}
		dec = base64.NewDecoder(base64.StdEncoding, wrapped)
	case "7bit", "8bit", "binary", "":
		dec = r
	default:
		return nil, fmt.Errorf("unhandled encoding %q", enc)
	}
	return dec, nil
}

type nopCloser struct {
	io.Writer
}

func (nopCloser) Close() error {
	return nil
}

func encodingWriter(enc string, w io.Writer) (io.WriteCloser, error) {
	var wc io.WriteCloser
	switch strings.ToLower(encodingMechanism(enc)) {
	case "quoted-printable":
		wc = quotedprintable.NewWriter(w)
	case "base64":
		wc = base64.NewEncoder(base64.StdEncoding, &lineWrapper{w: w, maxLineLen: 76})
	case "7bit", "8bit":
		// RFC 5322 §2.1.1 limits lines to 998 octets. For 7bit/8bit the body is
		// written verbatim, so a line longer than that is hard-wrapped by
		// inserting a CRLF, which becomes a real line break in the decoded body.
		// Content that needs wrapping isn't truly 7bit/8bit-representable and
		// should be re-encoded as quoted-printable or base64 by the caller.
		wc = nopCloser{&lineWrapper{w: w, maxLineLen: 998}}
	case "binary", "":
		wc = nopCloser{w}
	default:
		return nil, fmt.Errorf("unhandled encoding %q", enc)
	}
	return wc, nil
}

// whitespaceReplacingReader replaces space and tab characters with a LF so
// base64 bodies with a continuation indent can be decoded by the base64 decoder
// even though it is against the spec.
type whitespaceReplacingReader struct {
	wrapped io.Reader
}

func (r *whitespaceReplacingReader) Read(p []byte) (int, error) {
	n, err := r.wrapped.Read(p)

	for i := 0; i < n; i++ {
		if p[i] == ' ' || p[i] == '\t' {
			p[i] = '\n'
		}
	}

	return n, err
}

// lenientQPReader rewrites malformed quoted-printable so the strict stdlib
// decoder accepts it, while preserving valid sequences. A "line ending" here is
// CRLF or a bare LF; a bare CR (not followed by LF) is not a line ending.
// It handles the following cases:
//  1. "=[ws](CR)LF" (whitespace before a soft break): the whitespace is
//     stripped, yielding a valid "=(CR)LF" soft break.
//  2. "=[ws]X" or "=X" where X is not a valid hex pair: the '=' is a literal,
//     encoded as "=3D". This includes "=" followed by a bare CR.
//  3. "=62" and other valid "=XX" hex sequences: preserved.
//  4. "=(CR)LF" valid soft break: preserved.
//  5. Whitespace immediately before a soft break is encoded to "=20"/"=09" to
//     preserve it (the decoder would otherwise strip it). Whitespace before a
//     real line ending, or at end of input, is left for the decoder to strip
//     per RFC 2045 §6.7.
type lenientQPReader struct {
	wrapped *bufio.Reader
	pending []byte // data waiting to be read (e.g. from expansion)
}

// lineEndingAhead reports whether the byte c, previously peeked at 0-based
// offset off in the wrapped reader, begins a line ending: a LF, or a CR
// followed by LF. A bare CR is not a line ending. When the byte after a CR
// cannot be peeked because the buffer is exhausted, the CR is treated as a
// line ending (preserving lenient behavior at the buffer boundary); at EOF a
// trailing CR is genuinely bare and is not a line ending.
func (r *lenientQPReader) lineEndingAhead(off int, c byte) bool {
	switch c {
	case '\n':
		return true
	case '\r':
		peeked, err := r.wrapped.Peek(off + 2)
		if len(peeked) > off+1 {
			return peeked[off+1] == '\n'
		}
		return err != io.EOF
	}
	return false
}

func (r *lenientQPReader) Read(p []byte) (int, error) {
	if len(r.pending) > 0 {
		n := copy(p, r.pending)
		r.pending = r.pending[n:]
		return n, nil
	}

	i := 0
	for i < len(p) {
		// Peek 1 byte
		bPrefix, err := r.wrapped.Peek(1)
		if err != nil {
			if i > 0 {
				return i, nil
			}
			return 0, err
		}

		b := bPrefix[0]

		if b == '=' {
			// Look ahead to categorize
			peekSize := 2
			spacesCount := 0
			var targetChar byte
			isSoftBreak := false
			isLiteral := false

			for {
				peaked, _ := r.wrapped.Peek(peekSize)
				if len(peaked) < peekSize {
					// EOF or peek error (e.g. buffer full): targetChar stays 0
					// and the switch below classifies the '=' as a literal.
					break
				}

				c := peaked[peekSize-1]
				if c == ' ' || c == '\t' {
					spacesCount++
					peekSize++
					continue
				}

				targetChar = c
				break
			}

			// Determine whether targetChar begins a soft line break. A soft
			// break is "=" + optional whitespace + a line ending. A bare CR
			// (not followed by LF) is NOT a line ending, so in that case the
			// '=' is a literal.
			switch {
			case r.lineEndingAhead(peekSize-1, targetChar):
				if spacesCount > 0 {
					// Malformed soft break: =[spaces](CR)LF. Strip spaces.
					isSoftBreak = true
				}
				// Else valid soft break: =(CR)LF. Fall through to default copy.
			case spacesCount == 0 && isHex(targetChar):
				// Possibly valid QP hex (e.g. =62): need the second digit.
				peaked3, _ := r.wrapped.Peek(3)
				if len(peaked3) < 3 || !isHex(peaked3[2]) {
					// =X? or =X[EOF]. Malformed.
					isLiteral = true
				}
				// Else valid QP hex: =XX. Preserved.
			default:
				// =[spaces]X, =[bad char], =[bare CR], or =[EOF]. Malformed
				// literal.
				isLiteral = true
			}

			if isLiteral {
				// Emit '=3D'. Convert = to =3D.
				toWrite := []byte("=3D")
				n := copy(p[i:], toWrite)
				i += n
				r.wrapped.Discard(1) // Consume '='

				if n < 3 {
					// Pending remainder
					r.pending = toWrite[n:]
					return i, nil
				}
				continue
			} else if isSoftBreak {
				// Malformed soft break: emit '=' but skip spaces
				p[i] = '='
				i++
				r.wrapped.Discard(1 + spacesCount) // Consume '=' and spaces
				continue
			}
			// Default: Valid soft break or Valid Hex. Copy '='.
			p[i] = '='
			i++
			r.wrapped.Discard(1)
		} else if b == ' ' || b == '\t' {
			// Check for trailing whitespace
			isTrailing := false
			peekSize := 1
			for {
				peaked, _ := r.wrapped.Peek(peekSize)
				if len(peaked) < peekSize {
					// EOF after the whitespace run. Leave it unencoded so the
					// decoder strips it (RFC 2045 §6.7: trailing whitespace on
					// the last line is deleted).
					break
				}
				c := peaked[peekSize-1]
				if c == ' ' || c == '\t' {
					peekSize++
					continue
				}
				if c == '=' {
					// Check if '=' is a soft break
					// We need to look ahead from 'peekSize' (position of '=')
					// We can't reuse r.wrapped.Peek easily with large offset unless we loop?
					// actually we can.

					// We need to check if '=' is followed by [spaces]\r\n
					// We start scanning from peekSize.
					subPeekSize := peekSize + 1
					for {
						subPeaked, _ := r.wrapped.Peek(subPeekSize)
						if len(subPeaked) < subPeekSize {
							// EOF
							break
						}
						sc := subPeaked[subPeekSize-1]
						if sc == ' ' || sc == '\t' {
							subPeekSize++
							continue
						}
						// The whitespace run precedes a soft break only if a
						// line ending follows the '='; a bare CR is not a line
						// ending (the '=' branch treats it as a literal), so
						// the whitespace stays mid-line and is left as-is.
						if r.lineEndingAhead(subPeekSize-1, sc) {
							isTrailing = true
						}
						break
					}
				}
				break
			}

			if isTrailing {
				// Encode to =20 or =09
				toWrite := []byte("=20")
				if b == '\t' {
					toWrite = []byte("=09")
				}
				n := copy(p[i:], toWrite)
				i += n
				r.wrapped.Discard(1)
				if n < 3 {
					r.pending = toWrite[n:]
					return i, nil
				}
				continue
			}
			p[i] = b
			i++
			r.wrapped.Discard(1)
		} else {
			p[i] = b
			i++
			r.wrapped.Discard(1)
		}
	}
	return i, nil
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f')
}

type lineWrapper struct {
	w          io.Writer
	maxLineLen int

	curLineLen int
	cr         bool
}

func (w *lineWrapper) Write(b []byte) (int, error) {
	var written int
	for len(b) > 0 {
		var l []byte
		l, b = cutLine(b, w.maxLineLen-w.curLineLen)

		lf := bytes.HasSuffix(l, []byte("\n"))
		l = bytes.TrimSuffix(l, []byte("\n"))

		n, err := w.w.Write(l)
		if err != nil {
			return written, err
		}
		written += n

		cr := bytes.HasSuffix(l, []byte("\r"))
		if len(l) == 0 {
			cr = w.cr
		}

		if !lf && len(b) == 0 {
			w.curLineLen += len(l)
			w.cr = cr
			break
		}
		w.curLineLen = 0

		ending := []byte("\r\n")
		if cr {
			ending = []byte("\n")
		}
		_, err = w.w.Write(ending)
		if err != nil {
			return written, err
		}
		// If the written `\n` was part of the input bytes slice, then account for it.
		if lf {
			written++
		}
		w.cr = false
	}

	return written, nil
}

func cutLine(b []byte, max int) ([]byte, []byte) {
	for i := 0; i < len(b); i++ {
		if b[i] == '\r' && i == max {
			continue
		}
		if b[i] == '\n' {
			return b[:i+1], b[i+1:]
		}
		if i >= max {
			return b[:i], b[i:]
		}
	}
	return b, nil
}
