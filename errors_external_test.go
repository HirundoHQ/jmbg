package jmbg_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hirundohq/jmbg"
)

// TestErrZeroIsReachableFromConsumers is the point of exporting ErrZero:
// code outside the package must be able to tell "this JMBG was never
// parsed" apart from any other serialization failure.
func TestErrZeroIsReachableFromConsumers(t *testing.T) {
	type dto struct {
		JMBG jmbg.JMBG `json:"jmbg"`
	}

	tests := []struct {
		name string
		call func() error
	}{
		{"MarshalText", func() error { _, err := jmbg.JMBG{}.MarshalText(); return err }},
		{"Value", func() error { _, err := jmbg.JMBG{}.Value(); return err }},
		{"json.Marshal", func() error { _, err := json.Marshal(dto{}); return err }},
		{"sql.Null, valid but zero", func() error {
			_, err := sql.Null[jmbg.JMBG]{Valid: true}.Value()
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, jmbg.ErrZero) {
				t.Errorf("error = %v, want jmbg.ErrZero", err)
			}
		})
	}
}
