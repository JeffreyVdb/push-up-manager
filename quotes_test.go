package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestQuoteFallbackFilteringAndCache(t *testing.T) {
	var first, second atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/first" {
			first.Add(1)
			w.WriteHeader(429)
			return
		}
		second.Add(1)
		fmt.Fprint(w, `[{"q":"Make money and conquer the world.","a":"Business"},{"q":"Peace begins with one mindful breath.","a":"Test author"},{"q":"<b>Peace begins here</b>","a":"HTML"},{"q":"Peace begins with one mindful breath.","a":"Duplicate"}]`)
	}))
	defer upstream.Close()
	q := &quoteService{client: upstream.Client(), providers: []quoteProvider{{"QuoteSlate", upstream.URL + "/first", "https://github.com/Musheer360/QuoteSlate"}, {"ZenQuotes", upstream.URL + "/second", "https://zenquotes.io/"}}}
	for range 3 {
		quotes := q.pool(context.Background())
		if len(quotes) != 1 || quotes[0].Source != "ZenQuotes" || quotes[0].SourceURL != "https://zenquotes.io/" {
			t.Fatal(quotes)
		}
	}
	if first.Load() != 1 || second.Load() != 1 {
		t.Fatal("batch not cached")
	}
}

func TestQuoteFailuresBackOffAndRetainStalePool(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, `not json`) }))
	defer upstream.Close()
	q := &quoteService{client: upstream.Client(), providers: []quoteProvider{{"QuoteSlate", upstream.URL, ""}}}
	if len(q.pool(context.Background())) != 0 {
		t.Fatal("expected bundled fallback")
	}
	q.pool(context.Background())
	if calls.Load() != 1 {
		t.Fatal("failure not cached")
	}
	q.nextFetch = time.Time{}
	q.cached = []reflection{{Text: "A quiet moment of gratitude."}}
	if len(q.pool(context.Background())) != 1 {
		t.Fatal("stale usable pool discarded")
	}
}

func TestQuoteTimeoutFallsBack(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, `[{"q":"Return gently to this present moment.","a":"Test author"}]`)
	}))
	defer upstream.Close()
	q := &quoteService{client: &http.Client{Timeout: 50 * time.Millisecond}, providers: []quoteProvider{{"QuoteSlate", upstream.URL + "/slow", ""}, {"ZenQuotes", upstream.URL + "/ok", "https://zenquotes.io/"}}}
	if got := q.pool(context.Background()); len(got) != 1 {
		t.Fatal(got)
	}
}
