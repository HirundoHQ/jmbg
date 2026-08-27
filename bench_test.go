package jmbg

import (
	"bytes"
	"fmt"
	"io"
	"math/rand/v2"
	"testing"
)

// payload repeats unit until it covers at least size bytes.
func payload(unit string, size int) []byte {
	return bytes.Repeat([]byte(unit), size/len(unit)+1)
}

// benchPayloads span the shapes that drive the digit-run state machine down
// different paths, from "never starts a run" to "every run is a JMBG".
var benchPayloads = []struct {
	name string
	data []byte
}{
	// No run of digits ever reaches 13, so the run buffer is barely touched.
	{"plain-log", payload(
		"level=INFO msg=\"request served\" path=/api/v1/policies status=200 dur=13ms\n", 1<<20)},
	// One JMBG per line: the realistic case for a leak tripwire.
	{"log-with-jmbg", payload(
		"level=INFO msg=\"user loaded\" id=4242 jmbg=0710003730015 region=Beograd\n", 1<<20)},
	// Every run is a valid JMBG: worst case for Parse.
	{"dense-jmbg", payload("0710003730015 ", 1<<20)},
	// 16-digit runs: every run is disqualified after the 14th digit.
	{"card-numbers", payload("4111111111111111 ", 1<<20)},
	// 13-digit identifiers that are almost never JMBGs — the realistic shape
	// for order or account numbers in a log. Every run reaches full length
	// and is handed to Parse, which rejects nearly all of them.
	{"13-digit-ids", thirteenDigitIDs(1 << 20)},
}

// thirteenDigitIDs builds space-separated 13-digit numerals from a fixed
// PCG seed, so the benchmark payload is identical run to run.
func thirteenDigitIDs(size int) []byte {
	rng := rand.New(rand.NewPCG(7, 7))
	var b []byte
	for len(b) < size {
		for range 13 {
			b = append(b, byte('0'+rng.IntN(10)))
		}
		b = append(b, ' ')
	}
	return b
}

func BenchmarkScanner(b *testing.B) {
	for _, p := range benchPayloads {
		b.Run(p.name, func(b *testing.B) {
			s := NewScanner(func(Match) {})
			b.SetBytes(int64(len(p.data)))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_, _ = s.Write(p.data)
				_ = s.Close()
			}
		})
	}
}

func BenchmarkRedactor(b *testing.B) {
	for _, p := range benchPayloads {
		b.Run(p.name, func(b *testing.B) {
			r := NewRedactor(io.Discard, nil)
			b.SetBytes(int64(len(p.data)))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_, _ = r.Write(p.data)
				_ = r.Close()
			}
		})
	}
}

// BenchmarkScannerChunked shows the cost of feeding the same payload in
// small fragments, which is how an http.ResponseWriter or a log sink
// actually delivers it.
func BenchmarkScannerChunked(b *testing.B) {
	data := payload("level=INFO jmbg=0710003730015 ok\n", 1<<20)
	for _, size := range []int{64, 512, 4096, 65536} {
		b.Run(fmt.Sprintf("%dB", size), func(b *testing.B) {
			s := NewScanner(func(Match) {})
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				for i := 0; i < len(data); i += size {
					_, _ = s.Write(data[i:min(i+size, len(data))])
				}
				_ = s.Close()
			}
		})
	}
}

// BenchmarkParse separates the two paths that matter: a valid JMBG, and the
// rejection a stream of ordinary 13-digit identifiers actually takes.
func BenchmarkParse(b *testing.B) {
	cases := []struct{ name, input string }{
		{"valid", "0710003730015"},
		{"invalid-date", "9999999999999"},
		{"invalid-region", "0710003660015"},
		{"invalid-checksum", "0710003730025"},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, _ = Parse(c.input)
			}
		})
	}
}

// BenchmarkPathological covers inputs shaped to stress the run machine
// rather than to look realistic: one enormous digit run that never ends,
// the densest possible stream of real JMBGs, and a giant run delivered one
// byte at a time.
func BenchmarkPathological(b *testing.B) {
	const size = 10 << 20

	oneHugeRun := bytes.Repeat([]byte{'7'}, size)
	denseJMBG := payload("0710003730015 ", size)

	b.Run("Scanner/one-10MiB-run", func(b *testing.B) {
		s := NewScanner(func(Match) {})
		b.SetBytes(int64(len(oneHugeRun)))
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = s.Write(oneHugeRun)
			_ = s.Close()
		}
	})

	b.Run("Redactor/one-10MiB-run", func(b *testing.B) {
		r := NewRedactor(io.Discard, nil)
		b.SetBytes(int64(len(oneHugeRun)))
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = r.Write(oneHugeRun)
			_ = r.Close()
		}
	})

	b.Run("Scanner/one-10MiB-run-bytewise", func(b *testing.B) {
		s := NewScanner(func(Match) {})
		b.SetBytes(int64(len(oneHugeRun)))
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			for i := range oneHugeRun {
				_, _ = s.Write(oneHugeRun[i : i+1])
			}
			_ = s.Close()
		}
	})

	b.Run("Redactor/dense-jmbg-10MiB", func(b *testing.B) {
		r := NewRedactor(io.Discard, nil)
		b.SetBytes(int64(len(denseJMBG)))
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = r.Write(denseJMBG)
			_ = r.Close()
		}
	})
}
