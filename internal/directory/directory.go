// Package directory imitates Avito's existing employee directory and role
// model. The case explicitly says real HR systems are out of scope: auth and
// roles already exist, so the prototype ships a synthetic, read-only directory.
package directory

import (
	"sort"
	"time"

	"tropa/internal/clock"
)

// Roles.
const (
	RoleIntern = "intern"
	RoleMentor = "mentor"
	RoleLead   = "lead"
	RoleHRBP   = "hrbp"
)

// Person is a synthetic employee.
type Person struct {
	ID           string    `json:"id"`
	Login        string    `json:"login"`
	Name         string    `json:"name"`
	Role         string    `json:"role"`
	Title        string    `json:"title"`
	TeamID       string    `json:"team_id"`
	MentorID     string    `json:"mentor_id,omitempty"`
	LeadID       string    `json:"lead_id,omitempty"`
	StartDate    time.Time `json:"start_date"`
	Contact      string    `json:"contact"`
	passwordHash string
}

// TestPassword is the shared password of all synthetic demo accounts.
const TestPassword = "buddy2026"

// bcrypt(cost 10) of TestPassword, precomputed to keep startup fast.
const testHash = "$2a$10$Dv/RbCZ1pksVewvkQBhIBevjBZorRvAThvEBbX8Xe.TQuxLoTFT7u"

// InternshipStart is the synthetic start date of the current cohort (Monday).
var InternshipStart = time.Date(2026, 11, 9, 0, 0, 0, 0, clock.MSK)

// Directory is an in-memory read-only employee directory.
type Directory struct {
	byID    map[string]*Person
	byLogin map[string]*Person
}

// New returns the synthetic directory.
func New() *Directory {
	people := []*Person{
		{ID: "u-intern-alex", Login: "alex", Name: "Алекс Новиков", Role: RoleIntern, Title: "Стажёр Backend", TeamID: "ads-backend", MentorID: "u-mentor-artem", LeadID: "u-lead-maria", StartDate: InternshipStart, Contact: "@alex.novikov"},
		{ID: "u-intern-dana", Login: "dana", Name: "Дана Орлова", Role: RoleIntern, Title: "Стажёр Frontend", TeamID: "search-frontend", MentorID: "u-mentor-ilya", LeadID: "u-lead-oleg", StartDate: InternshipStart, Contact: "@dana.orlova"},
		{ID: "u-mentor-artem", Login: "artem", Name: "Артём Ким", Role: RoleMentor, Title: "Senior Backend, наставник", TeamID: "ads-backend", LeadID: "u-lead-maria", Contact: "@artem.kim"},
		{ID: "u-mentor-ilya", Login: "ilya", Name: "Илья Смирнов", Role: RoleMentor, Title: "Senior Frontend, наставник", TeamID: "search-frontend", LeadID: "u-lead-oleg", Contact: "@ilya.smirnov"},
		{ID: "u-lead-maria", Login: "maria", Name: "Мария Соколова", Role: RoleLead, Title: "Руководитель команды", TeamID: "ads-backend", Contact: "@maria.sokolova"},
		{ID: "u-lead-oleg", Login: "oleg", Name: "Олег Лебедев", Role: RoleLead, Title: "Руководитель команды", TeamID: "search-frontend", Contact: "@oleg.lebedev"},
		{ID: "u-hrbp-olga", Login: "olga", Name: "Ольга Петрова", Role: RoleHRBP, Title: "HR BP стажёрской программы", Contact: "@olga.petrova"},
	}
	d := &Directory{byID: map[string]*Person{}, byLogin: map[string]*Person{}}
	for _, p := range people {
		p.passwordHash = testHash
		d.byID[p.ID] = p
		d.byLogin[p.Login] = p
	}
	return d
}

// ByID returns a person by id.
func (d *Directory) ByID(id string) (*Person, bool) { p, ok := d.byID[id]; return p, ok }

// ByLogin returns a person by login.
func (d *Directory) ByLogin(login string) (*Person, bool) { p, ok := d.byLogin[login]; return p, ok }

// PasswordHash exposes the stored hash for the auth package.
func (p *Person) PasswordHash() string { return p.passwordHash }

// Interns returns all interns sorted by id.
func (d *Directory) Interns() []*Person {
	return d.filter(func(p *Person) bool { return p.Role == RoleIntern })
}

// All returns every person sorted by id.
func (d *Directory) All() []*Person { return d.filter(func(*Person) bool { return true }) }

// Mentees returns interns mentored by mentorID.
func (d *Directory) Mentees(mentorID string) []*Person {
	return d.filter(func(p *Person) bool { return p.Role == RoleIntern && p.MentorID == mentorID })
}

// TeamInterns returns interns of a team.
func (d *Directory) TeamInterns(teamID string) []*Person {
	return d.filter(func(p *Person) bool { return p.Role == RoleIntern && p.TeamID == teamID })
}

// LeadOf returns the lead of a team.
func (d *Directory) LeadOf(teamID string) (*Person, bool) {
	for _, p := range d.byID {
		if p.Role == RoleLead && p.TeamID == teamID {
			return p, true
		}
	}
	return nil, false
}

// HRBP returns the HR business partner.
func (d *Directory) HRBP() (*Person, bool) {
	for _, p := range d.byID {
		if p.Role == RoleHRBP {
			return p, true
		}
	}
	return nil, false
}

func (d *Directory) filter(keep func(*Person) bool) []*Person {
	var out []*Person
	for _, p := range d.byID {
		if keep(p) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// FirstName returns the first word of the name.
func (p *Person) FirstName() string {
	for i, r := range p.Name {
		if r == ' ' {
			return p.Name[:i]
		}
	}
	return p.Name
}
