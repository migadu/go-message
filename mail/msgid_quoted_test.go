package mail_test

import (
	"reflect"
	"testing"

	"github.com/emersion/go-message/mail"
)

func TestHeader_MessageID_quotedLocal(t *testing.T) {
	tests := []struct{ raw, want string }{
		{`<simple.id@example.com>`, `simple.id@example.com`},
		{`<"simple.id"@example.com>`, `simple.id@example.com`},
		{`<"simple\.id"@example.com>`, `simple.id@example.com`},
		{`<"local@tag"@example.com>`, `"local@tag"@example.com`},
		{`<"local\@tag"@example.com>`, `"local@tag"@example.com`},
		{`<"a\"b\\c"@example.com>`, `"a\"b\\c"@example.com`},
		{`<"a b"@example.com>`, `"a b"@example.com`},
		{`<"a..b"@example.com>`, `"a..b"@example.com`},
		{`<""@example.com>`, `""@example.com`},
		{`<"left<right>"@example.com>`, `"left<right>"@example.com`},
		{`<"café"@example.com>`, `café@example.com`},
		{`<"left".right@example.com>`, `left.right@example.com`},
		{`<left."right@tag"@example.com>`, `"left.right@tag"@example.com`},
		{`(before) < (left) "local@tag" (separator) @ (domain) example.com (end) > (after)`, `"local@tag"@example.com`},
		{`<local@[literal@tag]>`, `local@[literal@tag]`},
	}
	for _, test := range tests {
		t.Run(test.raw, func(t *testing.T) {
			var h mail.Header
			h.Set("Message-ID", test.raw)
			id, err := h.MessageID()
			if err != nil || id != test.want {
				t.Fatalf("MessageID() = %q, %v; want %q", id, err, test.want)
			}

			// Normalized identifiers must retain quoting where necessary, so
			// the public setter produces an identifier the parser can read again.
			h.SetMessageID(id)
			if raw := h.Get("Message-ID"); raw != "<"+test.want+">" {
				t.Errorf("SetMessageID() wrote %q", raw)
			}
			if got, err := h.MessageID(); err != nil || got != id {
				t.Errorf("setter roundtrip = %q, %v; want %q", got, err, id)
			}
		})
	}
}

func TestHeader_MsgIDList_quotedLocal(t *testing.T) {
	for _, field := range []string{"References", "In-Reply-To"} {
		t.Run(field, func(t *testing.T) {
			var h mail.Header
			h.Set(field, `<plain@example.com> (between) <"simple"@example.com> <"local\@tag"@example.com> <"a b"@example.com> <"a\"b\\c"@example.com>`)
			want := []string{`plain@example.com`, `simple@example.com`, `"local@tag"@example.com`, `"a b"@example.com`, `"a\"b\\c"@example.com`}
			ids, err := h.MsgIDList(field)
			if err != nil || !reflect.DeepEqual(ids, want) {
				t.Fatalf("MsgIDList() = %q, %v; want %q", ids, err, want)
			}
			h.SetMsgIDList(field, ids)
			if got, err := h.MsgIDList(field); err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("setter roundtrip = %q, %v; want %q", got, err, want)
			}
		})
	}
}

func TestHeader_MessageID_quotedLocalInvalid(t *testing.T) {
	for _, raw := range []string{
		`<"unterminated@example.com>`,
		`<"trailing\`,
		`<"good"example.com>`,
		`<"good"@>`,
		`<"good"@example.com`,
		`<"good"@example.com>junk`,
		`<"good"@example@com>`,
		`<plain@middle@example.com>`,
		`<"good"junk@example.com>`,
		`<"good".@example.com>`,
		`<"good".."more"@example.com>`,
		`<(unclosed "good"@example.com>`,
		`<"good" (unclosed @example.com>`,
		"<\"left\r\nInjected: yes\"@example.com>",
		"<\"left\\\r\nInjected: yes\"@example.com>",
		"<\"left\x00right\"@example.com>",
		"<\"left\x7fright\"@example.com>",
		"<\"left\xffright\"@example.com>",
	} {
		t.Run(raw, func(t *testing.T) {
			var h mail.Header
			h.Set("Message-ID", raw)
			if id, err := h.MessageID(); err == nil || id != "" {
				t.Errorf("MessageID() = %q, %v; want error", id, err)
			}
			for _, field := range []string{"References", "In-Reply-To"} {
				h.Set(field, raw)
				if ids, err := h.MsgIDList(field); err == nil {
					t.Errorf("MsgIDList(%q) = %q without error", field, ids)
				}
			}
		})
	}
}
