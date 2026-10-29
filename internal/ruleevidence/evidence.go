// Package ruleevidence binds local hints to observations and limits their life.
package ruleevidence

import "time"

type Origin struct {
	Session   string `json:"session"`
	Turn      int    `json:"turn,omitempty"`
	Sequence  int    `json:"sequence,omitempty"`
	Signature string `json:"signature"`
	At        int64  `json:"at"`
}

func (o Origin) Valid() bool {
	return o.Session != "" && o.Signature != "" && o.At > 0 && (o.Turn > 0 || o.Sequence > 0)
}

type Usage struct {
	Offered   int   `json:"offered"`
	Succeeded int   `json:"succeeded"`
	Failed    int   `json:"failed"`
	LastUsed  int64 `json:"last_used,omitempty"`
}

// Expired removes unsupported, old or repeatedly ineffective hints. A hint
// cannot extend its useful life merely by being offered repeatedly.
func Expired(o Origin, u Usage, now time.Time) bool {
	if !o.Valid() {
		return true
	}
	age := now.Sub(time.UnixMilli(o.At))
	return age > 90*24*time.Hour || u.Failed >= 3 && u.Succeeded == 0 || u.Offered >= 10 && u.Succeeded == 0 || age > 30*24*time.Hour && u.Offered == 0
}
