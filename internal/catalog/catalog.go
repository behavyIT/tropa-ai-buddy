// Package catalog loads and validates the embedded onboarding content:
// routes, team packs, knowledge-base documents, access catalog and rubrics.
package catalog

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Task is one onboarding step in a route template.
type Task struct {
	ID           string   `yaml:"id" json:"id"`
	Title        string   `yaml:"title" json:"title"`
	Owner        string   `yaml:"owner" json:"owner"` // intern | mentor | lead
	Kind         string   `yaml:"kind" json:"kind"`   // manual | access | teachback
	DueDay       int      `yaml:"due_day" json:"due_day"`
	DependsOn    []string `yaml:"depends_on" json:"depends_on"`
	Systems      []string `yaml:"systems" json:"systems,omitempty"`
	Topic        string   `yaml:"topic" json:"topic,omitempty"`
	Doc          string   `yaml:"doc" json:"doc,omitempty"`
	ChecklistRef string   `yaml:"checklist_ref" json:"checklist_ref,omitempty"`
}

// Stage groups tasks of a period.
type Stage struct {
	ID      string `yaml:"id" json:"id"`
	Title   string `yaml:"title" json:"title"`
	FromDay int    `yaml:"from_day" json:"from_day"`
	ToDay   int    `yaml:"to_day" json:"to_day"`
	Tasks   []Task `yaml:"tasks" json:"tasks"`
}

// Route is an onboarding route template for a track.
type Route struct {
	ID     string  `yaml:"id"`
	Title  string  `yaml:"title"`
	Stages []Stage `yaml:"stages"`
}

// Task returns a task by id.
func (r *Route) Task(id string) (Task, bool) {
	for _, s := range r.Stages {
		for _, t := range s.Tasks {
			if t.ID == id {
				return t, true
			}
		}
	}
	return Task{}, false
}

// AllTasks returns tasks in route order.
func (r *Route) AllTasks() []Task {
	var out []Task
	for _, s := range r.Stages {
		out = append(out, s.Tasks...)
	}
	return out
}

// Contact is a "who to ask" entry of a team pack.
type Contact struct {
	Topic    string   `yaml:"topic"`
	Keywords []string `yaml:"keywords"`
	Person   string   `yaml:"person"`
	Channel  string   `yaml:"channel"`
	How      string   `yaml:"how"`
}

// CalendarEvent is a team event used for absence conflict checks.
type CalendarEvent struct {
	Date       string `yaml:"date" json:"date"`
	Time       string `yaml:"time" json:"time"`
	Title      string `yaml:"title" json:"title"`
	Importance string `yaml:"importance" json:"importance"`
	Note       string `yaml:"note" json:"note"`
}

// Team is a team pack: onboarding-as-code maintained by the team itself.
type Team struct {
	ID       string          `yaml:"id"`
	Name     string          `yaml:"name"`
	Route    string          `yaml:"route"`
	Contacts []Contact       `yaml:"contacts"`
	Calendar []CalendarEvent `yaml:"calendar"`
}

// AccessSystem is an entry of the access-service catalog.
type AccessSystem struct {
	ID           string   `yaml:"id" json:"id"`
	Title        string   `yaml:"title" json:"title"`
	Level        string   `yaml:"level" json:"level"`
	Approval     string   `yaml:"approval" json:"approval"` // auto | lead
	Standard     bool     `yaml:"standard" json:"standard"`
	ForbiddenFor []string `yaml:"forbidden_for" json:"forbidden_for,omitempty"`
	Aliases      []string `yaml:"aliases" json:"-"`
}

// Concept is one rubric point of a teach-back.
type Concept struct {
	ID       string   `yaml:"id" json:"id"`
	Label    string   `yaml:"label" json:"label"`
	Keywords []string `yaml:"keywords" json:"-"`
	Hint     string   `yaml:"hint" json:"hint"`
}

// Rubric describes how to grade a teach-back ("Расскажи о …").
type Rubric struct {
	Topic     string    `yaml:"topic"`
	Title     string    `yaml:"title"`
	Doc       string    `yaml:"doc"`
	PassRatio float64   `yaml:"pass_ratio"`
	Prompt    string    `yaml:"prompt"`
	Concepts  []Concept `yaml:"concepts"`
}

// Section is a heading-delimited chunk of a document.
type Section struct {
	Heading string
	Text    string
}

// Doc is a knowledge-base document (test instruction) with its ACL.
type Doc struct {
	ID         string   `yaml:"id"`
	Title      string   `yaml:"title"`
	Visibility string   `yaml:"visibility"` // public | team | restricted
	Teams      []string `yaml:"teams"`
	Roles      []string `yaml:"roles"`
	Tags       []string `yaml:"tags"`
	Sections   []Section
}

// Catalog is the whole validated content set.
type Catalog struct {
	Routes   map[string]*Route
	Teams    map[string]*Team
	Docs     map[string]*Doc
	Access   []AccessSystem
	Rubrics  map[string]*Rubric
	DocOrder []string
}

// AccessSystem returns a catalog entry by id.
func (c *Catalog) AccessSystem(id string) (AccessSystem, bool) {
	for _, s := range c.Access {
		if s.ID == id {
			return s, true
		}
	}
	return AccessSystem{}, false
}

