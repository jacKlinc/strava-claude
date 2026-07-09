package shared

import (
	"encoding/json"
	"math"
)

// RawStream is one named time-series (e.g. "heartrate", "watts") as returned
// by an upstream API before downsampling. OriginalSize carries an upstream
// hint of the true series length when it can exceed len(Data) (e.g. Strava
// reports this even when the returned resolution truncates the data).
type RawStream struct {
	Type         string        `json:"type"`
	Data         []json.Number `json:"data"`
	OriginalSize int           `json:"original_size"`
}

type CondensedStreams struct {
	OriginalSize int                    `json:"original_size"`
	NumSamples   int                    `json:"num_samples"`
	SampleEvery  int                    `json:"sample_every"`
	Streams      map[string]interface{} `json:"streams"`
}

// CondenseStreams downsamples all stream arrays to at most maxPts points,
// saving Claude's context window on long activities.
func CondenseStreams(streams []RawStream, maxPts int) CondensedStreams {
	if len(streams) == 0 {
		return CondensedStreams{}
	}

	originalSize := 0
	for _, s := range streams {
		if s.OriginalSize > originalSize {
			originalSize = s.OriginalSize
		}
		if len(s.Data) > originalSize {
			originalSize = len(s.Data)
		}
	}

	step := 1
	if originalSize > maxPts {
		step = int(math.Ceil(float64(originalSize) / float64(maxPts)))
	}

	result := make(map[string]interface{}, len(streams))
	for _, s := range streams {
		sampled := make([]json.Number, 0, (len(s.Data)/step)+1)
		for i := 0; i < len(s.Data); i += step {
			sampled = append(sampled, s.Data[i])
		}
		result[s.Type] = sampled
	}

	numSamples := (originalSize + step - 1) / step

	return CondensedStreams{
		OriginalSize: originalSize,
		NumSamples:   numSamples,
		SampleEvery:  step,
		Streams:      result,
	}
}
