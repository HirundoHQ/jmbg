package jmbg

import (
	"database/sql/driver"
	"fmt"
)

// MarshalText implements encoding.TextMarshaler, emitting the 13-digit
// JMBG string. It returns ErrZero for the zero value.
func (j JMBG) MarshalText() ([]byte, error) {
	if j.original == "" {
		return nil, ErrZero
	}
	return []byte(j.original), nil
}

// UnmarshalText implements encoding.TextUnmarshaler by delegating to
// Parse, so an invalid JMBG can never enter the system. The error still
// satisfies errors.Is against the Err* sentinels through the wrapping
// encoding/json adds. On failure the receiver is left untouched.
func (j *JMBG) UnmarshalText(b []byte) error {
	parsed, err := Parse(string(b))
	if err != nil {
		return err
	}
	*j = parsed
	return nil
}

// Value implements driver.Valuer, storing the 13-digit JMBG string. It
// returns ErrZero for the zero value.
func (j JMBG) Value() (driver.Value, error) {
	if j.original == "" {
		return nil, ErrZero
	}
	return j.original, nil
}

// Scan implements sql.Scanner, accepting the string and []byte forms a
// driver may deliver a text column in and validating them through Parse.
// Any other type — including nil, which is how a SQL NULL arrives —
// returns an error; use database/sql.Null[JMBG] for nullable columns. On
// failure the receiver is left untouched.
func (j *JMBG) Scan(src any) error {
	switch s := src.(type) {
	case string:
		return j.UnmarshalText([]byte(s))
	case []byte:
		return j.UnmarshalText(s)
	default:
		return fmt.Errorf("jmbg: cannot scan %T into JMBG; use sql.Null[JMBG] for nullable columns", src)
	}
}
