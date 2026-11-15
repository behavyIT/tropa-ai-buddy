package catalog_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"tropa/content"
	"tropa/internal/catalog"
)

func TestCAT01_EmbeddedContentIsValid(t *testing.T) {
	c, err := catalog.Load(content.FS)
	if err != nil {
		t.Fatal(err)
	}
	r := c.Routes["backend"]
	if len(r.AllTasks()) != 22 {
		t.Fatalf("tasks: %d", len(r.AllTasks()))
	}
	if _, ok := r.Task("nope"); ok {
		t.Fatal("unknown task")
	}
	if _, ok := c.AccessSystem("nope"); ok {
		t.Fatal("unknown system")
	}
	for _, d := range c.Docs {
		if len(d.Sections) == 0 {
			t.Fatalf("doc %s has no sections", d.ID)
		}
	}
}

func TestCAT02_ValidationCatchesBrokenContent(t *testing.T) {
	fs := fstest.MapFS{
		"routes/r.yaml": {Data: []byte(`id: r
stages:
  - id: s
    tasks:
      - {id: a, title: A, owner: boss, kind: manual, doc: missing}
      - {id: a, title: A2, owner: intern, kind: teachback, topic: nope, systems: [ghost], depends_on: [zzz]}
`)},
		"teams/t.yaml":        {Data: []byte("id: t\nroute: nope\n")},
		"teachback/x.yaml":    {Data: []byte("topic: x\ndoc: nodoc\n")},
		"kb/d.md":             {Data: []byte("---\nid: d\ntitle: D\n---\nintro line\n## H\ntext\n")},
		"access_catalog.yaml": {Data: []byte("systems: []\n")},
	}
	_, err := catalog.Load(fs)
	if err == nil {
		t.Fatal("expected validation errors")
	}
	for _, want := range []string{"duplicate task a", "bad owner", "unknown doc missing", "unknown rubric nope", "unknown system ghost", "depends on unknown zzz", "unknown route nope", "rubric x: unknown doc"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
}

func TestCAT03_ParseErrors(t *testing.T) {
	cases := map[string]string{
		"no front matter": "hello",
		"unterminated":    "---\nid: x\n",
		"missing id":      "---\ntitle: x\n---\n",
		"bad yaml":        "---\n: : :\n---\n",
	}
	for name, src := range cases {
		if _, err := catalog.ParseDoc([]byte(src)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	d, err := catalog.ParseDoc([]byte("---\r\nid: x\r\ntitle: X\r\n---\r\nintro\r\n"))
	if err != nil || d.Visibility != "public" || d.Sections[0].Heading != "X" {
		t.Fatalf("crlf/default: %+v %v", d, err)
	}
	for _, fs := range []fstest.MapFS{
		{"routes/r.yaml": {Data: []byte("[")}},
		{"routes/x.txt": {Data: []byte("")}, "teams/t.yaml": {Data: []byte("[")}},
		{"routes/x.txt": {}, "teams/x.txt": {}, "teachback/t.yaml": {Data: []byte("[")}},
		{"routes/x.txt": {}, "teams/x.txt": {}, "teachback/x.txt": {}, "kb/a.md": {Data: []byte("x")}},
		{"routes/x.txt": {}, "teams/x.txt": {}, "teachback/x.txt": {}, "kb/x.txt": {}},
		{"routes/x.txt": {}, "teams/x.txt": {}, "teachback/x.txt": {}, "kb/x.txt": {}, "access_catalog.yaml": {Data: []byte("[")}},
		{},
	} {
		if _, err := catalog.Load(fs); err == nil {
			t.Errorf("expected load error for %v", fs)
		}
	}
}
