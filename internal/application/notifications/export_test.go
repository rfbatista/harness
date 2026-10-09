package notifications

import "time"

// SetDeliverTimeout bounds each Deliver call by d instead of 30 s.
func (s *Service) SetDeliverTimeout(d time.Duration) { s.delivery.timeout = d }
