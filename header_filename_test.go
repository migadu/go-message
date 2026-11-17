package message

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestHeader_unquotedFilenameSpaces(t *testing.T) {
	for _, tc := range []struct {
		value, filename string
	}{
		{"inline; filename=Concur _1_.png", "Concur _1_.png"},
		{"attachment ; FILENAME \t= \tQuarterly  Report-2026.pdf \t", "Quarterly  Report-2026.pdf"},
		{`attachment; size=42; filename=Quarterly report.pdf; description="A; \"quoted\" note"`, "Quarterly report.pdf"},
		{`attachment; description="literal (parentheses); filename=decoy.txt"; filename=real report.pdf`, "real report.pdf"},
		{"attachment; description=\"A\tB\"; filename=real report.pdf", "real report.pdf"},
		{"attachment; description=\"café\"; filename=real report.pdf", "real report.pdf"},
		{"attachment; filename=report 'v2'.pdf", "report 'v2'.pdf"},
		{"attachment; filename=report final.pdf;;", "report final.pdf"},
		{"attachment; filename=report final.pdf; \t;", "report final.pdf"},
	} {
		t.Run(tc.value, func(t *testing.T) {
			var h Header
			h.Set("Content-Disposition", tc.value)
			disp, params, err := h.ContentDisposition()
			wantDisp := "attachment"
			if strings.HasPrefix(tc.value, "inline") {
				wantDisp = "inline"
			}
			if err != nil || disp != wantDisp || params["filename"] != tc.filename {
				t.Fatalf("ContentDisposition() = %q, %v, %v; want %q and filename %q", disp, params, err, wantDisp, tc.filename)
			}
			if got := h.Get("Content-Disposition"); got != tc.value {
				t.Fatalf("raw field changed to %q", got)
			}
		})
	}
}

func TestHeader_unquotedFilenameInvalidPreservesError(t *testing.T) {
	for _, value := range []string{
		"attachment; filename=report\tfinal.pdf",
		"attachment; filename=report \x00final.pdf",
		"attachment; filename=report \rfinal.pdf",
		"attachment; filename=report \nfinal.pdf",
		"attachment; filename=report fïnal.pdf",
		"attachment; filename=path/to report.pdf",
		`attachment; filename=path\to report.pdf`,
		`attachment; filename=report "final".pdf`,
		"attachment; filename=report (final).pdf",
		"attachment; filename=report final.pdf size=123",
		"attachment; filename=report final.pdf; name=also broken",
		"attachment; filename=report final.pdf; description=\"bad\x00value\"",
		"attachment; filename=report final.pdf; description=\"bad\x01value\"",
		"attachment; filename=report final.pdf; description=\"bad\rvalue\"",
		"attachment; filename=report final.pdf; description=\"bad\nvalue\"",
		"attachment; filename=report final.pdf; description=\"bad\x7fvalue\"",
		"attachment; filename=report final.pdf; description=\"bad\\\x00value\"",
		"attachment; filename=report final.pdf; description=\"bad\xffvalue\"",
		"attachment; filename=report final.pdf; description=\"bad\\\xffvalue\"",
		"attachment; filename=report final.pdf; description=\"bad\xed\xa0\x80value\"",
		`attachment; description="closed"junk; filename=report final.pdf`,
		`attachment; description="unclosed; filename=report final.pdf`,
		`attachment; description=oops"; filename=report final.pdf`,
		"attachment (note); filename=report final.pdf",
		"attachment; size=42 (bytes); filename=report final.pdf",
		"attachment; filename=report final.pdf; filename=other.txt",
		"attachment; filename=report final.pdf; filename=report final.pdf",
		"attachment; filename=report final.pdf; FILENAME*=UTF-8''preferred.pdf",
		"attachment; filename*0*=UTF-8''preferred; filename*1=.pdf; filename=report final.pdf",
		"attachment; filename*bogus=unknown; filename=report final.pdf",
		"attachment; filename=report final.pdf; filename*=UTF-8''bad%ZZ",
		"attachment; name=report final.pdf",
	} {
		t.Run(value, func(t *testing.T) {
			wantDisp, wantParams, wantErr := parseHeaderWithParams(value)
			if wantErr == nil {
				t.Fatal("invalid test input was accepted by the original parser")
			}
			var h Header
			h.Set("Content-Disposition", value)
			disp, params, err := h.ContentDisposition()
			if err == nil || err.Error() != wantErr.Error() || disp != wantDisp || !reflect.DeepEqual(params, wantParams) {
				t.Fatalf("got %q, %v, %v; want original result %q, %v, %v", disp, params, err, wantDisp, wantParams, wantErr)
			}
		})
	}
}

func TestHeader_unquotedFilenameKeepsStrictInputs(t *testing.T) {
	for _, value := range []string{
		`attachment; filename="report; final.pdf"`,
		`attachment; filename="report \"final\".pdf"`,
		`attachment; filename="legacy final.pdf"; filename*=UTF-8''preferred%20final.pdf`,
		`attachment; filename="legacy.pdf"; filename*0*=UTF-8''preferred; filename*1*=%20final.pdf`,
		`attachment; filename="report.pdf"; filename="report.pdf"`,
		"attachment; description=\"bad\xffvalue\"; filename=report.pdf",
		"attachment; filename=\"bad\xffvalue.pdf\"",
	} {
		wantDisp, wantParams, wantErr := parseHeaderWithParams(value)
		if wantErr != nil {
			t.Fatal(wantErr)
		}
		var h Header
		h.Set("Content-Disposition", value)
		disp, params, err := h.ContentDisposition()
		if err != nil || disp != wantDisp || !reflect.DeepEqual(params, wantParams) {
			t.Errorf("%q: got %q, %v, %v; want original %q, %v", value, disp, params, err, wantDisp, wantParams)
		}
	}
	for _, value := range []string{"text/plain; name=report final.txt", "multipart/mixed; boundary=some boundary", "text/plain; charset=utf 8"} {
		var h Header
		h.Set("Content-Type", value)
		if _, params, err := h.ContentType(); err == nil || params != nil {
			t.Errorf("ContentType(%q) = %v, %v; want original invalid parameters", value, params, err)
		}
	}
}

func TestRead_unquotedFilenamePreservesRawMessage(t *testing.T) {
	for _, field := range []string{"Content-Disposition: attachment; filename=Concur _1_.png\r\n", "Content-Disposition: attachment; filename=Concur\r\n _1_.png\r\n"} {
		raw := "Mime-Version: 1.0\r\nContent-Type: image/png\r\nContent-Transfer-Encoding: binary\r\n" + field + "\r\nPNG bytes\r\n"
		e, err := Read(strings.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		if _, params, err := e.Header.ContentDisposition(); err != nil || params["filename"] != "Concur _1_.png" {
			t.Fatalf("ContentDisposition() = %v, %v", params, err)
		}
		var out bytes.Buffer
		if err := e.WriteTo(&out); err != nil {
			t.Fatal(err)
		}
		if got := out.String(); got != raw {
			t.Fatalf("Read/WriteTo changed raw message:\ngot %q\nwant %q", got, raw)
		}
	}
}
