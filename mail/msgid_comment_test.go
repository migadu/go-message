package mail_test

import (
	"reflect"
	"testing"

	"github.com/emersion/go-message/mail"
)

func TestHeader_MessageID_commentValidation(t *testing.T) {
	for _, comment := range []string{
		"(raw\x00nul)",
		"(escaped\\\x00nul)",
		"(invalid\xffutf8)",
		"(escaped\\\xffutf8)",
		"(lone\rcarriage return)",
		"(lone\nline feed)",
		"(bad\r\nInjected: yes)",
		"(escaped\\\r\nInjected: yes)",
		"(escaped\\\r\n folded)",
		"(escaped\\\nline feed)",
	} {
		for _, raw := range []string{
			`<"local"` + comment + `@example.com>`,
			`<local@` + comment + `example.com>`,
			`<local@example.com` + comment + `>`,
			comment + `<local@example.com>`,
			`<local@example.com>` + comment,
		} {
			t.Run(raw, func(t *testing.T) {
				var h mail.Header
				h.Set("Message-ID", raw)
				if id, err := h.MessageID(); err == nil || id != "" {
					t.Errorf("MessageID() = %q, %v; want error for malformed comment", id, err)
				}
				for _, field := range []string{"References", "In-Reply-To"} {
					h.Set(field, raw)
					if ids, err := h.MsgIDList(field); err == nil {
						t.Errorf("MsgIDList(%q) = %q without error for malformed comment", field, ids)
					}
				}
			})
		}
	}
}

func TestHeader_MessageID_validObsoleteComments(t *testing.T) {
	// RFC 5322 obs-ctext permits non-whitespace controls, including DEL.
	// Folding remains valid only when the CRLF is followed by whitespace.
	for _, comment := range []string{
		`(nested (inner) comment)`,
		`(escaped \( and \) and \\)`,
		"(UTF-8 café)",
		"(tab\tcomment)",
		"(folded\r\n comment)",
		"(folded\r\n\tcomment)",
		"(obsolete\x01\x08\x0b\x0c\x0e\x1f\x7f)",
		"(escaped\\\x01\\\x7f)",
	} {
		t.Run(comment, func(t *testing.T) {
			raw := comment + ` < ` + comment + ` "local" ` + comment + ` @ ` + comment + ` example.com ` + comment + ` > ` + comment
			var h mail.Header
			h.Set("Message-ID", raw)
			if id, err := h.MessageID(); err != nil || id != "local@example.com" {
				t.Fatalf("MessageID() = %q, %v; want local@example.com", id, err)
			}
			h.Set("References", raw+` <other@example.org>`)
			if ids, err := h.MsgIDList("References"); err != nil || !reflect.DeepEqual(ids, []string{"local@example.com", "other@example.org"}) {
				t.Fatalf("MsgIDList() = %q, %v", ids, err)
			}
		})
	}
}
