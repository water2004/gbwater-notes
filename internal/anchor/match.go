package anchor

import (
	"crypto/sha256"
	"encoding/hex"
)

type Query struct {
	quote  string
	prefix string
	suffix string
}

func NewQuery(quote, prefix, suffix string) Query {
	return Query{quote: Normalize(quote), prefix: Normalize(prefix), suffix: Normalize(suffix)}
}

func (q Query) Empty() bool { return q.quote == "" }

func (q Query) Key() string {
	digest := sha256.Sum256([]byte(q.quote + "\x00" + q.prefix + "\x00" + q.suffix))
	return hex.EncodeToString(digest[:])
}

// Range is a half-open character range in normalized article text.
type Range struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type alignment struct {
	distance  int
	start     int
	context   int
	ambiguous bool
}

func bestAlignment(a, b alignment) alignment {
	if b.distance < a.distance || (b.distance == a.distance && b.context > a.context) {
		return b
	}
	if b.distance == a.distance && b.context == a.context {
		a.ambiguous = a.ambiguous || b.ambiguous || a.start != b.start
	}
	return a
}

func contextBefore(text, prefix []rune, start int) int {
	count := 0
	for count < len(prefix) && count < start && text[start-1-count] == prefix[len(prefix)-1-count] {
		count++
	}
	return count
}

func contextAfter(text, suffix []rune, end int) int {
	count := 0
	for count < len(suffix) && end+count < len(text) && text[end+count] == suffix[count] {
		count++
	}
	return count
}

// Match aligns the whole quote against the best contiguous interval of the
// article. Insertions, deletions and substitutions each cost one edit. The
// edit budget also bounds length change, since every extra or missing rune
// requires an edit. Context distinguishes equally good matches.
func Match(text []rune, query Query) (Range, bool) {
	quote := []rune(query.quote)
	if len(quote) == 0 {
		return Range{}, false
	}
	prefix, suffix := []rune(query.prefix), []rune(query.suffix)
	budget := len(quote) / 4
	if len(text) < len(quote)-budget {
		return Range{}, false
	}
	previous, current := make([]alignment, len(quote)+1), make([]alignment, len(quote)+1)
	for i := range previous {
		previous[i].distance = i
	}
	best := alignment{distance: budget + 1}
	bestEnd := 0
	for i, character := range text {
		end := i + 1
		current[0] = alignment{start: end, context: contextBefore(text, prefix, end)}
		for j, original := range quote {
			diagonal, insertion, deletion := previous[j], previous[j+1], current[j]
			if original != character {
				diagonal.distance++
			}
			insertion.distance++
			deletion.distance++
			current[j+1] = bestAlignment(bestAlignment(diagonal, insertion), deletion)
		}
		candidate := current[len(quote)]
		if candidate.distance <= budget && candidate.start < end {
			candidate.context += contextAfter(text, suffix, end)
			switch {
			case candidate.distance < best.distance || (candidate.distance == best.distance && candidate.context > best.context):
				best, bestEnd = candidate, end
			case candidate.distance == best.distance && candidate.context == best.context:
				best.ambiguous = true
			}
		}
		previous, current = current, previous
	}
	if bestEnd == 0 || best.ambiguous {
		return Range{}, false
	}
	return Range{Start: best.start, End: bestEnd}, true
}
