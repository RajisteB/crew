package run

import "fmt"

// FormatEvent renders one event as a terminal line. `crew run` prints these
// live and `crew run status` prints them.
func FormatEvent(ev Event) string {
	ts := ev.Time.Format("15:04:05")
	switch ev.Kind {
	case EvStart:
		return fmt.Sprintf("%s ▶ run %s started at %s", ts, ev.Msg, ev.Station)
	case EvStepStart:
		return fmt.Sprintf("%s → %s (step %d)", ts, ev.Station, ev.Count)
	case EvStepEnd:
		return fmt.Sprintf("%s ✓ %s: %s", ts, ev.Station, ev.Msg)
	case EvRetry:
		return fmt.Sprintf("%s ↻ %s: reply rejected (%s); retrying once", ts, ev.Station, ev.Msg)
	case EvRoute:
		limit := ""
		if ev.Max != "" {
			limit = fmt.Sprintf(" (%d/%s)", ev.Count, ev.Max)
		}
		return fmt.Sprintf("%s   %s --%s--> %s%s", ts, ev.Station, ev.Route, ev.Msg, limit)
	case EvNote:
		if ev.Station == "" {
			return fmt.Sprintf("%s   note: %s", ts, ev.Msg)
		}
		return fmt.Sprintf("%s   note [%s]: %s", ts, ev.Station, ev.Msg)
	case EvDrift:
		return fmt.Sprintf("%s ⚠ %s; sending work to %s", ts, ev.Msg, ev.Station)
	case EvApproval:
		return fmt.Sprintf("%s ⏸ waiting for approval: %s", ts, ev.Msg)
	case EvMessage:
		return fmt.Sprintf("%s ✉ message delivered to %s: %s", ts, ev.Station, ev.Msg)
	case EvGoto:
		return fmt.Sprintf("%s ↪ %s", ts, ev.Msg)
	case EvInterrupted:
		return fmt.Sprintf("%s ■ %s (crew run resume continues it)", ts, ev.Msg)
	case EvDone:
		return fmt.Sprintf("%s ■ done: %s", ts, ev.Msg)
	case EvFailed:
		return fmt.Sprintf("%s ✗ failed at %s: %s", ts, ev.Station, ev.Msg)
	}
	return fmt.Sprintf("%s %s %s %s", ts, ev.Kind, ev.Station, ev.Msg)
}
