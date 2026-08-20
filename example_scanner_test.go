package jmbg_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/osiguraj-lako/jmbg"
)

func ExampleScan() {
	text := strings.NewReader("customer 42, JMBG: 0710003730015, active")
	_ = jmbg.Scan(text, func(m jmbg.Match) {
		fmt.Printf("found %s (born %s) at byte %d\n",
			m.JMBG, m.JMBG.BirthDate().Format("2006-01-02"), m.Offset)
	})
	// Output: found 0710003730015 (born 2003-10-07) at byte 19
}

// jmbgTee tees everything written to an http.ResponseWriter through a
// jmbg.Scanner, so response bodies are checked as they are sent.
type jmbgTee struct {
	http.ResponseWriter
	scanner *jmbg.Scanner
}

func (t *jmbgTee) Write(p []byte) (int, error) {
	t.scanner.Write(p) // never fails; scanning must not break the response
	return t.ResponseWriter.Write(p)
}

// DetectJMBGLeaks wraps a handler and reports every valid JMBG that appears
// in a response body — a data-loss-prevention tripwire for APIs that must
// never expose citizen numbers. Wrap it inside any compression middleware so
// it sees uncompressed bytes.
func DetectJMBGLeaks(next http.Handler, report func(*http.Request, jmbg.Match)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scanner := jmbg.NewScanner(func(m jmbg.Match) { report(r, m) })
		defer scanner.Close() // check a digit run touching the body's end
		next.ServeHTTP(&jmbgTee{ResponseWriter: w, scanner: scanner}, r)
	})
}

func ExampleScanner_httpMiddleware() {
	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"name":"Pera Perić","jmbg":"0101000710009"}`)
	})
	handler := DetectJMBGLeaks(api, func(r *http.Request, m jmbg.Match) {
		fmt.Printf("%s %s leaked JMBG %s at offset %d\n", r.Method, r.URL.Path, m.JMBG, m.Offset)
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/users/1", nil))
	// Output: GET /users/1 leaked JMBG 0101000710009 at offset 30
}
