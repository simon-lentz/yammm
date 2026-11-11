package schema

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// timestampFault is why no Timestamp holds a declared format.
type timestampFault uint8

const (
	timestampFormatSound timestampFault = iota
	timestampFormatNotUTF8
	timestampFormatAbbreviation
	timestampFormatUnreadable
)

// timestampProbes are the instants a declared layout is judged by. Between
// them every field holds a value that a layout which drops it cannot rebuild.
// Neither offset holds seconds, since RFC 3339, the default layout, writes none.
var timestampProbes = [...]time.Time{
	time.Date(2026, time.August, 19, 13, 34, 56, 789123456, time.FixedZone("", 5*60*60+30*60)),
	time.Date(1950, time.November, 23, 0, 7, 8, 100000000, time.FixedZone("", -(9*60*60+30*60))),
}

// fieldLoss is how much of one field a layout drops.
type fieldLoss uint8

const (
	fieldKept fieldLoss = iota
	fieldPartLost
	fieldLost
)

// timestampLoss is what a value stored through one layout loses. Its zero
// value loses nothing.
type timestampLoss struct {
	year            fieldLoss // in part: the century
	month           bool
	day             bool
	hour            fieldLoss // in part: the half of the day
	minute          bool
	second          bool
	fractionDropped int       // the trailing fraction digits of nine that are lost
	offset          fieldLoss // in part: the minutes
}

// judgeTimestampLayout returns the fault that refuses layout, or what a value
// stored through it loses. It reads text as a Timestamp value is read, in UTC.
func judgeTimestampLayout(layout string) (timestampFault, timestampLoss) {
	switch {
	case !utf8.ValidString(layout):
		return timestampFormatNotUTF8, timestampLoss{}
	case writesZoneAbbreviation(layout):
		return timestampFormatAbbreviation, timestampLoss{}
	}
	var loss timestampLoss
	for _, probe := range timestampProbes {
		back, err := time.ParseInLocation(layout, probe.Format(layout), time.UTC)
		if err != nil {
			return timestampFormatUnreadable, timestampLoss{}
		}
		loss.add(probe, back)
	}
	return timestampFormatSound, loss
}

// writesZoneAbbreviation reports whether layout renders two zones that differ
// in name alone as two texts. An abbreviation names no one offset, and package
// time reads the text "UTC" there as UTC whatever numeric offset stands beside it.
func writesZoneAbbreviation(layout string) bool {
	in := func(name string) string {
		return time.Date(2006, time.January, 2, 15, 4, 5, 0, time.FixedZone(name, 60*60)).Format(layout)
	}
	return in("AAA") != in("BBB")
}

// add records what back, the probe as the layout read its own text of it, lost.
// It compares the wall clock in each instant's own zone, since a layout writes
// the wall clock and the offset apart.
func (l *timestampLoss) add(probe, back time.Time) {
	l.year = max(l.year, lossOf(back.Year() == probe.Year(), back.Year()%100 == probe.Year()%100))
	l.month = l.month || back.Month() != probe.Month()
	l.day = l.day || back.Day() != probe.Day()
	l.hour = max(l.hour, lossOf(back.Hour() == probe.Hour(), back.Hour()%12 == probe.Hour()%12))
	l.minute = l.minute || back.Minute() != probe.Minute()
	l.second = l.second || back.Second() != probe.Second()
	l.fractionDropped = max(l.fractionDropped, 9-fractionDigitsKept(probe.Nanosecond(), back.Nanosecond()))
	_, probeOffset := probe.Zone()
	_, backOffset := back.Zone()
	l.offset = max(l.offset, lossOf(backOffset == probeOffset, backOffset/(60*60) == probeOffset/(60*60)))
}

func lossOf(whole, part bool) fieldLoss {
	switch {
	case whole:
		return fieldKept
	case part:
		return fieldPartLost
	}
	return fieldLost
}

// fractionDigitsKept returns the count of leading fraction digits of probe
// that back holds: 9 when back is probe, n when back is probe cut to n digits,
// and 0 otherwise.
func fractionDigitsKept(probe, back int) int {
	unit := 1
	for digits := 9; digits >= 1; digits-- {
		if probe/unit*unit == back {
			return digits
		}
		unit *= 10
	}
	return 0
}

// fields names what the loss drops, in the order a timestamp is written.
func (l timestampLoss) fields() []string {
	var lost []string
	add := func(when bool, name string) {
		if when {
			lost = append(lost, name)
		}
	}
	add(l.year == fieldLost, "the year")
	add(l.year == fieldPartLost, "the century of the year")
	add(l.month, "the month")
	add(l.day, "the day")
	add(l.hour == fieldLost, "the hour")
	add(l.hour == fieldPartLost, "whether the hour is before or after noon")
	add(l.minute, "the minute")
	add(l.second, "the second")
	switch kept := 9 - l.fractionDropped; kept {
	case 9:
	case 0:
		lost = append(lost, "the fractional second")
	case 1:
		lost = append(lost, "the fractional second past 1 digit")
	default:
		lost = append(lost, fmt.Sprintf("the fractional second past %d digits", kept))
	}
	add(l.offset == fieldLost, "the UTC offset")
	add(l.offset == fieldPartLost, "the minutes of the UTC offset")
	return lost
}

// warning returns the W_TIMESTAMP_LOSSY_FORMAT message for layout, and false
// when the loss is none.
func (l timestampLoss) warning(layout string) (string, bool) {
	lost := l.fields()
	if len(lost) == 0 {
		return "", false
	}
	list := lost[0]
	if last := len(lost) - 1; last > 0 {
		list = strings.Join(lost[:last], ", ") + " and " + lost[last]
	}
	return fmt.Sprintf("timestamp format %q cannot represent an instant: a value stored through it loses %s", layout, list), true
}

// timestampFaultText words fault for format, and reports false for a sound one.
func timestampFaultText(format string, fault timestampFault) (string, bool) {
	//exhaustive:enforce
	switch fault {
	case timestampFormatSound:
		return "", false
	case timestampFormatNotUTF8:
		return fmt.Sprintf("timestamp format %q is not valid UTF-8", format), true
	case timestampFormatAbbreviation:
		return fmt.Sprintf("timestamp format %q holds a zone abbreviation, which names no one offset; state the zone as a numeric offset such as Z07:00", format), true
	case timestampFormatUnreadable:
		probe, text := unreadTimestampProbe(format)
		return fmt.Sprintf("timestamp format %q does not read back the text it writes: it writes %s as %q",
			format, probe.Format(time.RFC3339Nano), text), true
	}
	return fmt.Sprintf("timestamp format %q holds an unknown fault", format), true // unreachable: timestampFault has exactly the four values above
}

// unreadTimestampProbe returns the first probe whose text an unreadable layout
// does not read back, with that text: the last probe when no earlier is.
func unreadTimestampProbe(layout string) (time.Time, string) {
	last := len(timestampProbes) - 1
	for _, probe := range timestampProbes[:last] {
		text := probe.Format(layout)
		if _, err := time.ParseInLocation(layout, text, time.UTC); err != nil {
			return probe, text
		}
	}
	return timestampProbes[last], timestampProbes[last].Format(layout)
}
