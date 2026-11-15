// Package nlu is the deterministic "offline brain": intent detection and slot
// extraction (dates, systems, topics) for Russian. It works without any LLM,
// so the prototype is fully functional even when no API key is configured,
// and it is the fallback whenever the LLM is slow, failing or refusing.
package nlu

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"tropa/internal/catalog"
	"tropa/internal/clock"
	"tropa/internal/textutil"
)

// Intents.
const (
	IntentGreeting     = "greeting"
	IntentThanks       = "thanks"
	IntentNextStep     = "next_step"
	IntentAbsence      = "absence"
	IntentAccess       = "access"
	IntentWho          = "who"
	IntentAskMentor    = "ask_mentor"
	IntentTeachback    = "teachback"
	IntentStatus       = "requests_status"
	IntentCompleteTask = "complete_task"
	IntentSensitive    = "sensitive"
	IntentQuestion     = "question"
	SubHow             = "how"
	SubDo              = "do"
)

// AllIntents lists intents for prompts and schemas.
var AllIntents = []string{IntentGreeting, IntentThanks, IntentNextStep, IntentAbsence, IntentAccess, IntentWho, IntentAskMentor,
	IntentTeachback, IntentStatus, IntentCompleteTask, IntentSensitive, IntentQuestion}

// Understanding is the parsed meaning of a user message.
type Understanding struct {
	Intent     string     `json:"intent"`
	Sub        string     `json:"sub,omitempty"`
	DateFrom   *time.Time `json:"date_from,omitempty"`
	DateTo     *time.Time `json:"date_to,omitempty"`
	Systems    []string   `json:"systems,omitempty"`
	Topic      string     `json:"topic,omitempty"`
	Standard   bool       `json:"standard,omitempty"`
	Confidence float64    `json:"confidence"`
}

// Parser holds catalog-derived vocabularies.
type Parser struct {
	cat *catalog.Catalog
}

// New creates a parser.
func New(cat *catalog.Catalog) *Parser { return &Parser{cat: cat} }

func has(norm string, keys ...string) bool { return textutil.ContainsAny(norm, keys) }

var sensitiveKeys = []string{"зарплат", "оклад", "сколько получа", "сколько плат", "премия", "премии", "больничн", "диагноз", "медицин",
	"паспорт", "снилс", " инн", "номер карты", "банковск", "кредит", "переведи деньги", "перевести деньги", "увольн", "уволит",
	"домашний адрес", "личный телефон", "личный номер"}

// IsSensitive reports topics out of scope by the case (sensitive personal data, finance).
func IsSensitive(norm string) bool { return has(" "+norm, sensitiveKeys...) }

