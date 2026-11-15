package clock_test

import (
	"testing"
	"time"

	"tropa/internal/clock"
)

func TestCLK01_SimulatedClockAndCalendar(t *testing.T) {
	wall := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cur := wall
	s := clock.NewSim(time.Date(2026, 11, 9, 9, 30, 0, 0, clock.MSK), func() time.Time { return cur })
	cur = cur.Add(time.Hour)
	if s.Now().Hour() != 10 || s.Now().Minute() != 30 {
		t.Fatalf("time flows: %v", s.Now())
	}
	next := s.AdvanceDays(2)
	if next.Day() != 11 || next.Hour() != 9 {
		t.Fatalf("advance: %v", next)
	}
	start := time.Date(2026, 11, 9, 0, 0, 0, 0, clock.MSK)
	if clock.DayOfInternship(start, next) != 3 || clock.DayOfInternship(start, start.AddDate(0, 0, -5)) != 1 {
		t.Fatal("day of internship")
	}
	fri := time.Date(2026, 11, 13, 12, 0, 0, 0, clock.MSK)
	if clock.AddWorkdays(fri, 1).Weekday() != time.Monday || clock.WorkdaysBetween(fri, fri.AddDate(0, 0, 3)) != 1 {
		t.Fatal("workdays")
	}
	if clock.NewSim(start, nil).Now().IsZero() || (clock.Real{}).Now().Location() != clock.MSK {
		t.Fatal("defaults")
	}
}
