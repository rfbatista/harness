package domain

import "testing"

func TestServiceStatusMachine(t *testing.T) {
	all := []ServiceStatus{ServiceStopped, ServiceStarting, ServiceRunning, ServiceExited, ServiceFailed}
	allowed := map[[2]ServiceStatus]bool{
		{ServiceStopped, ServiceStarting}: true, {ServiceExited, ServiceStarting}: true, {ServiceFailed, ServiceStarting}: true,
		{ServiceStarting, ServiceRunning}: true, {ServiceStarting, ServiceFailed}: true, {ServiceStarting, ServiceStopped}: true,
		{ServiceRunning, ServiceExited}: true, {ServiceRunning, ServiceStopped}: true,
	}
	for _, from := range all {
		for _, to := range all {
			if got := from.CanMoveTo(to); got != allowed[[2]ServiceStatus{from, to}] {
				t.Errorf("%s -> %s = %v", from, to, got)
			}
		}
	}
	if !ServiceStarting.Active() || !ServiceRunning.Active() || ServiceExited.Active() || ServiceStopped.Active() {
		t.Error("only starting and running are active")
	}
}
