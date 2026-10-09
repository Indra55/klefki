package search

import (
	"sort"
	"strings"
	"unicode"
	"vault/core/semantic"
)

// Query-only filtering keeps the encrypted index format and document terms intact.
var queryStopWords = func() map[string]bool {
	words := map[string]bool{}
	for _, word := range strings.Fields("a an and are as at be been being but by can could did do does for from had has have how i if in into is it its me my of on or our please show than that the their them there these they this those to was we were what when where which who why will with would you your") {
		words[word] = true
	}
	return words
}()

func relevantQueryTerms(terms map[string]int) []string {
	out := []string{}
	for term := range terms {
		if !queryStopWords[term] {
			out = append(out, term)
		}
	}
	sort.Strings(out)
	return out
}

func keywordEvidence(terms []string, document map[string]int) bool {
	matched := 0
	for _, term := range terms {
		if document[term] == 0 {
			continue
		}
		// Exact numbers and identifiers remain useful even inside long questions.
		if strings.IndexFunc(term, unicode.IsNumber) >= 0 {
			return true
		}
		matched++
	}
	minimum := 1
	if len(terms) >= 4 {
		minimum = min(3, (len(terms)+2)/3)
	}
	return matched >= minimum
}

func relevantSemanticHits(hits []Hit) []Hit {
	rank(hits)
	if len(hits) == 0 {
		return hits
	}
	// RRF discards original scores, so reject weak semantic evidence before fusion.
	m := semantic.ModelManifest()
	cutoff := max(m.SearchCutoff, hits[0].Score*m.SearchRelative)
	end := 0
	for end < len(hits) && hits[end].Score >= cutoff {
		end++
	}
	return hits[:end]
}
