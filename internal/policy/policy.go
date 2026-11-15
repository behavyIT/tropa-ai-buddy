// Package policy is the single place where permissions are decided. Every
// endpoint, search query and tool call goes through it — the LLM never decides
// what a user may see or do.
package policy

import (
	"fmt"
	"slices"

	"tropa/internal/catalog"
	"tropa/internal/directory"
	"tropa/internal/domain"
)

// Policy evaluates permissions on top of the directory role model.
type Policy struct{ dir *directory.Directory }

// New creates a policy.
func New(dir *directory.Directory) *Policy { return &Policy{dir: dir} }

// CanViewUser: self, the intern's mentor, the team lead, or HR BP.
func (p *Policy) CanViewUser(viewer *directory.Person, targetID string) bool {
	if viewer.ID == targetID {
		return true
	}
	t, ok := p.dir.ByID(targetID)
	if !ok {
		return false
	}
	switch viewer.Role {
	case directory.RoleHRBP:
		return t.Role == directory.RoleIntern
	case directory.RoleLead:
		return t.TeamID == viewer.TeamID
	case directory.RoleMentor:
		return t.MentorID == viewer.ID
	}
	return false
}

// VisibleInterns lists interns the viewer may follow.
func (p *Policy) VisibleInterns(viewer *directory.Person) []*directory.Person {
	switch viewer.Role {
	case directory.RoleHRBP:
		return p.dir.Interns()
	case directory.RoleLead:
		return p.dir.TeamInterns(viewer.TeamID)
	case directory.RoleMentor:
		return p.dir.Mentees(viewer.ID)
	}
	return nil
}

// CanApprove: only the lead of the requester's team approves absences/access.
func (p *Policy) CanApprove(viewer *directory.Person, requesterID string) bool {
	r, ok := p.dir.ByID(requesterID)
	return ok && viewer.Role == directory.RoleLead && viewer.TeamID == r.TeamID && viewer.ID != r.ID
}

// CanAnswer: the addressed mentor or the team lead.
func (p *Policy) CanAnswer(viewer *directory.Person, q domain.Question) bool {
	return viewer.ID == q.MentorID || (viewer.Role == directory.RoleLead && viewer.TeamID == q.TeamID)
}

// CanCompleteTask checks who may mark a route task done for an intern.
func (p *Policy) CanCompleteTask(viewer *directory.Person, internID string, t catalog.Task) bool {
	intern, ok := p.dir.ByID(internID)
	if !ok || intern.Role != directory.RoleIntern {
		return false
	}
	switch t.Owner {
	case "intern":
		return viewer.ID == internID && t.Kind == "manual"
	case "mentor":
		return viewer.ID == intern.MentorID || (viewer.Role == directory.RoleLead && viewer.TeamID == intern.TeamID)
	case "lead":
		return viewer.Role == directory.RoleLead && viewer.TeamID == intern.TeamID
	}
	return false
}

// CanSeeDoc applies the document ACL.
func (p *Policy) CanSeeDoc(viewer *directory.Person, visibility string, teams, roles []string) bool {
	switch visibility {
	case "public":
		return true
	case "team":
		return viewer.Role == directory.RoleHRBP || slices.Contains(teams, viewer.TeamID)
	case "restricted":
		if !slices.Contains(roles, viewer.Role) {
			return false
		}
		return len(teams) == 0 || viewer.Role == directory.RoleHRBP || slices.Contains(teams, viewer.TeamID)
	}
	return false
}

// CanSeeCard: knowledge cards are team-scoped.
func (p *Policy) CanSeeCard(viewer *directory.Person, teamID string) bool {
	return viewer.Role == directory.RoleHRBP || viewer.TeamID == teamID
}

// AccessDecision explains whether a user may request a system.
type AccessDecision struct {
	Allowed bool
	Reason  string
}

// CanRequestAccess checks role restrictions of the access catalog.
func (p *Policy) CanRequestAccess(viewer *directory.Person, sys catalog.AccessSystem) AccessDecision {
	if slices.Contains(sys.ForbiddenFor, viewer.Role) {
		return AccessDecision{Allowed: false, Reason: fmt.Sprintf("%s недоступен для роли «стажёр» (тестовое правило)", sys.Title)}
	}
	return AccessDecision{Allowed: true}
}

// CanAskMentor: interns with an assigned mentor.
func (p *Policy) CanAskMentor(viewer *directory.Person) bool {
	return viewer.Role == directory.RoleIntern && viewer.MentorID != ""
}

// InsightsScope returns team ids visible in analytics (nil = all teams).
func (p *Policy) InsightsScope(viewer *directory.Person) ([]string, bool) {
	switch viewer.Role {
	case directory.RoleHRBP:
		return nil, true
	case directory.RoleLead:
		return []string{viewer.TeamID}, true
	}
	return nil, false
}

// CanManageFlag: HR BP manages global flags, a lead manages own team flags.
func (p *Policy) CanManageFlag(viewer *directory.Person, scope string) bool {
	switch viewer.Role {
	case directory.RoleHRBP:
		return true
	case directory.RoleLead:
		return scope == "team:"+viewer.TeamID
	}
	return false
}
