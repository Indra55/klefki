package semantic

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type Field struct {
	Kind     string `json:"kind"`
	Raw      string `json:"raw"`
	Value    string `json:"value"`
	Currency string `json:"currency,omitempty"`
	Evidence string `json:"evidence"`
}

var dates = regexp.MustCompile(`\b(?:\d{4}-\d{2}-\d{2}|\d{1,2}/\d{1,2}/\d{4}|\d{1,2} (?:January|February|March|April|May|June|July|August|September|October|November|December) \d{4}|(?:January|February|March|April|May|June|July|August|September|October|November|December) \d{1,2},? \d{4})\b`)
var unambiguousAmount = regexp.MustCompile(`^(?:\d+|\d{1,3}(?:,\d{3})+|\d{1,2}(?:,\d{2})*,\d{3})(?:\.\d{1,2})?$`)
var amounts = regexp.MustCompile(`(?:\b(?:INR|USD|EUR|GBP)\s*|[₹$€£]\s*)\d[\d,]*(?:\.\d{1,2})?\b`)

func ValidFields(fields []Field) bool {
	if len(fields) > 256 {
		return false
	}
	for _, f := range fields {
		if (f.Kind != "date" && f.Kind != "amount") || f.Raw == "" || f.Evidence == "" {
			return false
		}
		for _, s := range []string{f.Raw, f.Value, f.Currency, f.Evidence} {
			if len(s) > 512 || !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
				return false
			}
		}
		if f.Kind == "date" && f.Currency != "" {
			return false
		}
		if f.Currency != "" && f.Currency != "INR" && f.Currency != "USD" && f.Currency != "EUR" && f.Currency != "GBP" {
			return false
		}
	}
	return true
}

func Fields(text string) []Field {
	out := []Field{}
	add := func(kind string, re *regexp.Regexp) {
		for _, loc := range re.FindAllStringIndex(text, 256-len(out)) {
			raw := text[loc[0]:loc[1]]
			start, end := max(0, loc[0]-40), min(len(text), loc[1]+40)
			for start < loc[0] && !utf8.RuneStart(text[start]) {
				start++
			}
			for end < len(text) && !utf8.RuneStart(text[end]) {
				end++
			}
			f := Field{Kind: kind, Raw: raw, Evidence: text[start:end]}
			if kind == "date" {
				for _, layout := range []string{"2006-01-02", "2 January 2006", "January 2, 2006", "January 2 2006"} {
					if d, e := time.Parse(layout, raw); e == nil {
						f.Value = d.Format("2006-01-02")
						break
					}
				}
			} else {
				for _, c := range []string{"INR", "USD", "EUR", "GBP", "₹", "€", "£"} {
					if strings.HasPrefix(raw, c) {
						f.Currency = map[string]string{"₹": "INR", "€": "EUR", "£": "GBP"}[c]
						if f.Currency == "" {
							f.Currency = c
						}
						number := strings.TrimSpace(strings.TrimPrefix(raw, c))
						if unambiguousAmount.MatchString(number) {
							f.Value = strings.ReplaceAll(number, ",", "")
						}
						break
					}
				}
			}
			out = append(out, f)
		}
	}
	add("date", dates)
	if len(out) < 256 {
		add("amount", amounts)
	}
	return out
}
