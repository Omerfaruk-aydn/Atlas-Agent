// Package routine implements scheduled routines: prompts that run
// headlessly on a cron schedule, with their results kept in a log.
//
// The package is deliberately small and independent of the agent: a
// [Store] persists routine definitions and run logs, a [Schedule] decides
// when a routine is due, and a [Runner] drives an [Executor] for every due
// routine. The real executor starts the agent binary's own non-interactive
// run path; tests plug in a fake one.
package routine

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule is a parsed five-field cron expression:
//
//	minute hour day-of-month month day-of-week
//
// Each field accepts "*", numbers, ranges ("1-5"), lists ("1,15"), steps
// ("*/10", "0-30/5") and, for month and day-of-week, English names
// ("jan", "mon"). Day-of-week 7 means Sunday. As in classic cron, when both
// day fields are restricted a time matches if either one matches. The
// macros @hourly, @daily, @midnight, @weekly, @monthly and @yearly are
// also understood. Times are evaluated in the location of the time passed
// to [Schedule.Next].
type Schedule struct {
	expr    string
	minute  uint64
	hour    uint64
	dom     uint64
	month   uint64
	dow     uint64
	domStar bool
	dowStar bool
}

type fieldSpec struct {
	name     string
	min, max int
	names    map[string]int
}

var (
	minuteSpec = fieldSpec{name: "minute", min: 0, max: 59}
	hourSpec   = fieldSpec{name: "hour", min: 0, max: 23}
	domSpec    = fieldSpec{name: "day-of-month", min: 1, max: 31}
	monthSpec  = fieldSpec{name: "month", min: 1, max: 12, names: map[string]int{
		"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
		"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
	}}
	dowSpec = fieldSpec{name: "day-of-week", min: 0, max: 7, names: map[string]int{
		"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
	}}
)

var macros = map[string]string{
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
	"@monthly":  "0 0 1 * *",
	"@weekly":   "0 0 * * 0",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@hourly":   "0 * * * *",
}

// ParseSchedule parses a cron expression.
func ParseSchedule(expr string) (Schedule, error) {
	original := strings.TrimSpace(expr)
	if original == "" {
		return Schedule{}, errors.New("cron expression is empty")
	}
	text := original
	if strings.HasPrefix(text, "@") {
		expanded, ok := macros[strings.ToLower(text)]
		if !ok {
			return Schedule{}, fmt.Errorf("unknown cron macro %q", text)
		}
		text = expanded
	}
	fields := strings.Fields(text)
	if len(fields) != 5 {
		return Schedule{}, fmt.Errorf("cron expression %q needs 5 fields (minute hour day-of-month month day-of-week), got %d", original, len(fields))
	}
	s := Schedule{expr: original}
	specs := []fieldSpec{minuteSpec, hourSpec, domSpec, monthSpec, dowSpec}
	targets := []*uint64{&s.minute, &s.hour, &s.dom, &s.month, &s.dow}
	for i, field := range fields {
		bits, star, err := parseField(field, specs[i])
		if err != nil {
			return Schedule{}, fmt.Errorf("cron %s field %q: %w", specs[i].name, field, err)
		}
		*targets[i] = bits
		switch i {
		case 2:
			s.domStar = star
		case 4:
			s.dowStar = star
		}
	}
	// Day-of-week 7 is an alias for Sunday.
	if s.dow&(1<<7) != 0 {
		s.dow = (s.dow &^ (1 << 7)) | 1
	}
	return s, nil
}

// String returns the expression the schedule was parsed from.
func (s Schedule) String() string { return s.expr }

func parseField(field string, spec fieldSpec) (bits uint64, star bool, err error) {
	for part := range strings.SplitSeq(field, ",") {
		if part == "" {
			return 0, false, errors.New("empty list item")
		}
		rangePart, stepPart, hasStep := strings.Cut(part, "/")
		step := 1
		if hasStep {
			step, err = strconv.Atoi(stepPart)
			if err != nil || step < 1 {
				return 0, false, fmt.Errorf("invalid step %q", stepPart)
			}
		}
		lo, hi := spec.min, spec.max
		switch {
		case rangePart == "*" || rangePart == "?":
			if !hasStep {
				star = true
			}
		case strings.Contains(rangePart, "-"):
			a, b, _ := strings.Cut(rangePart, "-")
			if lo, err = parseValue(a, spec); err != nil {
				return 0, false, err
			}
			if hi, err = parseValue(b, spec); err != nil {
				return 0, false, err
			}
			if lo > hi {
				return 0, false, fmt.Errorf("range %q runs backwards", rangePart)
			}
		default:
			if lo, err = parseValue(rangePart, spec); err != nil {
				return 0, false, err
			}
			hi = lo
			if hasStep {
				// "5/10" means "from 5 to the end, every 10".
				hi = spec.max
			}
		}
		for v := lo; v <= hi; v += step {
			bits |= 1 << uint(v)
		}
	}
	return bits, star, nil
}

func parseValue(text string, spec fieldSpec) (int, error) {
	if v, ok := spec.names[strings.ToLower(text)]; ok {
		return v, nil
	}
	v, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("invalid value %q", text)
	}
	if v < spec.min || v > spec.max {
		return 0, fmt.Errorf("value %d is outside %d-%d", v, spec.min, spec.max)
	}
	return v, nil
}

func (s Schedule) dayMatches(t time.Time) bool {
	domOK := s.dom&(1<<uint(t.Day())) != 0
	dowOK := s.dow&(1<<uint(t.Weekday())) != 0
	if !s.domStar && !s.dowStar {
		return domOK || dowOK
	}
	return domOK && dowOK
}

// searchHorizon bounds Next so an impossible date such as "0 0 31 2 *"
// returns instead of looping.
const searchHorizon = 5 * 366 * 24 * time.Hour

// Next returns the first matching minute strictly after t, or the zero
// time when the expression never matches within five years.
func (s Schedule) Next(t time.Time) time.Time {
	t = t.Truncate(time.Minute).Add(time.Minute)
	limit := t.Add(searchHorizon)
	for t.Before(limit) {
		if s.month&(1<<uint(t.Month())) == 0 {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location())
			continue
		}
		if !s.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, t.Location())
			continue
		}
		if s.hour&(1<<uint(t.Hour())) == 0 {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, t.Location())
			continue
		}
		if s.minute&(1<<uint(t.Minute())) == 0 {
			t = t.Add(time.Minute)
			continue
		}
		return t
	}
	return time.Time{}
}
