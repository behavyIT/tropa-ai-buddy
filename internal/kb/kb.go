// Package kb is a permission-aware knowledge base: test instructions from team
// packs plus knowledge cards published by mentors. Documents are filtered by
// ACL *before* ranking, so text a user may not see never reaches an answer or
// an LLM prompt.
package kb

import (
	"math"
	"sort"
	"sync"
	"time"

	"tropa/internal/catalog"
	"tropa/internal/directory"
	"tropa/internal/domain"
	"tropa/internal/policy"
	"tropa/internal/textutil"
)

// Source kinds.
const (
	KindInstruction = "test_instruction"
	KindMentor      = "mentor_answer"
)

// Chunk is a searchable section of a document or a knowledge card.
type Chunk struct {
	DocID      string
	Title      string
	Heading    string
	Text       string
	Kind       string
	Visibility string
	Teams      []string
	Roles      []string
	Author     string
	Date       time.Time
	terms      map[string]int
	titleTerms map[string]bool
	length     int
}

// Hit is a ranked search result.
type Hit struct {
	Chunk    Chunk
	Score    float64
	Coverage float64
}

// Index is an in-memory BM25 index.
type Index struct {
	mu     sync.RWMutex
	cat    *catalog.Catalog
	dir    *directory.Directory
	pol    *policy.Policy
	chunks []Chunk
	df     map[string]int
	avgLen float64
}

var synonyms = map[string]string{
	"k8s": "kubernetes", "кубер": "kubernetes", "кубернетес": "kubernetes", "бадд": "наставник", "бадди": "наставник",
	"ментор": "наставник", "наставниц": "наставник", "выходн": "отгул", "отпрос": "отгул", "отпроситьс": "отгул",
	"гитлаб": "gitlab", "графан": "grafana", "сентр": "sentry", "сентри": "sentry", "мр": "mr", "пр": "mr", "мерж": "mr",
	"стейдж": "staging", "стейджинг": "staging", "дейл": "дейли", "созвон": "встреч", "митинг": "встреч",
}

// Expand maps query terms to canonical synonyms (keeping the original).
func Expand(terms []string) []string {
	out := make([]string, 0, len(terms)*2)
	seen := map[string]bool{}
	for _, t := range terms {
		for _, x := range []string{t, synonyms[t]} {
			if x != "" && !seen[x] {
				seen[x] = true
				out = append(out, x)
			}
		}
	}
	return out
}

// New builds the index from catalog documents.
func New(cat *catalog.Catalog, dir *directory.Directory, pol *policy.Policy) *Index {
	ix := &Index{cat: cat, dir: dir, pol: pol}
	ix.Rebuild(nil)
	return ix
}

