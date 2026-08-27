package jmbg_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/osiguraj-lako/jmbg"
)

// Example shows the whole lifecycle of a JMBG: parsing it, round-tripping
// it through JSON, and logging it without leaking the number.
func Example() {
	type user struct {
		Name string    `json:"name"`
		JMBG jmbg.JMBG `json:"jmbg"`
	}

	j, err := jmbg.Parse("0710003730015")
	if err != nil {
		panic(err)
	}

	b, err := json.Marshal(user{Name: "Pera Perić", JMBG: j})
	if err != nil {
		panic(err)
	}
	fmt.Println(string(b))

	var back user
	if err := json.Unmarshal(b, &back); err != nil {
		panic(err)
	}
	fmt.Println("round-trips:", back.JMBG == j)

	// slog masks the JMBG through LogValue; the timestamp is dropped here
	// only to keep the example output deterministic.
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}))
	logger.Info("user loaded", "jmbg", back.JMBG)

	// Output:
	// {"name":"Pera Perić","jmbg":"0710003730015"}
	// round-trips: true
	// level=INFO msg="user loaded" jmbg=0710*******15
}

// ExampleNewScanner_logSink shows the catch-all for log leaks: a Scanner on
// the handler's sink sees every JMBG actually written, including the nested
// values LogValue cannot reach. See the package doc for why.
func ExampleNewScanner_logSink() {
	type user struct {
		Name string
		JMBG jmbg.JMBG
	}

	j, err := jmbg.Parse("0710003730015")
	if err != nil {
		panic(err)
	}

	// Report the finding masked — a leak alert must not leak again.
	scanner := jmbg.NewScanner(func(m jmbg.Match) {
		fmt.Println("LEAK:", m.JMBG.Masked())
	})
	defer scanner.Close()

	logger := slog.New(slog.NewTextHandler(io.MultiWriter(os.Stdout, scanner),
		&slog.HandlerOptions{ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		}}))

	logger.Info("masked path", "jmbg", j)
	logger.Info("nested path", "user", user{"Pera", j})

	// Output:
	// level=INFO msg="masked path" jmbg=0710*******15
	// level=INFO msg="nested path" user="{Name:Pera JMBG:0710003730015}"
	// LEAK: 0710*******15
}
