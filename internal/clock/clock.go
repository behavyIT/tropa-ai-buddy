// Package clock provides the time source. In demo mode a simulated clock lets
// reviewers "time-travel" through the first weeks of an internship; all
// domain logic reads time only through Clock, which also makes it testable.
package clock

import (
	"sync"
	"time"
)

// MSK is the timezone used for the synthetic company.
var MSK = time.FixedZone("MSK", 3*60*60)

// Clock is the single time source of the application.
type Clock interface{ Now() time.Time }

// Real is the wall clock.
type Real struct{}

// Now returns the current wall-clock time in MSK.
func (Real) Now() time.Time { return time.Now().In(MSK) }

// Sim is a simulated clock: Now = base + (wall - setAt). Time keeps flowing
// naturally inside a simulated day, and Advance jumps to the next morning.
type Sim struct {
	mu    sync.RWMutex
	base  time.Time
	setAt time.Time
	wall  func() time.Time
}

// NewSim creates a simulated clock anchored at base.
func NewSim(base time.Time, wall func() time.Time) *Sim {
	if wall == nil {
		wall = time.Now
	}
	return &Sim{base: base.In(MSK), setAt: wall(), wall: wall}
}

// Now returns simulated time.
func (s *Sim) Now() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.base.Add(s.wall().Sub(s.setAt)).In(MSK)
}

// Set moves the simulated time to t.
func (s *Sim) Set(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.base = t.In(MSK)
	s.setAt = s.wall()
}

// AdvanceDays jumps n days ahead to 09:30 MSK of the target day.
func (s *Sim) AdvanceDays(n int) time.Time {
	d := StartOfDay(s.Now()).AddDate(0, 0, n).Add(9*time.Hour + 30*time.Minute)
	s.Set(d)
	return d
}

// StartOfDay truncates t to midnight MSK.
func StartOfDay(t time.Time) time.Time {
	t = t.In(MSK)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, MSK)
}

// DayOfInternship returns 1-based calendar day number since start.
func DayOfInternship(start, now time.Time) int {
	d := int(StartOfDay(now).Sub(StartOfDay(start)).Hours()/24) + 1
	if d < 1 {
		return 1
	}
	return d
}

// AddWorkdays adds n working days (Mon–Fri) to t's date.
func AddWorkdays(t time.Time, n int) time.Time {
	d := StartOfDay(t)
	for n > 0 {
		d = d.AddDate(0, 0, 1)
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			n--
		}
	}
	return d
}

// WorkdaysBetween counts working days in (from, to].
func WorkdaysBetween(from, to time.Time) int {
	a, b := StartOfDay(from), StartOfDay(to)
	n := 0
	for d := a.AddDate(0, 0, 1); !d.After(b); d = d.AddDate(0, 0, 1) {
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			n++
		}
	}
	return n
}
