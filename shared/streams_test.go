package shared

import (
	"encoding/json"
	"strconv"
	"testing"
)

func genData(n int) []json.Number {
	out := make([]json.Number, n)
	for i := range out {
		out[i] = json.Number(strconv.Itoa(i))
	}
	return out
}

func TestCondenseStreamsEmpty(t *testing.T) {
	got := CondenseStreams(nil, 200)
	if got.OriginalSize != 0 || got.NumSamples != 0 || got.SampleEvery != 0 || got.Streams != nil {
		t.Fatalf("expected zero value for empty input, got %+v", got)
	}
}

func TestCondenseStreamsBelowThreshold(t *testing.T) {
	streams := []RawStream{{Type: "time", Data: genData(50)}}
	got := CondenseStreams(streams, 200)

	if got.OriginalSize != 50 {
		t.Fatalf("expected original_size 50, got %d", got.OriginalSize)
	}
	if got.SampleEvery != 1 {
		t.Fatalf("expected sample_every 1 (no downsampling), got %d", got.SampleEvery)
	}
	if got.NumSamples != 50 {
		t.Fatalf("expected num_samples 50, got %d", got.NumSamples)
	}
	sampled, ok := got.Streams["time"].([]json.Number)
	if !ok || len(sampled) != 50 {
		t.Fatalf("expected 50 samples for 'time', got %v", got.Streams["time"])
	}
}

func TestCondenseStreamsAboveThreshold(t *testing.T) {
	streams := []RawStream{{Type: "time", Data: genData(450)}}
	got := CondenseStreams(streams, 200)

	if got.OriginalSize != 450 {
		t.Fatalf("expected original_size 450, got %d", got.OriginalSize)
	}
	if got.SampleEvery != 3 {
		t.Fatalf("expected sample_every 3 (ceil(450/200)), got %d", got.SampleEvery)
	}
	if got.NumSamples != 150 {
		t.Fatalf("expected num_samples 150, got %d", got.NumSamples)
	}
	sampled, ok := got.Streams["time"].([]json.Number)
	if !ok || len(sampled) != 150 {
		t.Fatalf("expected 150 samples, got %v", len(sampled))
	}
	if sampled[0] != "0" || sampled[1] != "3" {
		t.Fatalf("expected strided samples starting 0,3,..., got %v, %v", sampled[0], sampled[1])
	}
}

func TestCondenseStreamsUsesMaxDataLength(t *testing.T) {
	streams := []RawStream{
		{Type: "time", Data: genData(300)},
		{Type: "heartrate", Data: genData(100)},
	}
	got := CondenseStreams(streams, 200)

	if got.OriginalSize != 300 {
		t.Fatalf("expected original_size to be the max stream length (300), got %d", got.OriginalSize)
	}
}

func TestCondenseStreamsUsesOriginalSizeHint(t *testing.T) {
	// Strava-style: upstream reports a true original_size larger than the
	// (already-truncated) data actually returned.
	streams := []RawStream{{Type: "time", Data: genData(50), OriginalSize: 900}}
	got := CondenseStreams(streams, 200)

	if got.OriginalSize != 900 {
		t.Fatalf("expected original_size to honor the upstream hint (900), got %d", got.OriginalSize)
	}
}
