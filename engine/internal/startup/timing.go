// Package startup keeps readiness independent of stores and their backfills.
package startup

import "time"

// Observer receives fixed stage names and durations, never database values/errors.
// A zero duration announces stage entry; completion always has a positive duration.
type Observer func(stage string, elapsed time.Duration)

func Measure(observer Observer, stage string, work func() error) error {
	if observer != nil {
		observer(stage, 0)
	}
	start := time.Now()
	err := work()
	if observer != nil {
		observer(stage, time.Since(start))
	}
	return err
}
