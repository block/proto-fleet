package modes

import "errors"

type Candidate struct {
	DeviceIdentifier string
	CurrentPowerW    float64
	EfficiencyJH     *float64
	ReasonSelected   string
}

type Mode interface {
	Select(candidates []Candidate) ([]Candidate, error)
}

func totalKW(candidates []Candidate) float64 {
	var total float64
	for _, candidate := range candidates {
		total += candidate.CurrentPowerW / 1000
	}
	return total
}

var ErrNoCurtailableCandidates = errors.New("no curtailable candidates")
