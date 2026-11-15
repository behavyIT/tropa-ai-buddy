// Package flags implements runtime feature flags with per-team overrides —
// the way scenarios are rolled out team by team (and A/B-tested by cohort).
package flags

import (
	"context"
	"sort"
	"time"

	"tropa/internal/domain"
)

// Keys.
const (
	ActionsAbsence = "actions.absence"
	ActionsAccess  = "actions.access"
	Teachback      = "teachback"
	MentorDigest   = "mentor.digest"
	LLM            = "llm"
)

// Def is a flag definition with its compiled default.
type Def struct {
	Key         string `json:"key"`
	Description string `json:"description"`
	Default     bool   `json:"default"`
}

// Defs are all known flags.
var Defs = []Def{
	{ActionsAbsence, "Оформление отгула через Тропу (сервис отсутствий)", true},
	{ActionsAccess, "Заявки на доступы через Тропу (сервис доступов)", true},
	{Teachback, "Режим «Расскажи мне» для пунктов чек-листа", true},
	{MentorDigest, "Вопросы наставнику и сводка вопросов", true},
	{LLM, "LLM-режим ответов (если настроен ключ)", true},
}

// Store is the persistence needed by the service.
type Store interface {
	ListFlags(ctx context.Context) ([]domain.Flag, error)
	SetFlag(ctx context.Context, f domain.Flag) error
}

// Service resolves flags: team override → global override → default.
type Service struct{ st Store }

// New creates the service.
func New(st Store) *Service { return &Service{st: st} }

// View is the effective value of a flag for a team.
type View struct {
	Def
	Enabled bool   `json:"enabled"`
	Source  string `json:"source"` // default | global | team
}

// Known reports whether a key is defined.
func Known(key string) bool {
	for _, d := range Defs {
		if d.Key == key {
			return true
		}
	}
	return false
}

// List returns effective flags for a team.
func (s *Service) List(ctx context.Context, teamID string) ([]View, error) {
	stored, err := s.st.ListFlags(ctx)
	if err != nil {
		return nil, err
	}
	idx := map[string]domain.Flag{}
	for _, f := range stored {
		idx[f.Key+"|"+f.Scope] = f
	}
	out := make([]View, 0, len(Defs))
	for _, d := range Defs {
		v := View{Def: d, Enabled: d.Default, Source: "default"}
		if f, ok := idx[d.Key+"|global"]; ok {
			v.Enabled, v.Source = f.Enabled, "global"
		}
		if f, ok := idx[d.Key+"|team:"+teamID]; ok && teamID != "" {
			v.Enabled, v.Source = f.Enabled, "team"
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// Enabled returns the effective value; on storage errors the compiled default wins.
func (s *Service) Enabled(ctx context.Context, key, teamID string) bool {
	views, err := s.List(ctx, teamID)
	if err != nil {
		for _, d := range Defs {
			if d.Key == key {
				return d.Default
			}
		}
		return false
	}
	for _, v := range views {
		if v.Key == key {
			return v.Enabled
		}
	}
	return false
}

// Set stores an override.
func (s *Service) Set(ctx context.Context, key, scope string, enabled bool, by string, now time.Time) error {
	return s.st.SetFlag(ctx, domain.Flag{Key: key, Scope: scope, Enabled: enabled, UpdatedBy: by, UpdatedAt: now})
}
