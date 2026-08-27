package jmbg

import (
	"fmt"
	"strings"
	"time"
)

// JMBG is a parsed and validated JMBG number. The zero value is not a valid
// JMBG; obtain instances via Parse.
//
// JMBG is comparable: two values are equal with == exactly when they were
// parsed from the same 13 digits, so it can be used as a map key or
// deduplicated with a set.
type JMBG struct {
	original  string
	region    Region
	unique    int
	checksum  int
	gender    Gender
	birthDate time.Time
}

// Parse parses and validates a JMBG string. Leading and trailing whitespace
// is ignored; the remainder must be exactly 13 ASCII digits. The returned
// error wraps one of the Err* sentinel errors and can be tested with
// errors.Is.
//
// Parse is a pure function of its input and has no notion of "now", so a
// birth date in the future is accepted: a YYY below 100 maps to 2000-2099
// and may not have happened yet. Age then returns a negative number.
// Callers who care should check j.BirthDate().After(time.Now()) themselves.
func Parse(input string) (JMBG, error) {
	input = strings.TrimSpace(input)

	// The count is in bytes, and says so: for multibyte input the byte and
	// character counts differ, and a bare number would mislead a caller who
	// counted characters.
	if len(input) != digitsInJMBG {
		return JMBG{}, fmt.Errorf("%w, got %d bytes", ErrLength, len(input))
	}

	var d [digitsInJMBG]byte
	copy(d[:], input)
	for _, c := range d {
		if !isDigit(c) {
			return JMBG{}, ErrDigits
		}
	}

	j, err := parseDigits(&d, input)

	// parseDigits returns its sentinels bare, so the streaming path pays
	// nothing for a reason it discards. Add the detail a direct caller
	// expects — only on failure, and by comparison rather than errors.Is,
	// since nothing has wrapped them yet.
	switch err {
	case nil:
		return j, nil
	case ErrDate:
		if year, ok := yearAt(&d); ok {
			return JMBG{}, fmt.Errorf("%w: '%02d/%02d/%d'",
				ErrDate, twoDigitAt(&d, 0), twoDigitAt(&d, 2), year)
		}
		return JMBG{}, fmt.Errorf("%w: year digits '%03d'", ErrDate, yyyAt(&d))
	case ErrRegion:
		return JMBG{}, fmt.Errorf("%w: code %d", ErrRegion, twoDigitAt(&d, 7))
	default:
		return JMBG{}, err
	}
}

// checksumWeights are the modulo-11 weights applied to the first twelve
// digits.
var checksumWeights = [12]int{7, 6, 5, 4, 3, 2, 7, 6, 5, 4, 3, 2}

// digitAt returns the value of the digit at index i.
func digitAt(d *[digitsInJMBG]byte, i int) int { return int(d[i] - '0') }

// twoDigitAt returns the two-digit value starting at index i.
func twoDigitAt(d *[digitsInJMBG]byte, i int) int {
	return digitAt(d, i)*10 + digitAt(d, i+1)
}

// yyyAt returns the three year digits as written.
func yyyAt(d *[digitsInJMBG]byte) int {
	return digitAt(d, 4)*100 + digitAt(d, 5)*10 + digitAt(d, 6)
}

// yearAt returns the full birth year, and whether the digits name one at all.
//
// YYY are the last three digits and only two windows have ever been issued:
// 800-999 for 1800-1999 and 000-099 for 2000-2099. Anything between would
// place the birth in 2100-2799, so it is not a distant birth date but a
// malformed one, and the caller reports ErrDate.
func yearAt(d *[digitsInJMBG]byte) (int, bool) {
	yyy := yyyAt(d)
	switch {
	case yyy >= 800:
		return 1000 + yyy, true
	case yyy <= 99:
		return 2000 + yyy, true
	default:
		return 0, false
	}
}

// parseDigits validates thirteen ASCII digits and builds the JMBG.
//
// It returns the Err* sentinels bare, without the detail Parse adds, and
// allocates nothing unless it succeeds. Scanner and Redactor call it for
// every 13-digit run in a stream and throw the reason away; on a log full of
// ordinary 13-digit identifiers nearly every call fails, and formatting why
// cost several times more than the validation itself.
//
// original must be the same thirteen digits as a string. A caller that
// already holds one passes it so the JMBG can share it; the streaming path
// passes "" and lets parseDigits build one — which it does only once the
// digits have proved to be a JMBG.
func parseDigits(d *[digitsInJMBG]byte, original string) (JMBG, error) {
	dd := twoDigitAt(d, 0)
	mm := twoDigitAt(d, 2)
	year, ok := yearAt(d)
	if !ok {
		return JMBG{}, ErrDate
	}

	date := time.Date(year, time.Month(mm), dd, 0, 0, 0, 0, time.UTC)
	if date.Day() != dd || int(date.Month()) != mm || date.Year() != year {
		return JMBG{}, ErrDate
	}

	region, ok := regions[twoDigitAt(d, 7)]
	if !ok {
		return JMBG{}, ErrRegion
	}

	// m = 11 - (weighted sum mod 11); if m is between 1 and 9 the checksum is
	// m, and if m is 10 or 11 the checksum is 0.
	sum := 0
	for i, w := range checksumWeights {
		sum += digitAt(d, i) * w
	}
	expected := 11 - sum%11
	if expected > 9 {
		expected = 0
	}
	checksum := digitAt(d, 12)
	if checksum != expected {
		return JMBG{}, ErrChecksum
	}

	bbb := digitAt(d, 9)*100 + digitAt(d, 10)*10 + digitAt(d, 11)
	gender := Male
	if bbb >= 500 {
		gender = Female
	}

	if original == "" {
		original = string(d[:])
	}
	return JMBG{
		original:  original,
		region:    region,
		unique:    bbb,
		checksum:  checksum,
		gender:    gender,
		birthDate: date,
	}, nil
}

// Valid reports whether the given string is a valid JMBG.
func Valid(input string) bool {
	_, err := Parse(input)
	return err == nil
}

// String returns the 13-digit JMBG string. It implements fmt.Stringer.
func (j JMBG) String() string {
	return j.original
}

// BirthDate returns the birth date at midnight UTC.
func (j JMBG) BirthDate() time.Time {
	return j.birthDate
}

// Age returns the age in full years at the current time. See AgeAt for the
// birthday convention.
func (j JMBG) Age() int {
	return j.AgeAt(time.Now())
}

// AgeAt returns the age in full years at the given moment. A person born on
// February 29 is considered to have their birthday on March 1 in non-leap
// years.
func (j JMBG) AgeAt(now time.Time) int {
	years := now.Year() - j.birthDate.Year()
	if now.Month() < j.birthDate.Month() ||
		(now.Month() == j.birthDate.Month() && now.Day() < j.birthDate.Day()) {
		years--
	}
	return years
}

// Gender returns the gender encoded in the unique number.
func (j JMBG) Gender() Gender {
	return j.gender
}

// IsMale reports whether the JMBG belongs to a male person.
func (j JMBG) IsMale() bool {
	return j.gender == Male
}

// IsFemale reports whether the JMBG belongs to a female person.
func (j JMBG) IsFemale() bool {
	return j.gender == Female
}

// IsAdult reports whether the person is 18 or older at the current time.
func (j JMBG) IsAdult() bool {
	return j.Age() >= 18
}

// Region returns the region encoded in the JMBG.
func (j JMBG) Region() Region {
	return j.region
}

// Unique returns the three-digit unique number (BBB).
func (j JMBG) Unique() int {
	return j.unique
}

// Checksum returns the check digit.
func (j JMBG) Checksum() int {
	return j.checksum
}
