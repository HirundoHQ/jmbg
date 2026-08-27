package jmbg

import (
	"bytes"
	"io"
	"runtime"
	"testing"
)

// countingWriter records how many bytes actually reached the destination.
type countingWriter struct{ n int64 }

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	return len(p), nil
}

// TestRedactorHoldsAtMostOneRun is the streaming proof: after every Write,
// everything except the digit run still in progress has already reached the
// destination. A buffering implementation — one that staged the payload and
// flushed at Close — would fail this immediately.
func TestRedactorHoldsAtMostOneRun(t *testing.T) {
	chunk := payload("user 0710003730015 order 4111111111111111 note ok\n", 64<<10)

	var dst countingWriter
	r := NewRedactor(&dst, nil)

	var sent int64
	for range 128 { // 8 MiB in 64 KiB writes
		n, err := r.Write(chunk)
		if err != nil {
			t.Fatalf("Write: unexpected error: %v", err)
		}
		sent += int64(n)

		if held := sent - dst.n; held > digitsInJMBG {
			t.Fatalf("after %d bytes written, only %d reached dst — %d held back, want at most %d",
				sent, dst.n, held, digitsInJMBG)
		}
	}

	if err := r.Close(); err != nil {
		t.Fatalf("Close: unexpected error: %v", err)
	}
	if dst.n != sent {
		t.Errorf("after Close dst has %d bytes, want %d (redaction preserves length)", dst.n, sent)
	}
}

// TestStreamingMemoryIsIndependentOfStreamSize is the leak proof: pushing a
// thousand times more data through must not allocate a thousand times more
// memory.
func TestStreamingMemoryIsIndependentOfStreamSize(t *testing.T) {
	// Text with no 13-digit run, so Parse is never reached and any
	// allocation would have to come from buffering.
	chunk := payload("level=INFO msg=\"served\" path=/v1/policies status=200\n", 64<<10)

	measure := func(t *testing.T, name string, totalBytes int, drive func(chunk []byte, reps int)) uint64 {
		t.Helper()
		reps := totalBytes / len(chunk)

		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)

		drive(chunk, reps)

		runtime.ReadMemStats(&after)
		allocated := after.TotalAlloc - before.TotalAlloc
		t.Logf("%s: %d MiB streamed, %d bytes allocated", name, totalBytes>>20, allocated)
		return allocated
	}

	drivers := map[string]func(chunk []byte, reps int){
		"Scanner": func(chunk []byte, reps int) {
			s := NewScanner(func(Match) {})
			for range reps {
				_, _ = s.Write(chunk)
			}
			_ = s.Close()
		},
		"Redactor": func(chunk []byte, reps int) {
			r := NewRedactor(io.Discard, nil)
			for range reps {
				_, _ = r.Write(chunk)
			}
			_ = r.Close()
		},
	}

	for name, drive := range drivers {
		t.Run(name, func(t *testing.T) {
			// 64 MiB, not more: the 64x small/large ratio is the proof, and
			// a bigger large leg only slows every test run (a 1 GiB leg cost
			// ~24s under -race for no extra evidence).
			small := measure(t, "1 MiB", 1<<20, drive)
			large := measure(t, "64 MiB", 64<<20, drive)

			// A buffering implementation would allocate proportionally. The
			// slack is for harness noise; the point is that 64x the data
			// must not mean 64x the memory.
			const ceiling = 64 << 10
			if large > ceiling {
				t.Errorf("streaming 64 MiB allocated %d bytes, want under %d — implementation is buffering",
					large, ceiling)
			}
			if small > ceiling {
				t.Errorf("streaming 1 MiB allocated %d bytes, want under %d", small, ceiling)
			}
		})
	}
}

// TestDoesNotRetainCallerBuffer proves neither type aliases the slice it was
// handed: scribbling over the caller's buffer after Write returns must not
// change what is eventually emitted, so a run held across a Write boundary
// has to be a copy.
func TestDoesNotRetainCallerBuffer(t *testing.T) {
	t.Run("Redactor", func(t *testing.T) {
		var dst bytes.Buffer
		r := NewRedactor(&dst, nil)

		buf := []byte("id=0710003730015") // the run is still held at return
		if _, err := r.Write(buf); err != nil {
			t.Fatalf("Write: unexpected error: %v", err)
		}
		for i := range buf {
			buf[i] = 'X'
		}
		if err := r.Close(); err != nil {
			t.Fatalf("Close: unexpected error: %v", err)
		}

		if want := "id=*************"; dst.String() != want {
			t.Errorf("dst = %q, want %q — the held run aliased the caller's buffer",
				dst.String(), want)
		}
	})

	t.Run("Scanner", func(t *testing.T) {
		var got []Match
		s := NewScanner(func(m Match) { got = append(got, m) })

		buf := []byte("id=0710003730015")
		if _, err := s.Write(buf); err != nil {
			t.Fatalf("Write: unexpected error: %v", err)
		}
		for i := range buf {
			buf[i] = 'X'
		}
		if err := s.Close(); err != nil {
			t.Fatalf("Close: unexpected error: %v", err)
		}

		if len(got) != 1 || got[0].JMBG.String() != "0710003730015" {
			t.Errorf("matches = %v, want one 0710003730015 — the run aliased the caller's buffer", got)
		}
	})
}

// TestDigitRunLengthSaturates guards an overflow only a very long stream
// reaches: on a 32-bit int an unbounded counter wraps negative after 2 GiB
// of uninterrupted digits, and push then indexes the array negatively.
func TestDigitRunLengthSaturates(t *testing.T) {
	var run digitRun
	for range 10000 {
		run.push('7')
	}

	if limit := len(run.digits) + 1; run.length > limit {
		t.Errorf("length = %d after 10000 digits, want it capped at %d", run.length, limit)
	}
	if !run.overlong() {
		t.Error("a 10000-digit run is not reported as overlong")
	}
	if run.held() != nil {
		t.Errorf("held() = %q, want nil for an overlong run", run.held())
	}

	// The saturated run must still behave: it is not a JMBG, and it resets.
	if _, ok := run.end(10000); ok {
		t.Error("an overlong run reported a match")
	}
	if !run.empty() {
		t.Error("end did not reset the run")
	}
}
