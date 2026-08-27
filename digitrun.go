package jmbg

// digitsInJMBG is the length of a JMBG in ASCII digits, and the single
// definition of how long a digit run must be to be a candidate.
const digitsInJMBG = 13

// isDigit reports whether c is an ASCII digit.
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// digitRun is the shared detection state machine behind Scanner and
// Redactor. It tracks the current maximal run of ASCII digits in a stream
// and recognizes the runs that are valid JMBGs.
//
// Both types drive it so that "what counts as a JMBG in a byte stream" is
// defined once and they cannot drift apart. FuzzRedactor uses Scanner as
// Redactor's oracle, so a divergence between them would be invisible.
type digitRun struct {
	digits [digitsInJMBG]byte // kept while the run may still be a JMBG
	length int                // digits seen, saturating just past len(digits)
	pos    int64              // bytes consumed from the stream so far
}

// The three states a run can be in: none in progress, exactly a JMBG's
// length and so worth validating, or grown past it — which rules it out
// entirely, since only maximal runs count.
func (r *digitRun) empty() bool    { return r.length == 0 }
func (r *digitRun) complete() bool { return r.length == len(r.digits) }
func (r *digitRun) overlong() bool { return r.length > len(r.digits) }

// push extends the run by one digit, keeping the digit only while the run
// could still be a JMBG.
//
// The counter saturates instead of running free: nothing needs the true
// length, and an unbounded one overflows a 32-bit int after 2 GiB of
// uninterrupted digits, at which point all three predicates read false at
// once and this method indexes digits negatively.
func (r *digitRun) push(c byte) {
	switch {
	case r.length < len(r.digits):
		r.digits[r.length] = c
		r.length++
	case r.length == len(r.digits):
		r.length++ // crosses into overlong exactly once
	}
}

// held returns the digits the run is still holding, and nothing once the run
// has outgrown a JMBG — a caller that was buffering them has released them
// by then.
func (r *digitRun) held() []byte {
	if r.overlong() {
		return nil
	}
	return r.digits[:r.length]
}

// reset abandons the run without examining it, for a caller that has
// already dealt with its digits.
func (r *digitRun) reset() { r.length = 0 }

// end closes the run, which stops just before the given stream offset, and
// reports the JMBG it held if it held one. The run is reset either way.
func (r *digitRun) end(offset int64) (Match, bool) {
	complete := r.complete()
	r.reset()
	if !complete {
		return Match{}, false
	}
	// Straight from the array: parseDigits builds no string and formats no
	// error unless these digits really are a JMBG.
	j, err := parseDigits(&r.digits, "")
	if err != nil {
		return Match{}, false
	}
	return Match{JMBG: j, Offset: offset - int64(len(r.digits))}, true
}
