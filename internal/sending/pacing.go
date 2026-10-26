package sending

import (
	"fmt"
	"time"
)

const (
	// DefaultRate is the pacing a mailbox receives when its settings are first
	// created. Operators may save any whole number of starts per minute from 1
	// through 30.
	DefaultRate = 15
	// DefaultConcurrency is the submission capacity a mailbox receives when its
	// settings are first created. Operators may save any whole number of
	// simultaneous requests from 1 through 10.
	DefaultConcurrency = 2
)

func ValidateRate(n int) error {
	if n < 1 || n > 30 {
		return fmt.Errorf("sending rate must be an integer from 1 through 30")
	}
	return nil
}

func ValidateConcurrency(n int) error {
	if n < 1 || n > 10 {
		return fmt.Errorf("concurrency must be an integer from 1 through 10")
	}
	return nil
}

// ValidateSettings rejects a mailbox settings pair that cannot govern launches.
// A stored value outside its range is an error, never permission to fall back
// to a default limit.
func ValidateSettings(s Settings) error {
	if s.Concurrency < 1 || s.Concurrency > 10 {
		return fmt.Errorf("stored concurrency must be an integer from 1 through 10")
	}
	if s.Rate < 1 || s.Rate > 30 {
		return fmt.Errorf("stored sending rate must be an integer from 1 through 30")
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
