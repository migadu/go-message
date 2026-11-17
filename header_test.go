package message

import (
	"reflect"
	"testing"
)

func TestHeader(t *testing.T) {
	mediaType := "text/plain"
	mediaParams := map[string]string{"charset": "utf-8"}
	desc := "Plan de complémentarité de l'Homme"
	disp := "attachment"
	dispParams := map[string]string{"filename": "complémentarité.txt"}

	var h Header
	h.SetContentType(mediaType, mediaParams)
	h.SetText("Content-Description", desc)
	h.SetContentDisposition(disp, dispParams)

	if gotMediaType, gotParams, err := h.ContentType(); err != nil {
		t.Error("Expected no error when parsing content type, but got:", err)
	} else if gotMediaType != mediaType {
		t.Errorf("Expected media type %q but got %q", mediaType, gotMediaType)
	} else if !reflect.DeepEqual(gotParams, mediaParams) {
		t.Errorf("Expected media params %v but got %v", mediaParams, gotParams)
	}

	if gotDesc, err := h.Text("Content-Description"); err != nil {
		t.Error("Expected no error when parsing content description, but got:", err)
	} else if gotDesc != desc {
		t.Errorf("Expected content description %q but got %q", desc, gotDesc)
	}

	if gotDisp, gotParams, err := h.ContentDisposition(); err != nil {
		t.Error("Expected no error when parsing content disposition, but got:", err)
	} else if gotDisp != disp {
		t.Errorf("Expected disposition %q but got %q", disp, gotDisp)
	} else if !reflect.DeepEqual(gotParams, dispParams) {
		t.Errorf("Expected disposition params %v but got %v", dispParams, gotParams)
	}
}

func TestEmptyContentType(t *testing.T) {
	var h Header

	mediaType := "text/plain"
	if gotMediaType, _, err := h.ContentType(); err != nil {
		t.Error("Expected no error when parsing empty content type, but got:", err)
	} else if gotMediaType != mediaType {
		t.Errorf("Expected media type %q but got %q", mediaType, gotMediaType)
	}
}

func TestKnownCharset(t *testing.T) {
	var h Header

	h.Set("Subject", "=?ISO-8859-1?B?SWYgeW91IGNhbiByZWFkIHRoaXMgeW8=?=")

	fields := h.Fields()
	if !fields.Next() {
		t.Error("Expected to be able to advance to first header item")
	}

	_, err := fields.Text()
	if err != nil {
		t.Error("Expected no error when decoding header key of known charset, but got: ", err)
	}
}

func TestUnknownCharset(t *testing.T) {
	var h Header

	h.Set("Subject", "=?INVALIDCHARSET?B?dSB1bmRlcnN0YW5kIHRoZSBleGFtcGxlLg==?=")

	fields := h.Fields()
	if !fields.Next() {
		t.Error("Expected to be able to advance to first header item")
	}

	_, err := fields.Text()
	if err == nil {
		t.Error("Expected error decoding header key of unknown charset")
	}
	if !IsUnknownCharset(err) {
		t.Error("Expected error to verify IsUnknownCharset")
	}
}

func TestHeader_trailingSemicolons(t *testing.T) {
	tests := []struct {
		field, value string
		wantValue    string
		wantParams   map[string]string
	}{
		{"Content-Type", "text/plain; charset=iso-8859-1;", "text/plain", map[string]string{"charset": "iso-8859-1"}},
		{"Content-Type", "text/plain; charset=iso-8859-1;;", "text/plain", map[string]string{"charset": "iso-8859-1"}},
		{"Content-Type", "text/plain; charset=iso-8859-1; ;", "text/plain", map[string]string{"charset": "iso-8859-1"}},
		{"Content-Type", "text/plain;charset=iso-8859-1;\t; ;", "text/plain", map[string]string{"charset": "iso-8859-1"}},
		{"Content-Type", "multipart/alternative; boundary=b1;;", "multipart/alternative", map[string]string{"boundary": "b1"}},
		{"Content-Type", "text/plain;;", "text/plain", map[string]string{}},
		{"Content-Disposition", "attachment; filename=a.pdf;;", "attachment", map[string]string{"filename": "a.pdf"}},
	}
	for _, tc := range tests {
		var h Header
		h.Set(tc.field, tc.value)
		var v string
		var params map[string]string
		var err error
		if tc.field == "Content-Type" {
			v, params, err = h.ContentType()
		} else {
			v, params, err = h.ContentDisposition()
		}
		if err != nil || v != tc.wantValue || !reflect.DeepEqual(params, tc.wantParams) {
			t.Errorf("%s: %q = %q %v (err %v), want %q %v", tc.field, tc.value, v, params, err, tc.wantValue, tc.wantParams)
		}
	}
}

// A value that is still invalid without its trailing semicolons keeps the
// strict parser's result for the value as given: no parameters, the error, and
// the media type, or the whole value when no media type parsed either.
func TestHeader_trailingSemicolonsInvalid(t *testing.T) {
	tests := []struct{ value, want string }{
		{"text/plain; format;;", "text/plain"},
		{"text/plain; charset=\"a;;", "text/plain"},
		{"text/plain; charset=a; charset=b;;", "text/plain; charset=a; charset=b;;"},
	}
	for _, tc := range tests {
		var h Header
		h.Set("Content-Type", tc.value)
		v, params, err := h.ContentType()
		if err == nil || v != tc.want || params != nil {
			t.Errorf("ContentType(%q) = %q %v (err %v), want %q, no parameters and an error", tc.value, v, params, err, tc.want)
		}
	}
}
