package jmbg

import (
	"database/sql"
	"database/sql/driver"
	"encoding"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

var (
	_ encoding.TextMarshaler   = JMBG{}
	_ encoding.TextUnmarshaler = (*JMBG)(nil)
	_ driver.Valuer            = JMBG{}
	_ sql.Scanner              = (*JMBG)(nil)
)

// jsonUser exercises JMBG as a struct field. It is comparable, so
// round-trips can be asserted with ==.
type jsonUser struct {
	Name string `json:"name"`
	JMBG JMBG   `json:"jmbg"`
}

func TestJSONRoundTrip(t *testing.T) {
	want := jsonUser{Name: "Pera Perić", JMBG: mustParse(t, "0710003730015")}

	b, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal: unexpected error: %v", err)
	}
	const wantJSON = `{"name":"Pera Perić","jmbg":"0710003730015"}`
	if string(b) != wantJSON {
		t.Errorf("json.Marshal = %s, want %s", b, wantJSON)
	}

	var got jsonUser
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal: unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("round-trip = %+v, want %+v", got, want)
	}
}

func TestJSONUnmarshalInvalid(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{"bad checksum", `{"jmbg":"0710003730025"}`, ErrChecksum},
		{"too short", `{"jmbg":"123"}`, ErrLength},
		{"unknown region", `{"jmbg":"0710003600014"}`, ErrRegion},
		{"not digits", `{"jmbg":"01019907100ab"}`, ErrDigits},
		{"bad date", `{"jmbg":"3201990710009"}`, ErrDate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var u jsonUser
			err := json.Unmarshal([]byte(tt.input), &u)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("json.Unmarshal error = %v, want %v", err, tt.wantErr)
			}
			if u.JMBG != (JMBG{}) {
				t.Errorf("JMBG = %v, want the zero value after a failed unmarshal", u.JMBG)
			}
		})
	}
}

func TestJSONMapKey(t *testing.T) {
	j := mustParse(t, "0710003730015")

	b, err := json.Marshal(map[JMBG]string{j: "Pera"})
	if err != nil {
		t.Fatalf("json.Marshal: unexpected error: %v", err)
	}
	const wantJSON = `{"0710003730015":"Pera"}`
	if string(b) != wantJSON {
		t.Errorf("json.Marshal = %s, want %s", b, wantJSON)
	}

	var got map[JMBG]string
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal: unexpected error: %v", err)
	}
	if len(got) != 1 || got[j] != "Pera" {
		t.Errorf("round-trip = %v, want one entry keyed by %s", got, j)
	}
}

func TestMarshalTextZeroValue(t *testing.T) {
	b, err := JMBG{}.MarshalText()
	if !errors.Is(err, ErrZero) {
		t.Errorf("MarshalText error = %v, want ErrZero", err)
	}
	if b != nil {
		t.Errorf("MarshalText bytes = %q, want nil", b)
	}

	if _, err := json.Marshal(jsonUser{Name: "Pera"}); !errors.Is(err, ErrZero) {
		t.Errorf("json.Marshal of a zero JMBG: error = %v, want ErrZero", err)
	}
}

func TestErrZeroMentionsOptionalTypes(t *testing.T) {
	msg := ErrZero.Error()
	for _, want := range []string{"*JMBG", "sql.Null[JMBG]"} {
		if !strings.Contains(msg, want) {
			t.Errorf("ErrZero = %q, want it to mention %q", msg, want)
		}
	}
}

func TestUnmarshalText(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantErr error
		want    string
	}{
		{"valid", []byte("0710003730015"), nil, "0710003730015"},
		{"whitespace trimmed like Parse", []byte("  0710003730015  "), nil, "0710003730015"},
		{"nil", nil, ErrLength, ""},
		{"empty", []byte(""), ErrLength, ""},
		{"bad checksum", []byte("0710003730025"), ErrChecksum, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var j JMBG
			err := j.UnmarshalText(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("UnmarshalText error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if j != (JMBG{}) {
					t.Errorf("JMBG = %v, want the zero value after a failed unmarshal", j)
				}
				return
			}
			if j.String() != tt.want {
				t.Errorf("String() = %q, want %q", j.String(), tt.want)
			}
		})
	}
}

func TestValue(t *testing.T) {
	j := mustParse(t, "0710003730015")

	v, err := j.Value()
	if err != nil {
		t.Fatalf("Value: unexpected error: %v", err)
	}
	if v != driver.Value("0710003730015") {
		t.Errorf("Value() = %#v, want the 13-digit string", v)
	}
}

