package jmbg

import (
	"bytes"
	"compress/gzip"
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

// collectMatches feeds input to a Scanner in chunks of the given size and
// returns everything it reports.
func collectMatches(t *testing.T, input string, chunkSize int) []Match {
	t.Helper()
	var got []Match
	s := NewScanner(func(m Match) { got = append(got, m) })
	for i := 0; i < len(input); i += chunkSize {
		end := min(i+chunkSize, len(input))
		if _, err := s.Write([]byte(input[i:end])); err != nil {
			t.Fatalf("Write: unexpected error: %v", err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: unexpected error: %v", err)
	}
	return got
}

func TestScanner(t *testing.T) {
	type match struct {
		jmbg   string
		offset int64
	}
	tests := []struct {
		name  string
		input string
		want  []match
	}{
		{"jmbg alone", "0710003730015", []match{{"0710003730015", 0}}},
		{"embedded in text", "JMBG: 0710003730015.", []match{{"0710003730015", 6}}},
		{"run touching end of stream", "id=0710003730015", []match{{"0710003730015", 3}}},
		{
			"two jmbgs", "0710003730015 and 0101000710009",
			[]match{{"0710003730015", 0}, {"0101000710009", 18}},
		},
		// Offsets are byte offsets: 'ć' is two bytes.
		{"multibyte text before", "Perić: 0710003730015", []match{{"0710003730015", 8}}},
		{"json string value", `{"jmbg":"0710003730015"}`, []match{{"0710003730015", 9}}},
		// Only maximal runs of exactly 13 digits are considered.
		{"14-digit run", "00710003730015", nil},
		{"13-digit window inside longer run", "0710003730015999", nil},
		{"12-digit run", "071000373001", nil},
		{"separated digits", "0710003 730015", nil},
		// Candidates still go through full validation.
		{"invalid checksum", "0710003730025", nil},
		{"invalid region", "0710003660015", nil},
		// A JSON number drops a leading zero; deliberately not recovered.
		{"leading zero lost in json number", `{"jmbg":101000710009}`, nil},
		{"empty input", "", nil},
		{"no digits at all", "no digits here", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Every chunking must produce identical results.
			for _, chunkSize := range []int{1, 4, len(tt.input) + 1} {
				var got []match
				for _, m := range collectMatches(t, tt.input, chunkSize) {
					got = append(got, match{m.JMBG.String(), m.Offset})
				}
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("chunk size %d: matches = %v, want %v", chunkSize, got, tt.want)
				}
			}
		})
	}
}

func TestScannerReusableAfterClose(t *testing.T) {
	var got []Match
	s := NewScanner(func(m Match) { got = append(got, m) })

	s.Write([]byte("0710003730015"))
	s.Close()
	s.Write([]byte("0101000710009"))
	s.Close()

	if len(got) != 2 {
		t.Fatalf("got %d matches, want 2", len(got))
	}
	if got[1].JMBG.String() != "0101000710009" || got[1].Offset != 13 {
		t.Errorf("second match = %s at %d, want 0101000710009 at 13",
			got[1].JMBG, got[1].Offset)
	}
}

func TestScan(t *testing.T) {
	var got []Match
	err := Scan(strings.NewReader("a 0710003730015 b"), func(m Match) { got = append(got, m) })
	if err != nil {
		t.Fatalf("Scan: unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].JMBG.String() != "0710003730015" || got[0].Offset != 2 {
		t.Errorf("matches = %v, want 0710003730015 at offset 2", got)
	}
}

func TestScanPropagatesReadError(t *testing.T) {
	readErr := errors.New("boom")
	err := Scan(iotest.ErrReader(readErr), func(Match) {})
	if !errors.Is(err, readErr) {
		t.Errorf("Scan error = %v, want %v", err, readErr)
	}
}

func TestScannerGzipCaveat(t *testing.T) {
	// The body has to be long enough that deflate actually compresses it.
	// Below roughly 262 bytes it emits a stored — uncompressed — block, and
	// the digits then survive verbatim in the "compressed" bytes, where the
	// scanner duly finds them. That is a real property of small gzip bodies,
	// documented in the README; it is not what this test is about.
	const repeats = 50
	body := strings.Repeat("JMBG: 0710003730015\n", repeats)

	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if compressed.Len() >= len(body) {
		t.Fatalf("body did not compress (%d bytes in, %d out): too small for "+
			"deflate to do anything, so the rest of this test proves nothing",
			len(body), compressed.Len())
	}

	// Raw compressed bytes hide the digits from the scanner.
	found := 0
	if err := Scan(bytes.NewReader(compressed.Bytes()), func(Match) { found++ }); err != nil {
		t.Fatal(err)
	}
	if found != 0 {
		t.Errorf("found %d JMBGs in compressed bytes, want 0", found)
	}

	// Decompressing first makes them visible again.
	zr, err := gzip.NewReader(bytes.NewReader(compressed.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	var got []Match
	if err := Scan(zr, func(m Match) { got = append(got, m) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != repeats {
		t.Fatalf("matches through gzip.Reader = %d, want %d", len(got), repeats)
	}
	for i, m := range got {
		if m.JMBG.String() != "0710003730015" {
			t.Errorf("match %d = %s, want 0710003730015", i, m.JMBG)
		}
	}
}

// FuzzScanner checks that chunking never changes the result: feeding a
// stream byte by byte must find exactly what a single write finds.
func FuzzScanner(f *testing.F) {
	seeds := []string{
		"JMBG: 0710003730015.",
		"0710003730015 and 0101000710009",
		"00710003730015",
		`{"jmbg":"0710003730015"}`,
		"no digits here",
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		var whole, split []Match
		s1 := NewScanner(func(m Match) { whole = append(whole, m) })
		s1.Write([]byte(input))
		s1.Close()
		s2 := NewScanner(func(m Match) { split = append(split, m) })
		for i := 0; i < len(input); i++ {
			s2.Write([]byte{input[i]})
		}
		s2.Close()
		if !reflect.DeepEqual(whole, split) {
			t.Errorf("chunking changed the result: whole = %v, split = %v", whole, split)
		}
	})
}
