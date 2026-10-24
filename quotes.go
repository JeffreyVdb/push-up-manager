package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

type reflection struct {
	Text      string `json:"text"`
	Author    string `json:"author"`
	Source    string `json:"source"`
	SourceURL string `json:"source_url"`
}

type quoteProvider struct{ name, url, attribution string }

// No user data is sent upstream. One batch serves every account for a day.
var quoteProviders = []quoteProvider{
	{"QuoteSlate", "https://quoteslate.vercel.app/api/quotes/random?count=15&tags=mindfulness,gratitude,self-reflection&maxLength=220", "https://github.com/Musheer360/QuoteSlate"},
	{"ZenQuotes", "https://zenquotes.io/api/quotes", "https://zenquotes.io/"},
}

type quoteService struct {
	mu        sync.Mutex
	client    *http.Client
	providers []quoteProvider
	cached    []reflection
	nextFetch time.Time
}

func newQuoteService() *quoteService {
	return &quoteService{client: &http.Client{Timeout: 2500 * time.Millisecond}, providers: quoteProviders}
}

func suitableReflection(text string) bool {
	if len([]rune(text)) < 15 || len([]rune(text)) > 260 || !utf8.ValidString(text) || strings.ContainsAny(text, "<>") {
		return false
	}
	lower := strings.ToLower(text)
	for _, word := range []string{"suicide", "kill", "conquer", "dominate", "hustle", "money", "wealth", "millionaire", "winner", "loser"} {
		if strings.Contains(lower, word) {
			return false
		}
	}
	// Whole words avoid accepting, for example, 'mind' inside 'remind'.
	words := strings.FieldsFunc(lower, func(r rune) bool { return !unicode.IsLetter(r) })
	for _, word := range words {
		switch word {
		case "breath", "breathe", "stillness", "peace", "peaceful", "mindfulness", "mindful", "meditation", "gratitude", "grateful", "compassion", "kindness", "patience", "present", "awareness", "inner":
			return true
		}
	}
	return false
}

func (q *quoteService) fetch(ctx context.Context, p quoteProvider) []reflection {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/json")
	res, err := q.client.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil
	}
	var rows []struct {
		Quote  string `json:"quote"`
		Author string `json:"author"`
		Q      string `json:"q"`
		A      string `json:"a"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 128<<10)).Decode(&rows) != nil {
		return nil
	}
	out := []reflection{}
	seen := map[string]bool{}
	for _, row := range rows {
		text, author := row.Quote, row.Author
		if p.name == "ZenQuotes" {
			text, author = row.Q, row.A
		}
		text, author = strings.TrimSpace(text), strings.TrimSpace(author)
		key := strings.ToLower(strings.Join(strings.Fields(text), " "))
		if !suitableReflection(text) || author == "" || len(author) > 120 || strings.ContainsAny(author, "<>") || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, reflection{text, author, p.name, p.attribution})
		if len(out) == 15 {
			break
		}
	}
	return out
}

func (q *quoteService) pool(ctx context.Context) []reflection {
	q.mu.Lock()
	defer q.mu.Unlock()
	if time.Now().Before(q.nextFetch) {
		return q.cached
	}
	// Back off on empty/invalid/429 responses as well as network errors.
	q.nextFetch = time.Now().Add(time.Hour)
	for _, provider := range q.providers {
		if quotes := q.fetch(ctx, provider); len(quotes) > 0 {
			q.cached = quotes
			q.nextFetch = time.Now().Add(24 * time.Hour)
			break
		}
	}
	if q.cached == nil {
		return []reflection{}
	}
	return q.cached
}

func (s *server) handleQuotes(w http.ResponseWriter, r *http.Request, _ *session) {
	writeJSON(w, http.StatusOK, map[string]any{"quotes": s.quotes.pool(r.Context())})
}
