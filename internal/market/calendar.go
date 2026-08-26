package market

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"
)

const maxDaySearch = 30

type Session struct {
	Day   time.Time
	Open  time.Time
	Close time.Time
}

func (s Session) Duration() time.Duration { return s.Close.Sub(s.Open) }

func (s Session) Contains(ts time.Time) bool {
	return !ts.Before(s.Open) && ts.Before(s.Close)
}

type Holiday struct {
	Date     time.Time
	Name     string
	OpenMin  int
	CloseMin int
	Closed   bool
}

type Calendar struct {
	loc      *time.Location
	openMin  int
	closeMin int
	days     map[string]Holiday
	minYear  int
	maxYear  int
}

type calendarFile struct {
	Timezone string `json:"timezone"`
	Session  struct {
		Open  string `json:"open"`
		Close string `json:"close"`
	} `json:"session"`
	Holidays []struct {
		Date  string `json:"date"`
		Name  string `json:"name"`
		Open  string `json:"open"`
		Close string `json:"close"`
	} `json:"holidays"`
}

func LoadCalendar(fsys fs.FS, name string) (*Calendar, error) {
	raw, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", name, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	var file calendarFile
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", name, err)
	}

	loc, err := time.LoadLocation(file.Timezone)
	if err != nil {
		return nil, fmt.Errorf("%s: timezone %q: %w", name, file.Timezone, err)
	}

	openMin, err := parseClock(file.Session.Open)
	if err != nil {
		return nil, fmt.Errorf("%s: session.open: %w", name, err)
	}
	closeMin, err := parseClock(file.Session.Close)
	if err != nil {
		return nil, fmt.Errorf("%s: session.close: %w", name, err)
	}
	if closeMin <= openMin {
		return nil, fmt.Errorf("%s: session.close must be after session.open", name)
	}
	if len(file.Holidays) == 0 {
		return nil, fmt.Errorf("%s: holidays is empty", name)
	}

	calendar := &Calendar{
		loc:      loc,
		openMin:  openMin,
		closeMin: closeMin,
		days:     make(map[string]Holiday, len(file.Holidays)),
	}

	for i, entry := range file.Holidays {
		date, err := time.ParseInLocation(time.DateOnly, entry.Date, loc)
		if err != nil {
			return nil, fmt.Errorf("%s: holidays[%d]: date must be YYYY-MM-DD", name, i)
		}
		if entry.Name == "" {
			return nil, fmt.Errorf("%s: holidays[%d] (%s): name is required", name, i, entry.Date)
		}

		holiday := Holiday{
			Date:     date,
			Name:     entry.Name,
			OpenMin:  openMin,
			CloseMin: closeMin,
			Closed:   true,
		}

		if entry.Open != "" || entry.Close != "" {
			holiday.Closed = false
			if entry.Open != "" {
				if holiday.OpenMin, err = parseClock(entry.Open); err != nil {
					return nil, fmt.Errorf("%s: holidays[%d] (%s): open: %w", name, i, entry.Date, err)
				}
			}
			if entry.Close != "" {
				if holiday.CloseMin, err = parseClock(entry.Close); err != nil {
					return nil, fmt.Errorf("%s: holidays[%d] (%s): close: %w", name, i, entry.Date, err)
				}
			}
			if holiday.CloseMin <= holiday.OpenMin {
				return nil, fmt.Errorf("%s: holidays[%d] (%s): close must be after open", name, i, entry.Date)
			}
		}

		key := dateKey(date)
		if _, exists := calendar.days[key]; exists {
			return nil, fmt.Errorf("%s: duplicate holiday %s", name, entry.Date)
		}
		calendar.days[key] = holiday

		if calendar.minYear == 0 || date.Year() < calendar.minYear {
			calendar.minYear = date.Year()
		}
		if date.Year() > calendar.maxYear {
			calendar.maxYear = date.Year()
		}
	}

	return calendar, nil
}