// Parse detects intent and slots.
func (p *Parser) Parse(text string, now time.Time) Understanding {
	norm := textutil.Normalize(text)
	u := Understanding{Intent: IntentQuestion, Confidence: 0.4}
	u.Systems = p.Systems(norm)
	u.DateFrom, u.DateTo = ParseDates(norm, now)
	topic := p.TeachTopic(norm)
	words := len(strings.Fields(norm))

	switch {
	case IsSensitive(norm):
		u.Intent, u.Confidence = IntentSensitive, 0.95
	case has(norm, "хочу рассказать", "расскажу", "давай расскажу", "объясню", "сдать", "проверь меня", "проверь мои знания", "проверь, как я понял", "teach") && topic != "":
		u.Intent, u.Topic, u.Confidence = IntentTeachback, topic, 0.9
	case has(norm, "хочу рассказать", "расскажу", "проверь меня", "сдать пункт", "teach-back", "teachback"):
		u.Intent, u.Confidence = IntentTeachback, 0.7
	case has(norm, "статус", "что с мо", "что там с", "одобрил", "согласовал", "мои заявки", "моих заявок", "где моя заявка", "заявка одобрен") &&
		has(norm, "заяв", "отгул", "доступ", "запрос"):
		u.Intent, u.Confidence = IntentStatus, 0.85
	case has(norm, "отгул", "выходной", "выходные дни", "отпрос", "отпуск на", "не выйду", "не смогу прийти", "day off", "взять день", "пропустить день"):
		u.Intent, u.Confidence = IntentAbsence, 0.9
		u.Sub = SubHow
		if u.DateFrom != nil || has(norm, "оформи", "хочу", "нужен отгул", "возьму", "запроси", "подай", "оформить на", "сделай заявку", "создай") && !has(norm, "как оформить", "как взять", "как подать") {
			u.Sub = SubDo
		}
	case has(norm, "я сделал", "я выполнил", "я закончил", "я завел", "готово", "выполнено", "отметь", "сделано", "я прошел", "я прочитал", "я изучил", "я настроил", "я добавил"):
		u.Intent, u.Confidence = IntentCompleteTask, 0.8
		u.Topic = norm
	case has(norm, "к кому", "кого спросить", "кому писать", "кому написать", "кто отвечает", "кто может помочь", "к кому обратиться", "кто такой", "кто такая", "кто мой", "кто моя", "кто наставник", "кто руководитель", "контакт"):
		u.Intent, u.Confidence = IntentWho, 0.85
		u.Topic = norm
	case has(norm, "доступ", "права на", "прав к", "выдай", "выдать прав") || (len(u.Systems) > 0 && has(norm, "нужен", "нужна", "нужно", "запроси", "дай ", "хочу", "получить", "подключи")):
		u.Intent, u.Confidence = IntentAccess, 0.85
		u.Standard = has(norm, "стандартн", "базов", "все доступы", "всё нужное", "все нужные", "набор")
		u.Sub = SubDo
		if has(norm, "как получить", "как запросить", "как подать", "где взять", "сколько ждать", "кто выдает", "кто согласует") {
			u.Sub = SubHow
		}
	case has(norm, "спроси наставника", "спроси у наставника", "передай наставнику", "вопрос наставнику", "спросить наставника", "спросить у наставника",
		"задать вопрос наставнику", "написать наставнику", "напиши наставнику", "спроси бадди", "спроси у бадди", "спросить бадди", "спросить у бадди"):
		u.Intent, u.Confidence = IntentAskMentor, 0.9
	case has(norm, "что дальше", "что делать", "чем заняться", "следующ", "с чего начать", "что сегодня", "план на", "мой план", "что мне делать",
		"какие задачи", "задачи на сегодня", "у меня задачи", "что на сегодня", "что осталось", "прогресс", "мой маршрут", "мои задачи", "что по плану", "на чем я") &&
		!has(norm, "если", "когда", "чтобы", "почему"): // «что делать, если…» — это вопрос, а не «что дальше»
		u.Intent, u.Confidence = IntentNextStep, 0.9
	case words <= 4 && regexp.MustCompile(`^(привет|здравств|добрый|доброе|хай|hello|hi|салют)`).MatchString(norm):
		u.Intent, u.Confidence = IntentGreeting, 0.9
	case words <= 6 && has(norm, "спасибо", "благодар", "супер, спасибо", "понял, спасибо"):
		u.Intent, u.Confidence = IntentThanks, 0.9
	}
	return u
}

// Systems extracts access-catalog systems mentioned in normalized text.
func (p *Parser) Systems(norm string) []string {
	toks := textutil.Tokens(norm)
	var out []string
	for _, s := range p.cat.Access {
		if matchAliases(toks, s.Aliases) {
			out = append(out, s.ID)
		}
	}
	return out
}

func matchAliases(toks, aliases []string) bool {
	for _, a := range aliases {
		prefix := strings.HasSuffix(a, "*")
		a = strings.TrimSuffix(a, "*")
		for _, t := range toks {
			if t == a || (prefix && strings.HasPrefix(t, a)) {
				return true
			}
		}
	}
	return false
}

// TeachTopic returns a teach-back rubric topic mentioned in the text.
func (p *Parser) TeachTopic(norm string) string {
	aliases := map[string][]string{"sentry": {"sentry", "сентри"}, "grafana": {"grafana", "графан*"}}
	toks := textutil.Tokens(norm)
	for topic, al := range aliases {
		if _, ok := p.cat.Rubrics[topic]; ok && matchAliases(toks, al) {
			return topic
		}
	}
	return ""
}

var months = map[string]time.Month{"январ": 1, "феврал": 2, "март": 3, "апрел": 4, "ма": 5, "июн": 6, "июл": 7, "август": 8, "сентябр": 9, "октябр": 10, "ноябр": 11, "декабр": 12}

