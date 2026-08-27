package jmbg

import "io"

// Match is a valid JMBG found in a byte stream.
type Match struct {
	JMBG   JMBG
	Offset int64 // byte offset of the JMBG's first digit in the stream
}

// Scanner finds valid JMBGs in a byte stream fed to it incrementally. It
// considers maximal runs of exactly 13 ASCII digits — bounded by non-digit
// bytes or the ends of the stream — and reports those that Parse accepts.
// Digit runs longer than 13 are never searched for embedded JMBGs, so a
// 16-digit card number reports nothing.
//
// Scanner implements io.WriteCloser: feed bytes with Write in fragments of
// any size (runs split across writes are handled), and call Close to end the
// stream so a run touching the final byte is checked too. Write and Close
// never fail. After Close the Scanner may keep being used; offsets continue
// from where the stream left off.
//
// The scanner assumes an ASCII-compatible encoding such as UTF-8, and sees
// only the bytes it is given: decompress (e.g. gzip) and extract text (e.g.
// from PDFs) before scanning.
//
// A Scanner must not be used from multiple goroutines concurrently.
type Scanner struct {
	found func(Match)
	run   digitRun
}

// NewScanner returns a Scanner that calls found for every valid JMBG. found
// must not be nil: the nil call would surface only on the first JMBG in the
// stream, which is to say in production.
func NewScanner(found func(Match)) *Scanner {
	return &Scanner{found: found}
}

// Write scans p, reporting any JMBG whose digit run ends within it. It
// always returns len(p) and a nil error.
func (s *Scanner) Write(p []byte) (int, error) {
	for i, c := range p {
		if isDigit(c) {
			s.run.push(c)
			continue
		}
		// Most bytes are non-digits with no run in progress. Keep that path
		// to inlined comparisons — end returns a Match by value, far too
		// heavy to pay per byte.
		if s.run.empty() {
			continue
		}
		if !s.run.complete() {
			s.run.reset()
			continue
		}
		if m, ok := s.run.end(s.run.pos + int64(i)); ok {
			s.found(m)
		}
	}
	s.run.pos += int64(len(p))
	return len(p), nil
}

// Close ends the stream, checking a digit run that touches the final byte.
// It always returns nil.
func (s *Scanner) Close() error {
	if m, ok := s.run.end(s.run.pos); ok {
		s.found(m)
	}
	return nil
}

// Scan reads r until EOF and calls found for every valid JMBG in the stream.
// It needs only constant memory regardless of stream size. found must not be
// nil, as for NewScanner.
func Scan(r io.Reader, found func(Match)) error {
	s := NewScanner(found)
	if _, err := io.Copy(s, r); err != nil {
		return err
	}
	return s.Close()
}
