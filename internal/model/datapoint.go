package model

type DataPoint struct {
	Name     string `json:"name"`
	Value    any    `json:"value"`
	Unit     string `json:"unit,omitempty"`
	Quality  string `json:"quality"`
	Severity string `json:"severity,omitempty"`
}

const (
	QualityGood    = "good"
	QualityBad     = "bad"
	QualityUnknown = "unknown"
)
