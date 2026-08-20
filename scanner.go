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
	found  func(Match)
	run    [13]byte // first bytes of the current digit run
	runLen int      // true length of the current run
	pos    int64    // bytes consumed so far
}

// NewScanner returns a Scanner that calls found for every valid JMBG.
func NewScanner(found func(Match)) *Scanner {
	return &Scanner{found: found}
}

// Write scans p, reporting any JMBG whose digit run ends within it. It
// always returns len(p) and a nil error.
func (s *Scanner) Write(p []byte) (int, error) {
	for i, c := range p {
		if c >= '0' && c <= '9' {
			if s.runLen < len(s.run) {
				s.run[s.runLen] = c
			}
			s.runLen++
		} else {
			s.endRun(s.pos + int64(i))
		}
	}
	s.pos += int64(len(p))
	return len(p), nil
}

// Close ends the stream, checking a digit run that touches the final byte.
// It always returns nil.
func (s *Scanner) Close() error {
	s.endRun(s.pos)
	return nil
}

// endRun terminates the current digit run, which ends just before the given
// stream offset.
func (s *Scanner) endRun(end int64) {
	if s.runLen == len(s.run) {
		if j, err := Parse(string(s.run[:])); err == nil {
			s.found(Match{JMBG: j, Offset: end - int64(len(s.run))})
		}
	}
	s.runLen = 0
}

// Scan reads r until EOF and calls found for every valid JMBG in the stream.
// It needs only constant memory regardless of stream size.
func Scan(r io.Reader, found func(Match)) error {
	s := NewScanner(found)
	if _, err := io.Copy(s, r); err != nil {
		return err
	}
	return s.Close()
}
