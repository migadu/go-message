package mail_test

import (
	"testing"

	"github.com/emersion/go-message/mail"
)

func TestAttachmentHeader_FilenameUnquotedSpaces(t *testing.T) {
	for _, value := range []string{"attachment; filename=Concur _1_.png", "inline; filename=Concur _1_.png", "attachment; filename=Concur _1_.png;;"} {
		var h mail.AttachmentHeader
		h.Set("Content-Disposition", value)
		h.Set("Content-Type", "image/png; name=fallback.png")
		if filename, err := h.Filename(); err != nil || filename != "Concur _1_.png" {
			t.Errorf("Filename(%q) = %q, %v; want Concur _1_.png", value, filename, err)
		}
	}
}
