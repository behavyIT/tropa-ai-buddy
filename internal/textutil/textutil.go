// Package textutil holds the tiny Russian-aware text pipeline shared by search,
// intent detection, teach-back grading and question de-duplication.
package textutil

import (
	"strings"
	"unicode"
)

// Normalize lowercases, folds ё→е and collapses whitespace.
func Normalize(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "ё", "е")
	return strings.Join(strings.Fields(s), " ")
}

// Tokens splits normalized text into letter/digit tokens.
func Tokens(s string) []string {
	s = Normalize(s)
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

var endings = []string{
	"иями", "ями", "ами", "ией", "ием", "ого", "его", "ому", "ему", "ыми", "ими", "иях",
	"ешь", "ишь", "ах", "ях", "ой", "ей", "ий", "ый", "ая", "яя", "ое", "ее", "ые", "ие",
	"ую", "юю", "ом", "ем", "ам", "ям", "ов", "ев", "ть", "ся", "сь", "ет", "ют", "ут",
	"ит", "ат", "ят", "им", "ым", "их", "ых", "у", "ю", "а", "я", "о", "е", "ы", "и", "ь", "й",
}

// Stem strips a single common Russian inflection ending (light stemmer).
func Stem(tok string) string {
	r := []rune(tok)
	if len(r) <= 3 || !isCyrillic(r[0]) {
		return tok
	}
	for _, e := range endings {
		er := []rune(e)
		if len(r)-len(er) >= 3 && strings.HasSuffix(tok, e) {
			return string(r[:len(r)-len(er)])
		}
	}
	return tok
}

func isCyrillic(r rune) bool { return r >= 'а' && r <= 'я' }

var stop = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`и в во не что он на я с со как а то все она так его но да ты к у же вы за бы по только ее мне было вот от меня еще нет о из ему теперь когда даже ну ли если уже или ни быть был него до вас нибудь опять уж вам ведь там потом себя ничего ей может они тут где есть надо ней для мы тебя их чем была сам чтоб без будто чего раз тоже себе под будет ж тогда кто этот того потому этого какой совсем ним здесь этом один почти мой тем чтобы нее сейчас были куда зачем всех никогда можно при наконец об другой хоть после над больше тот через эти нас про всего них какая много разве эту моя впрочем хорошо свою этой перед иногда лучше чуть том нельзя такой им более всегда конечно всю между это привет пожалуйста подскажи скажи какие нужно нужен нужна хочу мне мою моего мои мой твой ли бы какое каким как-то такое такие такая такую таких почему значит делать стоит the a an to of is are`) {
		stop[w] = true
	}
}

// IsStopword reports whether a normalized token carries no meaning for search.
func IsStopword(tok string) bool { return stop[tok] }

// Terms returns stemmed, stopword-free terms of the text.
func Terms(s string) []string {
	toks := Tokens(s)
	out := make([]string, 0, len(toks))
	for _, t := range toks {
		if stop[t] || len([]rune(t)) < 2 {
			continue
		}
		out = append(out, Stem(t))
	}
	return out
}

// ContainsAny reports whether normalized text contains any of the keys
// (keys are matched as substrings, so stems like "доступ" match "доступы").
func ContainsAny(norm string, keys []string) bool {
	for _, k := range keys {
		if k != "" && strings.Contains(norm, k) {
			return true
		}
	}
	return false
}

// Jaccard similarity between two term sets.
func Jaccard(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	sa := map[string]bool{}
	for _, x := range a {
		sa[x] = true
	}
	sb := map[string]bool{}
	for _, x := range b {
		sb[x] = true
	}
	inter := 0
	for x := range sa {
		if sb[x] {
			inter++
		}
	}
	return float64(inter) / float64(len(sa)+len(sb)-inter)
}

// Truncate cuts s to at most n runes, appending an ellipsis when cut.
func Truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	cut := string(r[:n])
	if i := strings.LastIndexAny(cut, ".!?\n"); i > n/2 {
		return cut[:i+1]
	}
	return strings.TrimSpace(cut) + "…"
}
