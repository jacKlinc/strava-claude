package shared

import (
	"encoding/json"
	"testing"
)

func annotateOne(t *testing.T, body string) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal(AnnotateTemp([]byte(body)), &out); err != nil {
		t.Fatalf("annotated body is not valid JSON: %v", err)
	}
	return out
}

func TestAnnotateTempSource(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		want     string
		wantNote bool
	}{
		{
			name: "weather service populated",
			body: `{"has_weather":true,"average_weather_temp":14.2,"average_temp":30.6}`,
			want: TempSourceWeather,
		},
		{
			// The Garmin fenix 6 trail run that prompted this: a warm wrist under
			// a watch strap on a cool Vancouver afternoon.
			name:     "device sensor only",
			body:     `{"has_weather":false,"average_weather_temp":null,"average_temp":30.60625,"min_temp":29,"max_temp":33,"device_name":"Garmin fenix 6"}`,
			want:     TempSourceDevice,
			wantNote: true,
		},
		{
			name:     "has_weather true but no weather temp",
			body:     `{"has_weather":true,"average_weather_temp":null,"average_temp":30.6}`,
			want:     TempSourceDevice,
			wantNote: true,
		},
		{
			name: "no temperature at all",
			body: `{"has_weather":false,"average_weather_temp":null,"average_temp":null}`,
			want: TempSourceUnavailable,
		},
		{
			name: "temperature fields absent entirely",
			body: `{"id":"i123","type":"Run"}`,
			want: TempSourceUnavailable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := annotateOne(t, tc.body)
			if got["temp_source"] != tc.want {
				t.Fatalf("temp_source = %v, want %s", got["temp_source"], tc.want)
			}
			_, hasNote := got["temp_note"]
			if hasNote != tc.wantNote {
				t.Fatalf("temp_note present = %v, want %v", hasNote, tc.wantNote)
			}
		})
	}
}

func TestAnnotateTempArray(t *testing.T) {
	body := `[{"has_weather":true,"average_weather_temp":14.2},{"has_weather":false,"average_temp":30.6}]`

	var out []map[string]interface{}
	if err := json.Unmarshal(AnnotateTemp([]byte(body)), &out); err != nil {
		t.Fatalf("annotated body is not valid JSON: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 activities, got %d", len(out))
	}
	if out[0]["temp_source"] != TempSourceWeather {
		t.Fatalf("activity 0: temp_source = %v", out[0]["temp_source"])
	}
	if out[1]["temp_source"] != TempSourceDevice {
		t.Fatalf("activity 1: temp_source = %v", out[1]["temp_source"])
	}
}

func TestAnnotateTempPreservesOtherFields(t *testing.T) {
	body := `{"id":"i123","icu_training_load":87,"nested":{"a":[1,2,3]},"average_temp":30.6}`
	got := annotateOne(t, body)

	if got["id"] != "i123" {
		t.Fatalf("id lost: %v", got["id"])
	}
	if got["icu_training_load"] != float64(87) {
		t.Fatalf("icu_training_load lost: %v", got["icu_training_load"])
	}
	nested, ok := got["nested"].(map[string]interface{})
	if !ok || len(nested["a"].([]interface{})) != 3 {
		t.Fatalf("nested object lost: %v", got["nested"])
	}
}

func TestAnnotateTempPassesThroughUndecodable(t *testing.T) {
	for _, body := range []string{"", "   ", "not json", `{"unterminated":`, `"a string"`, `42`} {
		if got := string(AnnotateTemp([]byte(body))); got != body {
			t.Fatalf("input %q was modified to %q", body, got)
		}
	}
}
