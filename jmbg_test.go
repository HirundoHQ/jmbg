package jmbg

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func mustParse(t *testing.T, input string) JMBG {
	t.Helper()
	j, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse(%q): unexpected error: %v", input, err)
	}
	return j
}

func TestValidJmbgReturnsTrue(t *testing.T) {
	if !Valid("0710003730015") {
		t.Error("expected Valid to return true for valid JMBG")
	}
}

func TestInvalidJmbgReturnsFalse(t *testing.T) {
	if Valid("1234567890123") {
		t.Error("expected Valid to return false for invalid JMBG")
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{"too short", "123456789", ErrLength},
		{"too long", "07100037300151", ErrLength},
		{"empty", "", ErrLength},
		{"ascii letters", "01019907100ab", ErrDigits},
		// Unicode digits, 13 bytes long so they reach the digit check.
		{"superscript digit", "01019907100²", ErrDigits},
		{"arabic-indic digit", "01019907100٩", ErrDigits},
		{"day out of range", "3201990710009", ErrDate},
		{"feb 29 in non-leap year", "2902979758318", ErrDate},
		{"unknown region", "0710003660015", ErrRegion},
		// Intermediate control value 10 or 11 requires check digit 0.
		{"checksum: intermediate value 8, wrong digit", "0710003730025", ErrChecksum},
		{"checksum: intermediate value 10, digit 1", "0101990710131", ErrChecksum},
		{"checksum: intermediate value 10, digit 9", "0101990710139", ErrChecksum},
		{"checksum: intermediate value 11, digit 5", "0101990710045", ErrChecksum},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j, err := Parse(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Parse(%q) error = %v, want %v", tt.input, err, tt.wantErr)
			}
			if j != (JMBG{}) {
				t.Errorf("Parse(%q) returned a non-zero JMBG alongside an error", tt.input)
			}
		})
	}
}

func TestChecksumZero(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"intermediate value 10", "0101990710130"},
		{"intermediate value 11", "0101990710040"},
		{"intermediate value 10, female", "2307988805080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := mustParse(t, tt.input)
			if j.Checksum() != 0 {
				t.Errorf("Checksum() = %d, want 0", j.Checksum())
			}
		})
	}
}

func TestRegions(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  Region
	}{
		{"Belgrade", "2902992710005", Region{71, "Belgrade", "Serbia"}},
		{"Novi Sad", "1505995800002", Region{80, "Novi Sad", "Serbia/Vojvodina"}},
		{
			"registered abroad", "1505995700008",
			Region{70, "citizens registered abroad at a diplomatic/consular post", "Serbia"},
		},
		{
			"single-digit region, foreigners in Serbia", "0101990060011",
			Region{6, "foreigners in Serbia", "foreign citizens"},
		},
		{"Pljevlja and Žabljak", "0101990290017", Region{29, "Pljevlja, Žabljak", "Montenegro"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := mustParse(t, tt.input)
			if j.Region() != tt.want {
				t.Errorf("Region() = %+v, want %+v", j.Region(), tt.want)
			}
		})
	}
}

func TestGender(t *testing.T) {
	tests := []struct {
		input      string
		wantGender Gender
		wantString string
		wantMale   bool
	}{
		{"0710003730015", Male, "Male", true},
		{"0710003735017", Female, "Female", false},
	}
	for _, tt := range tests {
		t.Run(tt.wantString, func(t *testing.T) {
			j := mustParse(t, tt.input)
			if j.Gender() != tt.wantGender {
				t.Errorf("Gender() = %v, want %v", j.Gender(), tt.wantGender)
			}
			if j.Gender().String() != tt.wantString {
				t.Errorf("Gender().String() = %q, want %q", j.Gender().String(), tt.wantString)
			}
			if j.IsMale() != tt.wantMale {
				t.Errorf("IsMale() = %v, want %v", j.IsMale(), tt.wantMale)
			}
			if j.IsFemale() == tt.wantMale {
				t.Errorf("IsFemale() = %v, want %v", j.IsFemale(), !tt.wantMale)
			}
		})
	}
}

