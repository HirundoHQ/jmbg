package jmbg

import (
	"bytes"
	"errors"
	"io"
)

// errInvalidWrite is returned when dst reports a write count outside the
// valid range — the same misbehavior io.Copy guards against.
var errInvalidWrite = errors.New("jmbg: writer returned an invalid write count")

// redactionMask replaces a valid JMBG. It is exactly as wide as the digits
// it replaces, so redaction preserves the length of the stream.
var redactionMask = bytes.Repeat([]byte{'*'}, digitsInJMBG)

// Redactor copies a byte stream through to a destination, replacing every
// valid JMBG it finds with 13 asterisks. Where Scanner reports a leak,
// Redactor prevents it: wrap a log file, an HTTP response body, or stdout
// and the number never reaches the sink.
//
// Detection, and the ASCII-encoding and single-goroutine caveats, are
// identical to Scanner's — including the false-positive rate, which here
// costs data rather than a spurious alert. About one in 1,200 random
// 13-digit numerals is a valid JMBG, so a 13-digit order ID is occasionally
// masked,
// and since redaction preserves length nothing downstream notices. Do not
// point a Redactor at a stream whose 13-digit numbers matter.
//
// Redactor implements io.WriteCloser. Writes may be split at any byte
// boundary; a run spanning writes is handled. Only the current digit run is
// held back — at most 13 bytes, with everything else forwarded straight from
// the caller's slice — but that run has not reached dst when Write returns.
// Close ends the stream and flushes it, and must be called; it does not
// close dst. After Close the Redactor may keep being used; offsets continue
// from where the stream left off.
//
// Unlike Scanner, Write and Close can fail: an error from dst is returned as
// is, and afterwards the Redactor makes no consistency promises — stop using
// it.
type Redactor struct {
	dst   io.Writer
	found func(Match)
	run   digitRun
}

// NewRedactor returns a Redactor that copies into dst, masking every valid
// JMBG. If found is non-nil it is called for each redacted JMBG, after the
// mask has been written, with Match.Offset relative to the input stream;
// because redaction preserves length, input and output offsets coincide.
func NewRedactor(dst io.Writer, found func(Match)) *Redactor {
	return &Redactor{dst: dst, found: found}
}

// Write redacts p and forwards it to dst. On success it returns len(p). If
// dst fails, it returns dst's error along with the number of leading bytes
// of p that need no resend — they reached dst or sit in the run buffer — so
// a retrying wrapper such as bufio.Writer does not duplicate the prefix
// that already went out. The count is best effort: a failure while emitting
// previously buffered digits cannot be attributed to positions in p, and
// after any error the Redactor makes no consistency promises.
func (r *Redactor) Write(p []byte) (int, error) {
	// span is the stretch of p that passes through untouched, held only
	// until something interrupts it so it can be written in one call. There
	// is deliberately no closure here: capturing span would force it out of
	// a register, and this loop runs per byte.
	//
	// The consumed count needs no bookkeeping. Every byte before index i is
	// either flushed or sitting in the run buffer, so at any failure the
	// prefix that needs no resend is simply i — or span+w when a span flush
	// itself fails partway.
	span := -1

	for i, c := range p {
		if isDigit(c) {
			switch {
			case r.run.empty():
				// A run starts here, so everything before it can go.
				if span >= 0 {
					w, err := r.writeAll(p[span:i])
					if err != nil {
						return r.consumed(span+w, err)
					}
					span = -1
				}
			case r.run.complete():
				// The next digit disqualifies the run: release the digits
				// it held and let the rest pass straight through.
				if _, err := r.writeAll(r.run.held()); err != nil {
					return r.consumed(i, err)
				}
				span = i
			case r.run.overlong():
				if span < 0 {
					span = i
				}
			}
			r.run.push(c)
			continue
		}

		switch {
		case r.run.overlong():
			// Its digits were already released above, so there is nothing
			// left to decide.
			r.run.reset()
		case !r.run.empty():
			if err := r.resolveRun(r.run.pos + int64(i)); err != nil {
				return r.consumed(i, err)
			}
		}
		if span < 0 {
			span = i
		}
	}

	if span >= 0 {
		w, err := r.writeAll(p[span:])
		if err != nil {
			return r.consumed(span+w, err)
		}
	}
	r.run.pos += int64(len(p))
	return len(p), nil
}

// consumed records that only the first n bytes of p were consumed before
// err stopped a Write, and returns both.
func (r *Redactor) consumed(n int, err error) (int, error) {
	r.run.pos += int64(n)
	return n, err
}

// Close ends the stream, flushing a digit run that touches the final byte.
// It does not close dst.
func (r *Redactor) Close() error {
	return r.resolveRun(r.run.pos)
}

// resolveRun ends the held digit run, which stops just before the given
// stream offset, and writes either its mask or the digits themselves.
func (r *Redactor) resolveRun(offset int64) error {
	// Snapshot before end resets the run: if it turns out not to be a JMBG,
	// these are the bytes that have to pass through unchanged.
	held := r.run.held()

	m, ok := r.run.end(offset)
	if !ok {
		_, err := r.writeAll(held)
		return err
	}
	if _, err := r.writeAll(redactionMask); err != nil {
		return err
	}
	// Report only once the mask is really out, so a leak alert never
	// describes bytes that never left the process.
	if r.found != nil {
		r.found(m)
	}
	return nil
}

// writeAll writes b to dst, looping over short writes, and reports how many
// bytes of b actually reached dst. The unsigned compare catches both a
// negative n and an overlong one in a single test, as io.Copy does — this
// runs once per dst.Write and the short-run path makes many of those.
func (r *Redactor) writeAll(b []byte) (int, error) {
	total := len(b)
	for len(b) > 0 {
		n, err := r.dst.Write(b)
		if uint(n) > uint(len(b)) {
			return total - len(b), errInvalidWrite
		}
		b = b[n:]
		if err != nil {
			return total - len(b), err
		}
		if n == 0 {
			return total - len(b), io.ErrShortWrite
		}
	}
	return total, nil
}
