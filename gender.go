package jmbg

// Gender is the gender encoded in a JMBG's unique number
// (000-499 male, 500-999 female).
type Gender int

// Gender values.
const (
	Male Gender = iota
	Female
)

// String returns a human-readable gender string.
func (g Gender) String() string {
	if g == Male {
		return "Male"
	}
	return "Female"
}
