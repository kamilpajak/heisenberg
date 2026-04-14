package semcluster

import (
	"regexp"
	"strings"
	"sync"
)

// antonymPairs lists word pairs whose presence on opposite sides of a
// potential merge vetoes the merge. Embedding models conflate polarity
// ("test passed" vs "test failed" can score above 0.85) so we gate on
// explicit antonymy.
//
// Left-side tokens are positive/success outcomes; right-side tokens are
// negative/failure outcomes. Order doesn't matter for the veto — we check
// both directions.
var antonymPairs = [][2]string{
	{"pass", "fail"},
	{"passed", "failed"},
	{"succeed", "fail"},
	{"succeeded", "failed"},
	{"complete", "fail"},
	{"completed", "failed"},
	{"ok", "error"},
	{"accepted", "refused"},
	{"accepted", "rejected"},
	{"allowed", "denied"},
	{"found", "missing"},
	{"up", "down"},
}

var antonymRegexOnce sync.Once
var antonymRegex map[string]*regexp.Regexp

func antonymWordRegex(word string) *regexp.Regexp {
	antonymRegexOnce.Do(func() {
		antonymRegex = make(map[string]*regexp.Regexp, len(antonymPairs)*2)
	})
	if r, ok := antonymRegex[word]; ok {
		return r
	}
	r := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(word) + `\b`)
	antonymRegex[word] = r
	return r
}

// antonymGuardrail returns true if a and b contain opposite-polarity
// keywords from antonymPairs, blocking a candidate semantic merge.
func antonymGuardrail(a, b string) bool {
	for _, pair := range antonymPairs {
		pos, neg := pair[0], pair[1]
		if hasWord(a, pos) && hasWord(b, neg) {
			return true
		}
		if hasWord(a, neg) && hasWord(b, pos) {
			return true
		}
	}
	return false
}

func hasWord(s, word string) bool {
	if !strings.Contains(strings.ToLower(s), strings.ToLower(word)) {
		return false
	}
	return antonymWordRegex(word).MatchString(s)
}