func (c *Calendar) Location() *time.Location { return c.loc }

func (c *Calendar) Date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, c.loc)
}

func (c *Calendar) Years() (int, int) { return c.minYear, c.maxYear }

func (c *Calendar) RegularSession() time.Duration {
	return time.Duration(c.closeMin-c.openMin) * time.Minute
}

func (c *Calendar) Holidays() int { return len(c.days) }

func (c *Calendar) Covers(day time.Time) bool {
	year := day.In(c.loc).Year()
	return year >= c.minYear && year <= c.maxYear
}

func (c *Calendar) Holiday(day time.Time) (Holiday, bool) {
	holiday, ok := c.days[dateKey(day.In(c.loc))]
	return holiday, ok
}

func (c *Calendar) IsTradingDay(day time.Time) bool {
	_, ok := c.Session(day)
	return ok
}

func (c *Calendar) Session(day time.Time) (Session, bool) {
	midnight := c.midnight(day)

	if weekday := midnight.Weekday(); weekday == time.Saturday || weekday == time.Sunday {
		return Session{}, false
	}

	openMin, closeMin := c.openMin, c.closeMin
	if holiday, ok := c.days[dateKey(midnight)]; ok {
		if holiday.Closed {
			return Session{}, false
		}
		openMin, closeMin = holiday.OpenMin, holiday.CloseMin
	}

	return Session{
		Day:   midnight,
		Open:  c.at(midnight, openMin),
		Close: c.at(midnight, closeMin),
	}, true
}

func (c *Calendar) SessionAt(ts time.Time) (Session, bool) {
	session, ok := c.Session(ts)
	if !ok || !session.Contains(ts) {
		return Session{}, false
	}
	return session, true
}

func (c *Calendar) NextTradingDay(day time.Time) (time.Time, error) {
	cursor := c.midnight(day)
	for range maxDaySearch {
		cursor = cursor.AddDate(0, 0, 1)
		if c.IsTradingDay(cursor) {
			return cursor, nil
		}
	}
	return time.Time{}, fmt.Errorf("no trading day within %d days after %s", maxDaySearch, dateKey(c.midnight(day)))
}

func (c *Calendar) PrevTradingDay(day time.Time) (time.Time, error) {
	cursor := c.midnight(day)
	for range maxDaySearch {
		cursor = cursor.AddDate(0, 0, -1)
		if c.IsTradingDay(cursor) {
			return cursor, nil
		}
	}
	return time.Time{}, fmt.Errorf("no trading day within %d days before %s", maxDaySearch, dateKey(c.midnight(day)))
}

func (c *Calendar) TradingDays(from, to time.Time) []time.Time {
	days := []time.Time{}
	cursor := c.midnight(from)
	end := to.In(c.loc)

	for !cursor.After(end) {
		if c.IsTradingDay(cursor) {
			days = append(days, cursor)
		}
		cursor = cursor.AddDate(0, 0, 1)
	}

	return days
}

func (c *Calendar) midnight(day time.Time) time.Time {
	local := day.In(c.loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, c.loc)
}

func (c *Calendar) at(midnight time.Time, minutes int) time.Time {
	return time.Date(midnight.Year(), midnight.Month(), midnight.Day(), minutes/60, minutes%60, 0, 0, c.loc).UTC()
}

func parseClock(text string) (int, error) {
	hourText, minuteText, ok := strings.Cut(text, ":")
	if !ok {
		return 0, fmt.Errorf("%q is not a HH:MM time", text)
	}

	hour, err := strconv.Atoi(hourText)
	if err != nil || hour < 0 || hour > 23 {
		return 0, fmt.Errorf("%q is not a HH:MM time", text)
	}

	minute, err := strconv.Atoi(minuteText)
	if err != nil || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("%q is not a HH:MM time", text)
	}

	return hour*60 + minute, nil
}

func dateKey(t time.Time) string { return t.Format(time.DateOnly) }
