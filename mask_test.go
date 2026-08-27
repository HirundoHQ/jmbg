package jmbg

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

var _ slog.LogValuer = JMBG{}

func TestMasked(t *testing.T) {
	tests := []struct {
		name string
		jmbg JMBG
		want string
	}{
		// The mask must not depend on gender, so cover both: unique 001 is
		// male, unique 508 is female.
		{"male jmbg", mustParse(t, "0710003730015"), "0710*******15"},
		{"female jmbg", mustParse(t, "2307988805080"), "2307*******80"},
		{"zero value", JMBG{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.jmbg.Masked()
			if got != tt.want {
				t.Errorf("Masked() = %q, want %q", got, tt.want)
			}
			if tt.want != "" && len(got) != 13 {
				t.Errorf("len(Masked()) = %d, want 13", len(got))
			}
		})
	}
}

func TestLogValue(t *testing.T) {
	j := mustParse(t, "0710003730015")
	if got := j.LogValue().String(); got != "0710*******15" {
		t.Errorf("LogValue() = %q, want %q", got, "0710*******15")
	}
}

// TestLogValueDoesNotLeakThroughSlog is the leak test: the negative
// assertion is the point of it.
func TestLogValueDoesNotLeakThroughSlog(t *testing.T) {
	const full = "0710003730015"
	j := mustParse(t, full)

	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("user loaded", "jmbg", j)

	out := buf.String()
	if !strings.Contains(out, "0710*******15") {
		t.Errorf("log output %q does not contain the masked JMBG", out)
	}
	if strings.Contains(out, full) {
		t.Errorf("log output %q leaked the full JMBG", out)
	}
}
