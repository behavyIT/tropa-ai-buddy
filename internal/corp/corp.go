// Package corp is the typed facade over corporate services reached via MCP.
// The agent depends on the Services interface, so tests can inject fakes and
// production can point the MCP client at real Avito connectors.
package corp

import (
	"context"

	"tropa/internal/catalog"
	"tropa/internal/mcp"
)

// Tool names exposed by the corporate MCP server.
const (
	ToolAbsenceCreate     = "absence_create_request"
	ToolAbsenceGet        = "absence_get_request"
	ToolAbsenceDecide     = "absence_decide"
	ToolCalendarEvents    = "calendar_team_events"
	ToolAccessCatalog     = "access_catalog"
	ToolAccessCreate      = "access_create_request"
	ToolAccessGet         = "access_get_request"
	ToolAccessDecide      = "access_decide"
	ToolChecklistComplete = "checklist_complete_item"
	ToolChecklistGet      = "checklist_get"
)

// AbsenceRequest is a request in the absence service.
type AbsenceRequest struct {
	RequestID       string `json:"request_id"`
	EmployeeID      string `json:"employee_id"`
	Type            string `json:"type"`
	DateFrom        string `json:"date_from"`
	DateTo          string `json:"date_to"`
	Comment         string `json:"comment"`
	Status          string `json:"status"`
	ApproverID      string `json:"approver_id"`
	ApproverName    string `json:"approver_name"`
	DecisionComment string `json:"decision_comment,omitempty"`
	CreatedAt       string `json:"created_at"`
}

// AbsenceCreate are arguments of absence_create_request.
type AbsenceCreate struct {
	EmployeeID     string `json:"employee_id"`
	Type           string `json:"type"`
	DateFrom       string `json:"date_from"`
	DateTo         string `json:"date_to"`
	Comment        string `json:"comment"`
	IdempotencyKey string `json:"idempotency_key"`
}

// TeamEvents is the calendar response used for conflict checks.
type TeamEvents struct {
	Events       []catalog.CalendarEvent `json:"events"`
	AbsentByDate map[string]int          `json:"absent_by_date"`
}

// AccessRequest is a request in the access service.
type AccessRequest struct {
	RequestID    string `json:"request_id"`
	EmployeeID   string `json:"employee_id"`
	System       string `json:"system"`
	Level        string `json:"level"`
	Status       string `json:"status"`
	ApproverID   string `json:"approver_id,omitempty"`
	ApproverName string `json:"approver_name,omitempty"`
	CreatedAt    string `json:"created_at"`
}

// AccessCreate are arguments of access_create_request.
type AccessCreate struct {
	EmployeeID     string `json:"employee_id"`
	System         string `json:"system"`
	Justification  string `json:"justification"`
	IdempotencyKey string `json:"idempotency_key"`
}

// ChecklistItem is an item of the newcomer checklist service.
type ChecklistItem struct {
	EmployeeID  string `json:"employee_id"`
	ItemID      string `json:"item_id"`
	Status      string `json:"status"`
	Evidence    string `json:"evidence,omitempty"`
	CompletedBy string `json:"completed_by,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
}

// ChecklistComplete are arguments of checklist_complete_item.
type ChecklistComplete struct {
	EmployeeID     string `json:"employee_id"`
	ItemID         string `json:"item_id"`
	Evidence       string `json:"evidence"`
	IdempotencyKey string `json:"idempotency_key"`
}

// Services is what the buddy needs from corporate systems.
type Services interface {
	CreateAbsence(ctx context.Context, actor string, a AbsenceCreate) (AbsenceRequest, error)
	GetAbsence(ctx context.Context, actor, id string) (AbsenceRequest, error)
	DecideAbsence(ctx context.Context, actor, id string, approve bool, comment string) (AbsenceRequest, error)
	TeamEvents(ctx context.Context, actor, teamID, from, to string) (TeamEvents, error)
	CreateAccess(ctx context.Context, actor string, a AccessCreate) (AccessRequest, error)
	GetAccess(ctx context.Context, actor, id string) (AccessRequest, error)
	DecideAccess(ctx context.Context, actor, id string, approve bool, comment string) (AccessRequest, error)
	CompleteChecklist(ctx context.Context, actor string, c ChecklistComplete) (ChecklistItem, error)
}

// Client implements Services over MCP.
type Client struct{ MCP *mcp.Client }

// CreateAbsence creates a day-off request.
func (c *Client) CreateAbsence(ctx context.Context, actor string, a AbsenceCreate) (r AbsenceRequest, err error) {
	return r, c.MCP.Call(ctx, actor, ToolAbsenceCreate, a, &r)
}

// GetAbsence reads a request.
func (c *Client) GetAbsence(ctx context.Context, actor, id string) (r AbsenceRequest, err error) {
	return r, c.MCP.Call(ctx, actor, ToolAbsenceGet, map[string]string{"request_id": id}, &r)
}

// DecideAbsence approves or rejects a request (lead only).
func (c *Client) DecideAbsence(ctx context.Context, actor, id string, approve bool, comment string) (r AbsenceRequest, err error) {
	return r, c.MCP.Call(ctx, actor, ToolAbsenceDecide, map[string]any{"request_id": id, "decision": decision(approve), "comment": comment}, &r)
}

// TeamEvents returns team calendar events in a range.
func (c *Client) TeamEvents(ctx context.Context, actor, teamID, from, to string) (r TeamEvents, err error) {
	return r, c.MCP.Call(ctx, actor, ToolCalendarEvents, map[string]string{"team_id": teamID, "date_from": from, "date_to": to}, &r)
}

// CreateAccess requests access to a system.
func (c *Client) CreateAccess(ctx context.Context, actor string, a AccessCreate) (r AccessRequest, err error) {
	return r, c.MCP.Call(ctx, actor, ToolAccessCreate, a, &r)
}

// GetAccess reads an access request.
func (c *Client) GetAccess(ctx context.Context, actor, id string) (r AccessRequest, err error) {
	return r, c.MCP.Call(ctx, actor, ToolAccessGet, map[string]string{"request_id": id}, &r)
}

// DecideAccess approves or rejects an access request (lead only).
func (c *Client) DecideAccess(ctx context.Context, actor, id string, approve bool, comment string) (r AccessRequest, err error) {
	return r, c.MCP.Call(ctx, actor, ToolAccessDecide, map[string]any{"request_id": id, "decision": decision(approve), "comment": comment}, &r)
}

// CompleteChecklist marks a checklist item done.
func (c *Client) CompleteChecklist(ctx context.Context, actor string, cc ChecklistComplete) (r ChecklistItem, err error) {
	return r, c.MCP.Call(ctx, actor, ToolChecklistComplete, cc, &r)
}

func decision(approve bool) string {
	if approve {
		return "approve"
	}
	return "reject"
}
