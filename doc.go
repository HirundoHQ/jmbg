// Package jmbg validates and parses Serbian unique master citizen numbers
// (JMBG - Jedinstveni Matični Broj Građana).
//
// JMBG format: DDMMYYYRRBBBC
//
//   - DD  - Day of birth (01-31)
//   - MM  - Month of birth (01-12)
//   - YYY - Year of birth (last 3 digits)
//   - RR  - Region code
//   - BBB - Unique number (000-499 male, 500-999 female)
//   - C   - Checksum digit
//
// # Serialization
//
// JMBG round-trips losslessly through any encoder that honors
// encoding.TextMarshaler and encoding.TextUnmarshaler — encoding/json
// included, as a value and as a map key — and through database/sql via
// driver.Valuer and sql.Scanner. Every inbound value goes through Parse,
// so an invalid JMBG cannot enter the system, and the resulting error
// still wraps the Err* sentinels.
//
// The zero JMBG is not a valid value and fails to serialize rather than
// emitting an empty string that could never be parsed back: MarshalText
// and Value return ErrZero. Express optionality with *JMBG,
// database/sql.Null[JMBG], or — for JSON struct fields — the omitzero tag,
// which drops the zero value without ever calling MarshalText:
//
//	JMBG jmbg.JMBG `json:"jmbg,omitzero"`
//
// Note how far the failure reaches without one of those: marshaling is
// all-or-nothing, so one unset JMBG field fails the whole payload rather
// than its own key (omitempty does not help — JMBG is a struct). Under
// slog's JSONHandler the enclosing value logs as "!ERROR:json: error
// calling MarshalText ...", losing the rest of the record too.
//
// # Logging
//
// A JMBG is personal data. It implements slog.LogValuer, so log/slog-based
// loggers emit the masked form automatically:
//
//	logger.Info("user loaded", "jmbg", j) // jmbg=0710*******15
//
// That reaches only a JMBG passed as the attribute value itself. slog does
// not walk into composite values, so one nested in a struct, slice, or map
// is formatted with %+v (TextHandler) or marshaled through MarshalText
// (JSONHandler), and the full number reaches the log:
//
//	logger.Info("user loaded", "user", u) // user="{Name:Pera JMBG:0710003730015}"
//
// LogValue is the per-value boundary; the stream is the catch-all. Point
// the handler at a Scanner to be told which JMBGs reached the log, or at a
// Redactor to keep them out of it entirely — both catch the nested cases
// LogValue cannot see. slog serializes writes to its sink and ends each
// record with a newline, so both are safe there and never see a number
// split across records. See the NewScanner log-sink example.
//
// String is deliberately left unmasked — it returns the raw 13 digits,
// which the JSON, database, and Scanner round-trips all depend on. So
// fmt.Println(j), and %v or %s in a logger that does not understand
// slog.LogValuer, print the full number; call Masked explicitly there.
package jmbg