func TestValueZeroValue(t *testing.T) {
	v, err := JMBG{}.Value()
	if !errors.Is(err, ErrZero) {
		t.Errorf("Value error = %v, want ErrZero", err)
	}
	if v != nil {
		t.Errorf("Value() = %#v, want nil", v)
	}
}

func TestSQLScan(t *testing.T) {
	tests := []struct {
		name string
		src  any
	}{
		{"string", "0710003730015"},
		{"bytes", []byte("0710003730015")},
		{"string with whitespace", "  0710003730015  "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var j JMBG
			if err := j.Scan(tt.src); err != nil {
				t.Fatalf("Scan: unexpected error: %v", err)
			}
			if j.String() != "0710003730015" {
				t.Errorf("String() = %q, want %q", j.String(), "0710003730015")
			}
		})
	}
}

func TestSQLScanInvalidValue(t *testing.T) {
	tests := []struct {
		name    string
		src     any
		wantErr error
	}{
		{"string, bad checksum", "0710003730025", ErrChecksum},
		{"bytes, bad checksum", []byte("0710003730025"), ErrChecksum},
		{"string, too short", "123", ErrLength},
		{"bytes, unknown region", []byte("0710003600014"), ErrRegion},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var j JMBG
			err := j.Scan(tt.src)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Scan error = %v, want %v", err, tt.wantErr)
			}
			if j != (JMBG{}) {
				t.Errorf("JMBG = %v, want the zero value after a failed scan", j)
			}
		})
	}
}

func TestSQLScanUnsupportedType(t *testing.T) {
	tests := []struct {
		name string
		src  any
	}{
		{"nil (SQL NULL)", nil},
		{"int", 42},
		{"time", date(2000, 1, 1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var j JMBG
			err := j.Scan(tt.src)
			if err == nil {
				t.Fatal("Scan: expected an error, got nil")
			}
			if !strings.Contains(err.Error(), "sql.Null[JMBG]") {
				t.Errorf("Scan error = %q, want it to point at sql.Null[JMBG]", err)
			}
		})
	}
}

func TestSQLRoundTrip(t *testing.T) {
	want := mustParse(t, "0710003730015")

	v, err := want.Value()
	if err != nil {
		t.Fatalf("Value: unexpected error: %v", err)
	}

	var got JMBG
	if err := got.Scan(v); err != nil {
		t.Fatalf("Scan: unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("round-trip = %v, want %v", got, want)
	}
}

func TestSQLNull(t *testing.T) {
	want := mustParse(t, "0710003730015")

	t.Run("value, non-NULL", func(t *testing.T) {
		v, err := sql.Null[JMBG]{V: want, Valid: true}.Value()
		if err != nil {
			t.Fatalf("Value: unexpected error: %v", err)
		}
		if v != driver.Value("0710003730015") {
			t.Errorf("Value() = %#v, want the 13-digit string", v)
		}
	})

	// A NULL column never reaches JMBG.Value, so the zero-value error does
	// not fire — that is the whole point of using sql.Null here.
	t.Run("value, NULL", func(t *testing.T) {
		v, err := sql.Null[JMBG]{}.Value()
		if err != nil {
			t.Fatalf("Value: unexpected error: %v", err)
		}
		if v != nil {
			t.Errorf("Value() = %#v, want nil", v)
		}
	})

	t.Run("scan, non-NULL", func(t *testing.T) {
		var got sql.Null[JMBG]
		if err := got.Scan("0710003730015"); err != nil {
			t.Fatalf("Scan: unexpected error: %v", err)
		}
		if !got.Valid || got.V != want {
			t.Errorf("Scan = %+v, want {V:%v Valid:true}", got, want)
		}
	})

	t.Run("scan, NULL", func(t *testing.T) {
		var got sql.Null[JMBG]
		if err := got.Scan(nil); err != nil {
			t.Fatalf("Scan: unexpected error: %v", err)
		}
		if got.Valid || got.V != (JMBG{}) {
			t.Errorf("Scan = %+v, want {V:zero Valid:false}", got)
		}
	})

	t.Run("scan, invalid", func(t *testing.T) {
		var got sql.Null[JMBG]
		if err := got.Scan("0710003730025"); !errors.Is(err, ErrChecksum) {
			t.Errorf("Scan error = %v, want %v", err, ErrChecksum)
		}
	})
}
