// SPDX-License-Identifier: MPL-2.0
package client

import (
	"encoding/json"
	"errors"
)

// Missing count is supported. When a collection declares a total, require it to
// remain consistent and match the completed unique listing before using absence.
// Concurrent list changes therefore fail closed rather than erase state.
type paginationTotal struct {
	value int
	known bool
}

func (p *paginationTotal) observe(raw json.RawMessage, seen int, last bool) error {
	if len(raw) > 0 {
		var count int
		if string(raw) == "null" || json.Unmarshal(raw, &count) != nil || count < 0 || count > 10000 || p.known && p.value != count {
			return errors.New("Iru returned invalid or changing pagination totals (content withheld)")
		}
		p.value, p.known = count, true
	}
	if p.known && (seen > p.value || last && seen != p.value) {
		return errors.New("Iru pagination total does not match the complete listing (content withheld)")
	}
	return nil
}