func TestAgeAt(t *testing.T) {
	tests := []struct {
		name  string
		birth time.Time
		now   time.Time
		want  int
	}{
		{"day before birthday", date(2003, time.October, 7), date(2021, time.October, 6), 17},
		{"on 18th birthday", date(2003, time.October, 7), date(2021, time.October, 7), 18},
		{"day after birthday", date(2003, time.October, 7), date(2021, time.October, 8), 18},
		// Both directions of the leap-year off-by-one a day-of-year
		// comparison would introduce.
		{
			"born in leap year, birthday in non-leap year",
			date(1992, time.June, 15), date(2015, time.June, 15), 23,
		},
		{
			"evaluated in leap year, day before a post-February birthday",
			date(2006, time.March, 1), date(2024, time.February, 29), 17,
		},
		// A Feb 29 birthday is observed on Mar 1 in non-leap years.
		{"born Feb 29, non-leap Feb 28", date(2008, time.February, 29), date(2026, time.February, 28), 17},
		{"born Feb 29, non-leap Mar 1", date(2008, time.February, 29), date(2026, time.March, 1), 18},
		{"born Feb 29, leap-year birthday", date(2008, time.February, 29), date(2028, time.February, 29), 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := JMBG{birthDate: tt.birth}
			if got := j.AgeAt(tt.now); got != tt.want {
				t.Errorf("AgeAt(%s) = %d, want %d for birth date %s",
					tt.now.Format("2006-01-02"), got, tt.want, tt.birth.Format("2006-01-02"))
			}
		})
	}
}

func TestAge(t *testing.T) {
	j := mustParse(t, "0710003730015")
	// Exact arithmetic is pinned by TestAgeAt; born 2003-10-07, the age is at
	// least 22 from 2025-10-07 on and can only grow.
	if age := j.Age(); age < 22 {
		t.Errorf("Age() = %d, want at least 22 for a person born 2003-10-07", age)
	}
}

func TestIsAdultReturnsTrue(t *testing.T) {
	j := mustParse(t, "0710003730015")
	if !j.IsAdult() {
		t.Error("expected IsAdult to return true for a person born in 2003")
	}
}

func TestIsAdultReturnsFalse(t *testing.T) {
	// Relative birth date, so the person stays ten years old forever.
	j := JMBG{birthDate: time.Now().AddDate(-10, 0, 0)}
	if j.IsAdult() {
		t.Error("expected IsAdult to return false for a ten-year-old")
	}
}

func TestAccessors(t *testing.T) {
	j := mustParse(t, "2902992710005")

	if j.String() != "2902992710005" {
		t.Errorf("String() = %q, want %q", j.String(), "2902992710005")
	}
	if got := j.BirthDate(); got != date(1992, time.February, 29) {
		t.Errorf("BirthDate() = %v, want 1992-02-29", got)
	}
	if want := (Region{71, "Belgrade", "Serbia"}); j.Region() != want {
		t.Errorf("Region() = %+v, want %+v", j.Region(), want)
	}
	if j.Unique() != 0 {
		t.Errorf("Unique() = %d, want 0", j.Unique())
	}
	if j.Checksum() != 5 {
		t.Errorf("Checksum() = %d, want 5", j.Checksum())
	}
}

func TestTrimWhitespace(t *testing.T) {
	j := mustParse(t, "  0710003730015  ")
	if j.String() != "0710003730015" {
		t.Errorf("String() = %q, want %q", j.String(), "0710003730015")
	}
}

func TestYearCalculation(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"2000s", "0101000710009", 2000},
		{"1900s", "1705978730032", 1978},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := mustParse(t, tt.input)
			if got := j.BirthDate().Year(); got != tt.want {
				t.Errorf("BirthDate().Year() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBoundaryUniqueNumbers(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		unique   int
		wantMale bool
	}{
		{"unique 0 is male", "1505995800002", 0, true},
		{"unique 499 is male", "1505995804997", 499, true},
		{"unique 500 is female", "1505995805004", 500, false},
		{"unique 999 is female", "1505995809999", 999, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := mustParse(t, tt.input)
			if j.Unique() != tt.unique {
				t.Errorf("Unique() = %d, want %d", j.Unique(), tt.unique)
			}
			if j.IsMale() != tt.wantMale {
				t.Errorf("IsMale() = %v, want %v", j.IsMale(), tt.wantMale)
			}
		})
	}
}

func TestLeapYearDate(t *testing.T) {
	j := mustParse(t, "2902992710005")
	if got := j.BirthDate(); got != date(1992, time.February, 29) {
		t.Errorf("BirthDate() = %v, want 1992-02-29", got)
	}
}

func FuzzParse(f *testing.F) {
	seeds := []string{
		"0710003730015",
		"0101990710130",
		"0101990710040",
		"2307988805080",
		"1505995700008",
		"2902992710005",
		"0101000710009",
		"1705978730032",
		"0101990060011",
		"  0710003730015  ",
		"1234567890123",
		"0710003730025",
		"0101990710131",
		"01019907100ab",
		"01019907100²",
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		j, err := Parse(input)
		if Valid(input) != (err == nil) {
			t.Errorf("Valid(%q) disagrees with Parse error %v", input, err)
		}
		if err != nil {
			if j != (JMBG{}) {
				t.Errorf("Parse(%q) returned a non-zero JMBG alongside error %v", input, err)
			}
			return
		}
		if j.String() != strings.TrimSpace(input) {
			t.Errorf("String() = %q, want trimmed input %q", j.String(), strings.TrimSpace(input))
		}
	})
}
