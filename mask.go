package jmbg

import "log/slog"

// Masked returns the JMBG with the middle digits hidden:
// "0710003730015" becomes "0710*******15", same length as the original.
// The zero value returns "".
//
// The visible day and month are what make log entries correlatable; the
// year, the region, and most of the unique number stay hidden. The trailing
// two digits are there because the shape is fixed at DDMM*******XC, not
// because they help: the last is the modulo-11 check digit, derivable from
// the rest. Treat a masked JMBG as pseudonymous, not anonymous.
func (j JMBG) Masked() string {
	if j.original == "" {
		return ""
	}
	return j.original[:4] + "*******" + j.original[11:]
}

// LogValue implements slog.LogValuer, returning the masked form so that
// log/slog-based loggers never print a full citizen number by accident.
// It reaches only a JMBG passed as the attribute value itself; see the
// package doc for what that misses and how to cover it.
func (j JMBG) LogValue() slog.Value {
	return slog.StringValue(j.Masked())
}
