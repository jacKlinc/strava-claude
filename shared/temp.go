package shared

import (
	"bytes"
	"encoding/json"
)

// Values for the injected temp_source field.
const (
	TempSourceWeather     = "weather_service"
	TempSourceDevice      = "device_sensor"
	TempSourceUnavailable = "unavailable"
)

// TempNote travels with activities whose only temperature is a device reading,
// so the caveat reaches consumers that never saw the tool description.
const TempNote = "average_temp/min_temp/max_temp are device sensor readings (wrist-worn devices approximate skin temperature); no weather-service ambient temperature is available for this activity."

// activity keeps unknown upstream fields as raw JSON so annotation is additive:
// everything we didn't look at round-trips byte-for-byte.
type activity map[string]json.RawMessage

// AnnotateTemp injects temp_source into an intervals.icu activity response —
// either a single activity object or an array of them — so consumers can tell a
// device sensor reading apart from a weather-service ambient temperature instead
// of guessing from the field names. Input that doesn't decode as one of those two
// shapes is returned unchanged.
func AnnotateTemp(body []byte) []byte {
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	if len(trimmed) == 0 {
		return body
	}

	if trimmed[0] == '[' {
		var acts []activity
		if err := json.Unmarshal(trimmed, &acts); err != nil {
			return body
		}
		for _, a := range acts {
			annotate(a)
		}
		return remarshal(acts, body)
	}

	var act activity
	if err := json.Unmarshal(trimmed, &act); err != nil {
		return body
	}
	annotate(act)
	return remarshal(act, body)
}

func remarshal(v interface{}, orig []byte) []byte {
	out, err := json.Marshal(v)
	if err != nil {
		return orig
	}
	return out
}

func annotate(a activity) {
	if a == nil {
		return
	}

	switch {
	case isTrue(a["has_weather"]) && present(a["average_weather_temp"]):
		a["temp_source"] = json.RawMessage(`"` + TempSourceWeather + `"`)
	case present(a["average_temp"]):
		a["temp_source"] = json.RawMessage(`"` + TempSourceDevice + `"`)
		note, _ := json.Marshal(TempNote)
		a["temp_note"] = note
	default:
		a["temp_source"] = json.RawMessage(`"` + TempSourceUnavailable + `"`)
	}
}

// present reports whether a field was set to something other than JSON null.
func present(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func isTrue(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("true"))
}
