package services

import "time"

func durationMilliseconds(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}

func elapsedMilliseconds(start time.Time) float64 {
	if start.IsZero() {
		return 0
	}
	return durationMilliseconds(time.Since(start))
}