// Rebuild re-indexes catalog docs plus the given knowledge cards.
func (ix *Index) Rebuild(cards []domain.KnowledgeCard) {
	var chunks []Chunk
	for _, id := range ix.cat.DocOrder {
		d := ix.cat.Docs[id]
		for _, s := range d.Sections {
			chunks = append(chunks, makeChunk(Chunk{DocID: d.ID, Title: d.Title, Heading: s.Heading, Text: s.Text, Kind: KindInstruction,
				Visibility: d.Visibility, Teams: d.Teams, Roles: d.Roles}, d.Tags))
		}
	}
	for _, c := range cards {
		author := ""
		if p, ok := ix.dir.ByID(c.AuthorID); ok {
			author = p.Name
		}
		chunks = append(chunks, makeChunk(Chunk{DocID: "card:" + c.ID, Title: c.Title, Heading: c.Title, Text: c.Body, Kind: KindMentor,
			Visibility: "team", Teams: []string{c.TeamID}, Author: author, Date: c.CreatedAt}, nil))
	}
	df := map[string]int{}
	total := 0
	for _, c := range chunks {
		for t := range c.terms {
			df[t]++
		}
		total += c.length
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.chunks, ix.df = chunks, df
	ix.avgLen = 1
	if len(chunks) > 0 {
		ix.avgLen = float64(total) / float64(len(chunks))
	}
}

func makeChunk(c Chunk, tags []string) Chunk {
	c.terms = map[string]int{}
	c.titleTerms = map[string]bool{}
	for _, t := range Expand(textutil.Terms(c.Title)) {
		c.titleTerms[t] = true
	}
	add := func(s string, w int) {
		for _, t := range Expand(textutil.Terms(s)) {
			c.terms[t] += w
			c.length += w
		}
	}
	add(c.Title, 2)
	add(c.Heading, 2)
	add(c.Text, 1)
	for _, t := range tags {
		add(t, 2)
	}
	return c
}

// Visible reports whether the user may read a chunk.
func (ix *Index) Visible(u *directory.Person, c Chunk) bool {
	if c.Kind == KindMentor {
		return len(c.Teams) > 0 && ix.pol.CanSeeCard(u, c.Teams[0])
	}
	return ix.pol.CanSeeDoc(u, c.Visibility, c.Teams, c.Roles)
}

// Search ranks chunks visible to u. At most one chunk per document is returned.
func (ix *Index) Search(u *directory.Person, query string, k int) []Hit {
	q := Expand(textutil.Terms(query))
	if len(q) == 0 {
		return nil
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	n := float64(len(ix.chunks))
	const k1, b = 1.2, 0.75
	var hits []Hit
	for _, c := range ix.chunks {
		if !ix.Visible(u, c) {
			continue // ACL first: invisible text is never scored or returned
		}
		score, matched := 0.0, 0
		idfAll, idfTitle := 0.0, 0.0
		for _, t := range q {
			idf := math.Log(1 + (n-float64(ix.df[t])+0.5)/(float64(ix.df[t])+0.5))
			idfAll += idf
			tf := float64(c.terms[t])
			if tf == 0 {
				continue
			}
			matched++
			if c.titleTerms[t] {
				idfTitle += idf
			}
			score += idf * tf * (k1 + 1) / (tf + k1*(1-b+b*float64(c.length)/ix.avgLen))
		}
		if score == 0 {
			continue
		}
		// "aboutness": a document whose title contains the rare (informative) words of
		// the question is about it — weight by the IDF share of query terms in the title
		score *= 1 + 0.5*idfTitle/idfAll
		if len(c.Teams) > 0 && c.Teams[0] == u.TeamID {
			score *= 1.15 // prefer the user's own team context
		}
		if c.Kind == KindMentor {
			score *= 1.1
		}
		hits = append(hits, Hit{Chunk: c, Score: score, Coverage: float64(matched) / float64(len(q))})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	seen := map[string]bool{}
	out := make([]Hit, 0, k)
	for _, h := range hits {
		if seen[h.Chunk.DocID] {
			continue
		}
		seen[h.Chunk.DocID] = true
		out = append(out, h)
		if len(out) == k {
			break
		}
	}
	return out
}

// Answerable decides if the best hit is good enough to answer from.
func Answerable(hits []Hit) bool {
	return len(hits) > 0 && hits[0].Score >= 2.0 && hits[0].Coverage >= 0.34
}

// DocTitle returns the title of a catalog doc (or "").
func (ix *Index) DocTitle(id string) string {
	if d, ok := ix.cat.Docs[id]; ok {
		return d.Title
	}
	return ""
}

// DocText returns the full text of a catalog doc if visible to u.
func (ix *Index) DocText(u *directory.Person, id string) (title, text string, ok bool) {
	d, found := ix.cat.Docs[id]
	if !found || !ix.pol.CanSeeDoc(u, d.Visibility, d.Teams, d.Roles) {
		return "", "", false
	}
	for _, s := range d.Sections {
		text += "## " + s.Heading + "\n" + s.Text + "\n\n"
	}
	return d.Title, text, true
}
