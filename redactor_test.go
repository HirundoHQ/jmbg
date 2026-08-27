package jmbg

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// redact runs input through a Redactor in chunks of the given size and
// returns everything dst received plus every match reported.
func redact(t *testing.T, input string, chunkSize int) (string, []Match) {
	t.Helper()
	var dst bytes.Buffer
	var got []Match
	r := NewRedactor(&dst, func(m Match) { got = append(got, m) })
	for i := 0; i < len(input); i += chunkSize {
		end := min(i+chunkSize, len(input))
		if _, err := r.Write([]byte(input[i:end])); err != nil {
			t.Fatalf("Write: unexpected error: %v", err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: unexpected error: %v", err)
	}
	return dst.String(), got
}

func TestRedactor(t *testing.T) {
	type match struct {
		jmbg   string
		offset int64
	}
	tests := []struct {
		name  string
		input string
		want  string
		found []match
	}{
		{
			"jmbg alone", "0710003730015", "*************",
			[]match{{"0710003730015", 0}},
		},
		{
			"embedded in text", "JMBG: 0710003730015.", "JMBG: *************.",
			[]match{{"0710003730015", 6}},
		},
		{
			"two jmbgs", "0710003730015 and 0101000710009",
			"************* and *************",
			[]match{{"0710003730015", 0}, {"0101000710009", 18}},
		},
		{
			"run touching end of stream", "id=0710003730015", "id=*************",
			[]match{{"0710003730015", 3}},
		},
		// A 14-digit run is not a maximal 13-digit run, so nothing is a JMBG.
		{"14-digit run", "00710003730015", "00710003730015", nil},
		{"16-digit card number", "4111111111111111", "4111111111111111", nil},
		{"12 digits", "071000373001", "071000373001", nil},
		{"invalid checksum", "0710003730025", "0710003730025", nil},
		{"unknown region", "0710003660015", "0710003660015", nil},
		// Offsets are byte offsets and non-ASCII bytes pass through as-is.
		{
			"multibyte text around", "Perić: 0710003730015 (Perić)",
			"Perić: ************* (Perić)",
			[]match{{"0710003730015", 8}},
		},
		{
			"json string value", `{"jmbg":"0710003730015"}`, `{"jmbg":"*************"}`,
			[]match{{"0710003730015", 9}},
		},
		{"empty input", "", "", nil},
		{"no digits", "no digits here", "no digits here", nil},
		{"digits only, short runs", "1 22 333", "1 22 333", nil},
	}

	for _, tt := range tests {
		// Named rather than numbered so each variant is addressable with
		// -run 'TestRedactor/<case>/<chunking>'.
		chunkings := []struct {
			name string
			size int
		}{
			{"bytewise", 1},
			{"chunked", 4},
			{"whole", max(len(tt.input), 1)},
		}

		for _, c := range chunkings {
			t.Run(tt.name+"/"+c.name, func(t *testing.T) {
				out, got := redact(t, tt.input, c.size)
				if out != tt.want {
					t.Errorf("output = %q, want %q", out, tt.want)
				}
				if len(out) != len(tt.input) {
					t.Errorf("len(output) = %d, want %d (redaction preserves length)",
						len(out), len(tt.input))
				}
				if len(got) != len(tt.found) {
					t.Fatalf("%d matches, want %d", len(got), len(tt.found))
				}
				for i, w := range tt.found {
					if got[i].JMBG.String() != w.jmbg || got[i].Offset != w.offset {
						t.Errorf("match %d = {%s %d}, want {%s %d}",
							i, got[i].JMBG, got[i].Offset, w.jmbg, w.offset)
					}
				}
			})
		}
	}
}

// TestRedactorHoldsRunUntilClose pins the contract that a digit run
// touching the end of the stream reaches dst only after Close.
func TestRedactorHoldsRunUntilClose(t *testing.T) {
	var dst bytes.Buffer
	r := NewRedactor(&dst, nil)

	if _, err := r.Write([]byte("id=0710003730015")); err != nil {
		t.Fatalf("Write: unexpected error: %v", err)
	}
	if got := dst.String(); got != "id=" {
		t.Errorf("before Close: dst = %q, want %q (run still held)", got, "id=")
	}

	if err := r.Close(); err != nil {
		t.Fatalf("Close: unexpected error: %v", err)
	}
	if got := dst.String(); got != "id=*************" {
		t.Errorf("after Close: dst = %q, want %q", got, "id=*************")
	}
}

func TestRedactorNilFound(t *testing.T) {
	var dst bytes.Buffer
	r := NewRedactor(&dst, nil)
	if _, err := r.Write([]byte("JMBG: 0710003730015.")); err != nil {
		t.Fatalf("Write: unexpected error: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: unexpected error: %v", err)
	}
	if got := dst.String(); got != "JMBG: *************." {
		t.Errorf("dst = %q, want %q", got, "JMBG: *************.")
	}
}

func TestRedactorReusableAfterClose(t *testing.T) {
	var dst bytes.Buffer
	var got []Match
	r := NewRedactor(&dst, func(m Match) { got = append(got, m) })

	for _, s := range []string{"a=0710003730015", "b=0101000710009"} {
		if _, err := r.Write([]byte(s)); err != nil {
			t.Fatalf("Write: unexpected error: %v", err)
		}
		if err := r.Close(); err != nil {
			t.Fatalf("Close: unexpected error: %v", err)
		}
	}

	if want := "a=*************b=*************"; dst.String() != want {
		t.Errorf("dst = %q, want %q", dst.String(), want)
	}
	// Offsets continue across Close, exactly like Scanner.
	if len(got) != 2 || got[0].Offset != 2 || got[1].Offset != 17 {
		t.Errorf("matches = %v, want offsets 2 and 17", got)
	}
}

// failWriter fails on the nth Write (1-based); earlier writes succeed.
type failWriter struct {
	n     int
	count int
	err   error
}

func (w *failWriter) Write(p []byte) (int, error) {
	w.count++
	if w.count == w.n {
		return 0, w.err
	}
	return len(p), nil
}

// halfWriter writes at most half of each slice, forcing writeAll to loop.
type halfWriter struct{ buf bytes.Buffer }

func (w *halfWriter) Write(p []byte) (int, error) {
	n := (len(p) + 1) / 2
	return w.buf.Write(p[:n])
}

// stuckWriter accepts nothing but reports no error — writeAll must not spin.
type stuckWriter struct{}

func (stuckWriter) Write([]byte) (int, error) { return 0, nil }

func TestRedactorPropagatesWriteError(t *testing.T) {
	boom := errors.New("boom")

	t.Run("from Write", func(t *testing.T) {
		r := NewRedactor(&failWriter{n: 1, err: boom}, nil)
		n, err := r.Write([]byte("JMBG: 0710003730015."))
		if !errors.Is(err, boom) {
			t.Errorf("Write error = %v, want %v", err, boom)
		}
		if n != 0 {
			t.Errorf("Write n = %d, want 0 — the very first dst.Write failed, so nothing reached dst", n)
		}
	})

	t.Run("from Close", func(t *testing.T) {
		// The first Write forwards "id=" and holds the run; Close flushes it.
		r := NewRedactor(&failWriter{n: 2, err: boom}, nil)
		if _, err := r.Write([]byte("id=0710003730015")); err != nil {
			t.Fatalf("Write: unexpected error: %v", err)
		}
		if err := r.Close(); !errors.Is(err, boom) {
			t.Errorf("Close error = %v, want %v", err, boom)
		}
	})

	// The 14th digit releases the held run mid-loop; that write can fail.
	t.Run("releasing a disqualified run", func(t *testing.T) {
		r := NewRedactor(&failWriter{n: 1, err: boom}, nil)
		if _, err := r.Write([]byte("00710003730015")); !errors.Is(err, boom) {
			t.Errorf("Write error = %v, want %v", err, boom)
		}
	})

	// A run resolved by a non-digit mid-loop writes the mask; so can that.
	t.Run("resolving a run mid-write", func(t *testing.T) {
		r := NewRedactor(&failWriter{n: 1, err: boom}, nil)
		if _, err := r.Write([]byte("0710003730015.")); !errors.Is(err, boom) {
			t.Errorf("Write error = %v, want %v", err, boom)
		}
	})
}

func TestRedactorHandlesShortWrites(t *testing.T) {
	var w halfWriter
	r := NewRedactor(&w, nil)

	const input = "JMBG: 0710003730015 and 0101000710009 done"
	if _, err := r.Write([]byte(input)); err != nil {
		t.Fatalf("Write: unexpected error: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: unexpected error: %v", err)
	}

	const want = "JMBG: ************* and ************* done"
	if got := w.buf.String(); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestRedactorStuckWriter(t *testing.T) {
	r := NewRedactor(stuckWriter{}, nil)
	if _, err := r.Write([]byte("hello")); !errors.Is(err, io.ErrShortWrite) {
		t.Errorf("Write error = %v, want io.ErrShortWrite", err)
	}
}

// FuzzRedactor checks two properties: chunking never changes the output,
// and the output is exactly the input with every Scanner-reported match
// replaced by the mask.
func FuzzRedactor(f *testing.F) {
	seeds := []string{
		"JMBG: 0710003730015.",
		"0710003730015 and 0101000710009",
		"00710003730015",
		"4111111111111111",
		`{"jmbg":"0710003730015"}`,
		"0710003730025",
		"Perić: 0710003730015",
		"no digits here",
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		var whole bytes.Buffer
		rw := NewRedactor(&whole, nil)
		if _, err := rw.Write([]byte(input)); err != nil {
			t.Fatalf("Write: unexpected error: %v", err)
		}
		if err := rw.Close(); err != nil {
			t.Fatalf("Close: unexpected error: %v", err)
		}

		var split bytes.Buffer
		rs := NewRedactor(&split, nil)
		for i := 0; i < len(input); i++ {
			if _, err := rs.Write([]byte{input[i]}); err != nil {
				t.Fatalf("Write: unexpected error: %v", err)
			}
		}
		if err := rs.Close(); err != nil {
			t.Fatalf("Close: unexpected error: %v", err)
		}

		if whole.String() != split.String() {
			t.Fatalf("chunking changed the output: whole = %q, split = %q",
				whole.String(), split.String())
		}

		// Independent oracle: Scanner says where the JMBGs are.
		want := []byte(input)
		if err := Scan(strings.NewReader(input), func(m Match) {
			copy(want[m.Offset:m.Offset+int64(len(redactionMask))], redactionMask)
		}); err != nil {
			t.Fatalf("Scan: unexpected error: %v", err)
		}
		if whole.String() != string(want) {
			t.Errorf("output = %q, want %q (input with Scanner's matches masked)",
				whole.String(), string(want))
		}
	})
}

// TestRedactorWriteReturnsFullCount pins the io.Writer contract. A Write
// whose bytes are all held back still consumed them, so it must report
// len(p) — otherwise io.Copy and io.MultiWriter abort with ErrShortWrite.
func TestRedactorWriteReturnsFullCount(t *testing.T) {
	var dst bytes.Buffer
	r := NewRedactor(&dst, nil)

	writes := [][]byte{
		[]byte("JMBG: "),
		[]byte(""),
		[]byte("0710003730015"), // entirely held back
		[]byte(" done"),
	}
	for _, p := range writes {
		n, err := r.Write(p)
		if err != nil {
			t.Fatalf("Write(%q): unexpected error: %v", p, err)
		}
		if n != len(p) {
			t.Errorf("Write(%q) = %d, want %d", p, n, len(p))
		}
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: unexpected error: %v", err)
	}
	if want := "JMBG: ************* done"; dst.String() != want {
		t.Errorf("dst = %q, want %q", dst.String(), want)
	}
}

func TestRedactorThroughIOCopy(t *testing.T) {
	const input = "JMBG: 0710003730015 and 0101000710009 done"

	var dst bytes.Buffer
	r := NewRedactor(&dst, nil)

	n, err := io.Copy(r, strings.NewReader(input))
	if err != nil {
		t.Fatalf("io.Copy: unexpected error: %v", err)
	}
	if n != int64(len(input)) {
		t.Errorf("io.Copy = %d bytes, want %d", n, len(input))
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: unexpected error: %v", err)
	}

	const want = "JMBG: ************* and ************* done"
	if dst.String() != want {
		t.Errorf("dst = %q, want %q", dst.String(), want)
	}
}

// TestRedactorThroughMultiWriter covers the tee shape the README suggests:
// one destination sees the redacted stream, another the raw bytes.
func TestRedactorThroughMultiWriter(t *testing.T) {
	var redacted, raw bytes.Buffer
	r := NewRedactor(&redacted, nil)

	const input = "JMBG: 0710003730015 done"
	if _, err := fmt.Fprint(io.MultiWriter(&raw, r), input); err != nil {
		t.Fatalf("Fprint: unexpected error: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: unexpected error: %v", err)
	}

	if want := "JMBG: ************* done"; redacted.String() != want {
		t.Errorf("redacted = %q, want %q", redacted.String(), want)
	}
	if raw.String() != input {
		t.Errorf("raw = %q, want the input unchanged", raw.String())
	}
}

// badCountWriter returns a fixed count with no error, however many bytes it
// was offered.
type badCountWriter struct{ n int }

func (w badCountWriter) Write([]byte) (int, error) { return w.n, nil }

// TestRedactorRejectsInvalidWriteCount pins that a dst lying about its
// write count surfaces as an error, not a slice-bounds panic — the same
// guard io.Copy has.
func TestRedactorRejectsInvalidWriteCount(t *testing.T) {
	for _, n := range []int{-1, 100} {
		r := NewRedactor(badCountWriter{n: n}, nil)
		if _, err := r.Write([]byte("hello")); !errors.Is(err, errInvalidWrite) {
			t.Errorf("n=%d: Write error = %v, want errInvalidWrite", n, err)
		}
	}
}

// acceptThenFail accepts the first accept bytes it is offered, then fails
// every subsequent call.
type acceptThenFail struct {
	accept int
	err    error
	buf    bytes.Buffer
}

func (w *acceptThenFail) Write(p []byte) (int, error) {
	if w.accept == 0 {
		return 0, w.err
	}
	n := min(w.accept, len(p))
	w.accept -= n
	w.buf.Write(p[:n])
	return n, nil
}

// TestRedactorReportsConsumedBytesOnError pins the io.Writer convention on
// the failure path: when dst dies after taking part of the stream, Write
// reports how much of p needs no resend instead of 0, so a retrying wrapper
// like bufio.Writer does not duplicate the prefix that already went out.
func TestRedactorReportsConsumedBytesOnError(t *testing.T) {
	boom := errors.New("boom")
	w := &acceptThenFail{accept: 4, err: boom}
	r := NewRedactor(w, nil)

	// "note=" is flushed when the digit run starts; dst takes 4 of its 5
	// bytes and then fails.
	n, err := r.Write([]byte("note=0710003730015"))
	if !errors.Is(err, boom) {
		t.Fatalf("Write error = %v, want %v", err, boom)
	}
	if n != 4 {
		t.Errorf("Write n = %d, want 4 (the bytes that reached dst)", n)
	}
	if got := w.buf.String(); got != "note" {
		t.Errorf("dst received %q, want %q", got, "note")
	}
}
