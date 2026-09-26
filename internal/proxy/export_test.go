package proxy

import "time"

// SetClock fixes the time the visit log reads, for tests of its windows.
func (p *Proxy) SetClock(now func() time.Time) { p.clock = now }
