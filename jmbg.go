// Package jmbg validates and parses Serbian unique master citizen numbers
// (JMBG - Jedinstveni Matični Broj Građana).
//
// JMBG format: DDMMYYYRRBBBC
//
//   - DD  - Day of birth (01-31)
//   - MM  - Month of birth (01-12)
//   - YYY - Year of birth (last 3 digits)
//   - RR  - Region code
//   - BBB - Unique number (000-499 male, 500-999 female)
//   - C   - Checksum digit
package jmbg

import (
	"fmt"
	"strings"
	"time"
)

// JMBG is a parsed and validated JMBG number. The zero value is not a valid
// JMBG; obtain instances via Parse.
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
func Parse(input string) (JMBG, error) {
	input = strings.TrimSpace(input)

	if len(input) != 13 {
		return JMBG{}, fmt.Errorf("%w, got %d", ErrLength, len(input))
	}

	for _, c := range input {
		if c < '0' || c > '9' {
			return JMBG{}, ErrDigits
		}
	}

	digit := func(i int) int { return int(input[i] - '0') }
	twoDigit := func(i int) int { return digit(i)*10 + digit(i+1) }

	dd := twoDigit(0)
	mm := twoDigit(2)
	yyy := digit(4)*100 + digit(5)*10 + digit(6)
	rr := twoDigit(7)
	bbb := digit(9)*100 + digit(10)*10 + digit(11)
	c := digit(12)

	// Determine full year
	year := 1000 + yyy
	if yyy < 800 {
		year = 2000 + yyy
	}

	// Validate date
	date := time.Date(year, time.Month(mm), dd, 0, 0, 0, 0, time.UTC)
	if date.Day() != dd || int(date.Month()) != mm || date.Year() != year {
		return JMBG{}, fmt.Errorf("%w: '%02d/%02d/%d'", ErrDate, dd, mm, year)
	}

	// Validate region
	region, ok := regions[rr]
	if !ok {
		return JMBG{}, fmt.Errorf("%w: code %d", ErrRegion, rr)
	}

	// Validate checksum (modulo 11).
	//
	// m = 11 - (weighted sum mod 11); if m is between 1 and 9 the checksum is m,
	// and if m is 10 or 11 the checksum is 0.
	w := []int{7, 6, 5, 4, 3, 2, 7, 6, 5, 4, 3, 2}
	sum := 0
	for i := 0; i < 12; i++ {
		sum += digit(i) * w[i]
	}
	expectedChecksum := 11 - sum%11
	if expectedChecksum > 9 {
		expectedChecksum = 0
	}
	if c != expectedChecksum {
		return JMBG{}, ErrChecksum
	}

	gender := Male
	if bbb >= 500 {
		gender = Female
	}

	return JMBG{
		original:  input,
		region:    region,
		unique:    bbb,
		checksum:  c,
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
