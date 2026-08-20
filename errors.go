package jmbg

import "errors"

// Sentinel errors returned by Parse, possibly wrapped with additional detail.
// Test for them with errors.Is.
var (
	ErrLength   = errors.New("jmbg: must have exactly 13 digits")
	ErrDigits   = errors.New("jmbg: must contain only digits")
	ErrDate     = errors.New("jmbg: birth date is not valid")
	ErrRegion   = errors.New("jmbg: region is not valid")
	ErrChecksum = errors.New("jmbg: checksum is not valid")
)
