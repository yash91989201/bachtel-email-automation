package sending

import (
	"fmt"
	"time"
)

const DefaultRate = 25

func ValidateRate(n int) error {
	if n < 1 || n > 30 {
		return fmt.Errorf("sending rate must be an integer from 1 through 30")
	}
	return nil
}

// nextStart preserves both minimum spacing and the rolling allowance. Keeping
// all recent starts matters when an operator lowers the configured rate.
func nextStart(now time.Time, s Settings) time.Time {
	n := s.Rate
	if n == 0 {
		n = DefaultRate
	}
	next := now
	if s.NextEligible.After(next) {
		next = s.NextEligible
	}
	if s.CooldownUntil.After(next) {
		next = s.CooldownUntil
	}
	interval := time.Minute / time.Duration(n)
	var recent []time.Time
	for _, start := range s.Starts {
		if start.Add(time.Minute).After(now) {
			recent = append(recent, start)
		}
		if start.Add(interval).After(next) {
			next = start.Add(interval)
		}
	}
	// Starts are stored in launch order. Allow starts exactly 60s apart.
	if len(recent) >= n {
		boundary := recent[len(recent)-n].Add(time.Minute)
		if boundary.After(next) {
			next = boundary
		}
	}
	return next
}