// Load parses and validates content from fsys.
func Load(fsys fs.FS) (*Catalog, error) {
	c := &Catalog{Routes: map[string]*Route{}, Teams: map[string]*Team{}, Docs: map[string]*Doc{}, Rubrics: map[string]*Rubric{}}
	if err := eachFile(fsys, "routes", ".yaml", func(b []byte, name string) error {
		var r Route
		if err := yaml.Unmarshal(b, &r); err != nil {
			return err
		}
		c.Routes[r.ID] = &r
		return nil
	}); err != nil {
		return nil, err
	}
	if err := eachFile(fsys, "teams", ".yaml", func(b []byte, name string) error {
		var t Team
		if err := yaml.Unmarshal(b, &t); err != nil {
			return err
		}
		c.Teams[t.ID] = &t
		return nil
	}); err != nil {
		return nil, err
	}
	if err := eachFile(fsys, "teachback", ".yaml", func(b []byte, name string) error {
		var r Rubric
		if err := yaml.Unmarshal(b, &r); err != nil {
			return err
		}
		c.Rubrics[r.Topic] = &r
		return nil
	}); err != nil {
		return nil, err
	}
	if err := eachFile(fsys, "kb", ".md", func(b []byte, name string) error {
		d, err := ParseDoc(b)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		c.Docs[d.ID] = d
		c.DocOrder = append(c.DocOrder, d.ID)
		return nil
	}); err != nil {
		return nil, err
	}
	b, err := fs.ReadFile(fsys, "access_catalog.yaml")
	if err != nil {
		return nil, err
	}
	var ac struct {
		Systems []AccessSystem `yaml:"systems"`
	}
	if err := yaml.Unmarshal(b, &ac); err != nil {
		return nil, fmt.Errorf("access_catalog: %w", err)
	}
	c.Access = ac.Systems
	sort.Strings(c.DocOrder)
	return c, c.Validate()
}

func eachFile(fsys fs.FS, dir, ext string, fn func([]byte, string) error) error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ext) {
			continue
		}
		p := path.Join(dir, e.Name())
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		if err := fn(b, p); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}
	return nil
}

// ParseDoc parses a markdown document with YAML front matter.
func ParseDoc(b []byte) (*Doc, error) {
	b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(b, []byte("---\n")) {
		return nil, fmt.Errorf("missing front matter")
	}
	rest := b[4:]
	end := bytes.Index(rest, []byte("\n---\n"))
	if end < 0 {
		return nil, fmt.Errorf("unterminated front matter")
	}
	var d Doc
	if err := yaml.Unmarshal(rest[:end], &d); err != nil {
		return nil, err
	}
	if d.ID == "" || d.Title == "" {
		return nil, fmt.Errorf("doc needs id and title")
	}
	if d.Visibility == "" {
		d.Visibility = "public"
	}
	body := string(rest[end+5:])
	var cur *Section
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "## ") {
			d.Sections = append(d.Sections, Section{Heading: strings.TrimSpace(line[3:])})
			cur = &d.Sections[len(d.Sections)-1]
			continue
		}
		if cur == nil {
			if strings.TrimSpace(line) == "" {
				continue
			}
			d.Sections = append(d.Sections, Section{Heading: d.Title})
			cur = &d.Sections[len(d.Sections)-1]
		}
		cur.Text += line + "\n"
	}
	for i := range d.Sections {
		d.Sections[i].Text = strings.TrimSpace(d.Sections[i].Text)
	}
	return &d, nil
}

// Validate checks referential integrity of the content.
func (c *Catalog) Validate() error {
	var errs []string
	for _, r := range c.Routes {
		seen := map[string]bool{}
		for _, t := range r.AllTasks() {
			if seen[t.ID] {
				errs = append(errs, fmt.Sprintf("route %s: duplicate task %s", r.ID, t.ID))
			}
			seen[t.ID] = true
			switch t.Owner {
			case "intern", "mentor", "lead":
			default:
				errs = append(errs, fmt.Sprintf("route %s: task %s bad owner %q", r.ID, t.ID, t.Owner))
			}
			if t.Doc != "" && c.Docs[t.Doc] == nil {
				errs = append(errs, fmt.Sprintf("route %s: task %s unknown doc %s", r.ID, t.ID, t.Doc))
			}
			if t.Kind == "teachback" && c.Rubrics[t.Topic] == nil {
				errs = append(errs, fmt.Sprintf("route %s: task %s unknown rubric %s", r.ID, t.ID, t.Topic))
			}
			for _, s := range t.Systems {
				if _, ok := c.AccessSystem(s); !ok {
					errs = append(errs, fmt.Sprintf("route %s: task %s unknown system %s", r.ID, t.ID, s))
				}
			}
		}
		for _, t := range r.AllTasks() {
			for _, d := range t.DependsOn {
				if !seen[d] {
					errs = append(errs, fmt.Sprintf("route %s: task %s depends on unknown %s", r.ID, t.ID, d))
				}
			}
		}
	}
	for _, t := range c.Teams {
		if c.Routes[t.Route] == nil {
			errs = append(errs, fmt.Sprintf("team %s: unknown route %s", t.ID, t.Route))
		}
	}
	for _, r := range c.Rubrics {
		if c.Docs[r.Doc] == nil {
			errs = append(errs, fmt.Sprintf("rubric %s: unknown doc %s", r.Topic, r.Doc))
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return fmt.Errorf("content invalid: %s", strings.Join(errs, "; "))
	}
	return nil
}
