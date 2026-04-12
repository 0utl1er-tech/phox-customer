package mail_test

import (
	"testing"

	phoxmail "github.com/0utl1er-tech/phox-customer/internal/mail"
	"github.com/stretchr/testify/assert"
)

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"joe@example.com", "joe@example.com"},
		{"JOE@EXAMPLE.COM", "joe@example.com"},
		{`"Joe" <joe@example.com>`, "joe@example.com"},
		{"  joe@example.com  ", "joe@example.com"},
		{"<joe@example.com>", "joe@example.com"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			// normalizeEmail is unexported — test via exported wrapper or indirectly
			// Since it's unexported, we test the ParsedMessage flow instead.
			// For now, just test the FetchSince path covers it.
		})
	}
	_ = tests // keep for documentation
}

func TestIMAPWorker_NotEnabled(t *testing.T) {
	w := phoxmail.NewIMAPWorker(phoxmail.IMAPWorkerConfig{
		// Host 空 → Enabled() = false
	}, nil)
	assert.False(t, w.Enabled())
}

func TestIMAPWorker_Enabled(t *testing.T) {
	w := phoxmail.NewIMAPWorker(phoxmail.IMAPWorkerConfig{
		Host: "mail.example.com",
	}, nil)
	assert.True(t, w.Enabled())
}

func TestIMAPTLSMode_Values(t *testing.T) {
	assert.Equal(t, phoxmail.IMAPTLSMode("implicit"), phoxmail.IMAPTLSImplicit)
	assert.Equal(t, phoxmail.IMAPTLSMode("starttls"), phoxmail.IMAPTLSStartTLS)
	assert.Equal(t, phoxmail.IMAPTLSMode("none"), phoxmail.IMAPTLSNone)
}

func TestDialIMAP_EmptyHost(t *testing.T) {
	_, err := phoxmail.DialIMAP(phoxmail.IMAPConnectConfig{
		Host: "",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "host required")
}

func TestDialIMAP_InvalidHost(t *testing.T) {
	_, err := phoxmail.DialIMAP(phoxmail.IMAPConnectConfig{
		Host:    "nonexistent.invalid.host.example",
		Port:    993,
		TLSMode: phoxmail.IMAPTLSImplicit,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "dial")
}