const monthRe = `(январ[яе]|феврал[яе]|марта?|апрел[яе]|ма[яе]|июн[яе]|июл[яе]|августа?|сентябр[яе]|октябр[яе]|ноябр[яе]|декабр[яе])`

var (
	reRangeMonth = regexp.MustCompile(`(?:с\s+)?(\d{1,2})(?:-?го)?\s*(?:-|–|—|по|до)\s*(\d{1,2})(?:-?го)?\s+` + monthRe)
	reDayMonth   = regexp.MustCompile(`(\d{1,2})(?:-?го)?\s+` + monthRe)
	reNumeric    = regexp.MustCompile(`\b(\d{1,2})[./](\d{1,2})(?:[./](\d{2,4}))?\b`)
	reWeekday    = regexp.MustCompile(`(понедельник|вторник|сред[уа]|четверг|пятниц[уа]|суббот[уа]|воскресень[ея])`)
	weekdays     = map[string]time.Weekday{"понедельник": time.Monday, "вторник": time.Tuesday, "сред": time.Wednesday, "четверг": time.Thursday, "пятниц": time.Friday, "суббот": time.Saturday, "воскресень": time.Sunday}
)

func monthOf(s string) time.Month {
	for k, m := range months {
		if strings.HasPrefix(s, k) && (k != "ма" || len([]rune(s)) <= 3) {
			return m
		}
	}
	return 0
}

func mkDate(now time.Time, day int, month time.Month, year int) *time.Time {
	if day < 1 || day > 31 || month < 1 || month > 12 {
		return nil
	}
	if year == 0 {
		year = now.Year()
		cand := time.Date(year, month, day, 0, 0, 0, 0, clock.MSK)
		if cand.Before(clock.StartOfDay(now).AddDate(0, 0, -60)) {
			year++
		}
	}
	t := time.Date(year, month, day, 0, 0, 0, 0, clock.MSK)
	if t.Day() != day {
		return nil
	}
	return &t
}

// ParseDates extracts a date or a date range from normalized Russian text.
func ParseDates(norm string, now time.Time) (from, to *time.Time) {
	today := clock.StartOfDay(now)
	if m := reRangeMonth.FindStringSubmatch(norm); m != nil {
		d1, _ := strconv.Atoi(m[1])
		d2, _ := strconv.Atoi(m[2])
		mon := monthOf(m[3])
		return mkDate(now, d1, mon, 0), mkDate(now, d2, mon, 0)
	}
	var found []*time.Time
	for _, m := range reDayMonth.FindAllStringSubmatch(norm, 2) {
		d, _ := strconv.Atoi(m[1])
		if t := mkDate(now, d, monthOf(m[2]), 0); t != nil {
			found = append(found, t)
		}
	}
	if len(found) == 0 {
		for _, m := range reNumeric.FindAllStringSubmatch(norm, 2) {
			d, _ := strconv.Atoi(m[1])
			mo, _ := strconv.Atoi(m[2])
			y := 0
			if m[3] != "" {
				y, _ = strconv.Atoi(m[3])
				if y < 100 {
					y += 2000
				}
			}
			if t := mkDate(now, d, time.Month(mo), y); t != nil {
				found = append(found, t)
			}
		}
	}
	if len(found) == 0 {
		switch {
		case strings.Contains(norm, "послезавтра"):
			t := today.AddDate(0, 0, 2)
			found = append(found, &t)
		case strings.Contains(norm, "завтра"):
			t := today.AddDate(0, 0, 1)
			found = append(found, &t)
		case strings.Contains(norm, "сегодня"):
			t := today
			found = append(found, &t)
		}
	}
	if len(found) == 0 {
		for _, m := range reWeekday.FindAllStringSubmatch(norm, 2) {
			for k, wd := range weekdays {
				if strings.HasPrefix(m[1], k) {
					delta := (int(wd) - int(today.Weekday()) + 7) % 7
					if delta == 0 {
						delta = 7
					}
					t := today.AddDate(0, 0, delta)
					found = append(found, &t)
				}
			}
		}
	}
	switch len(found) {
	case 0:
		return nil, nil
	case 1:
		return found[0], found[0]
	}
	if found[1].Before(*found[0]) {
		return found[1], found[0]
	}
	return found[0], found[1]
}
