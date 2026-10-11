package replay

import (
	"fmt"
	"strings"
	"time"
)

// ParseDeadline parses an explicit deadline policy value for the replay CLI:
// the literal "none" (case-insensitive, surrounding space ignored) means "no
// deadline"; any other value must be a positive Go duration. It returns
// (0, true, nil) for "none" so callers set an explicit disabled flag instead
// of overloading a zero duration, which would be ambiguous with an unset
// default.
func ParseDeadline(s string) (time.Duration, bool, error) {
	v := strings.TrimSpace(strings.ToLower(s))
	if v == "none" {
		return 0, true, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, false, fmt.Errorf("replay: invalid deadline %q: %w", s, err)
	}
	if d <= 0 {
		return 0, false, fmt.Errorf("replay: deadline must be a positive duration or \"none\", got %q", s)
	}
	return d, false, nil
}

// DeadlineString renders a deadline policy value in the form accepted by
// ParseDeadline ("none" or a duration).
func DeadlineString(d time.Duration, disabled bool) string {
	if disabled {
		return "none"
	}
	return d.String()
}
