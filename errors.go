package jmbg

import "errors"

// Sentinel errors returned by Parse, possibly wrapped with additional detail.
// UnmarshalText and Scan delegate to Parse, so they return these too. Test
// for them with errors.Is.
var (
	ErrLength   = errors.New("jmbg: must have exactly 13 digits")
	ErrDigits   = errors.New("jmbg: must contain only digits")
	ErrDate     = errors.New("jmbg: birth date is not valid")
	ErrRegion   = errors.New("jmbg: region is not valid")
	ErrChecksum = errors.New("jmbg: checksum is not valid")
)

// ErrZero is returned by MarshalText and Value when the JMBG is the zero
// value. It carries no digits, so emitting it would produce a value that
// could never be parsed back; serialization fails loudly instead. Express
// optionality with *JMBG or database/sql.Null[JMBG].
var ErrZero = errors.New("jmbg: zero value cannot be serialized; use *JMBG or sql.Null[JMBG] for optional values")
