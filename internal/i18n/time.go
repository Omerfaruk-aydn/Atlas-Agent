package i18n

import (
	"fmt"
	"time"

	"github.com/dustin/go-humanize"
)

// RelativeTime localizes interface timestamps without changing stored dates.
func RelativeTime(code string, at, now time.Time) string {
	if code == "en" || !Supported(code) {
		return humanize.RelTime(at, now, "ago", "from now")
	}
	delta := now.Sub(at)
	future := delta < 0
	if future {
		delta = -delta
	}
	if delta < time.Second {
		return Text(code, "just now")
	}
	units := []struct {
		duration               time.Duration
		singular, plural, dual string
	}{
		{365 * 24 * time.Hour, "year", "years", "two years"},
		{30 * 24 * time.Hour, "month", "months", "two months"},
		{7 * 24 * time.Hour, "week", "weeks", "two weeks"},
		{24 * time.Hour, "day", "days", "two days"},
		{time.Hour, "hour", "hours", "two hours"},
		{time.Minute, "minute", "minutes", "two minutes"},
		{time.Second, "second", "seconds", "two seconds"},
	}
	for _, unit := range units {
		if delta < unit.duration {
			continue
		}
		count := int64(delta / unit.duration)
		label := unit.plural
		if count == 1 || code == "ar" && (count%100 >= 11 || count%100 <= 2) {
			label = unit.singular
		}
		quantity := fmt.Sprintf("%d %s", count, Text(code, label))
		if code == "ar" && count == 2 {
			quantity = Text(code, unit.dual)
		}
		format := "%s ago"
		if future {
			format = "%s from now"
		}
		return fmt.Sprintf(Text(code, format), quantity)
	}
	return Text(code, "just now")
}
