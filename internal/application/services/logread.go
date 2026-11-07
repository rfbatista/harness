package services

import (
	"context"
	"strings"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// logFor is the service's log, opened on first use in this instance.
func (s *Service) logFor(svc *domain.Service) *logFile {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if l := s.logs[svc.ID]; l != nil {
		return l
	}
	runID := ""
	if svc.Run != nil {
		runID = svc.Run.ID
	}
	l := openLog(s.LogPath(svc.ID), s.opts.LogCap, runID)
	s.logs[svc.ID] = l
	return l
}

// forgetLog drops the open log of a deleted service.
func (s *Service) forgetLog(serviceID string) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	delete(s.logs, serviceID)
}

// ReadServiceLog implements ports.ServiceManager.
func (s *Service) ReadServiceLog(_ context.Context, serviceID string, offset *int64) (*ports.ServiceLogRead, error) {
	svc, err := s.store.GetService(serviceID)
	if err != nil {
		return nil, err
	}
	text, start, next, first, runID := s.logFor(svc).read(offset, ports.ServiceLogTailBytes, ports.ServiceLogMaxRead)
	return &ports.ServiceLogRead{Text: text, Offset: start, NextOffset: next, FirstOffset: first, RunID: runID}, nil
}

// ReadServiceLogTail implements ports.ServiceManager.
func (s *Service) ReadServiceLogTail(_ context.Context, serviceID string, lines int) (*ports.ServiceLogRead, bool, error) {
	svc, err := s.store.GetService(serviceID)
	if err != nil {
		return nil, false, err
	}
	lg := s.logFor(svc)
	text, start, next, first, runID := lg.read(nil, ports.ServiceLogMaxRead, ports.ServiceLogMaxRead)
	// The window cut the log when it begins after the log's first byte.
	cut := start > first
	if cut && !startsALine(lg, start) {
		// The window begins inside a line: that partial line is not given.
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text, start = text[i+1:], start+int64(i+1)
		} else {
			text, start = "", next
		}
	}
	newline := strings.HasSuffix(text, "\n")
	parts := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(parts) >= lines {
		// Enough lines: older ones are left out by the caller's choice, not cut.
		cut = false
	}
	if len(parts) > lines {
		skip := len(parts) - lines
		start += int64(len(strings.Join(parts[:skip], "\n")) + 1)
		text = strings.Join(parts[skip:], "\n")
		if newline {
			text += "\n"
		}
	}
	return &ports.ServiceLogRead{Text: text, Offset: start, NextOffset: next, FirstOffset: first, RunID: runID}, cut, nil
}

// startsALine says whether the byte before offset ends a line.
func startsALine(lg *logFile, offset int64) bool {
	prev, _, _, _, _ := lg.read(&[]int64{offset - 1}[0], 0, 1)
	return prev == "\n"
}

// FollowServiceLog implements ports.ServiceManager.
func (s *Service) FollowServiceLog(ctx context.Context, serviceID string, from *int64) (<-chan ports.ServiceLogEvent, error) {
	svc, err := s.store.GetService(serviceID)
	if err != nil {
		return nil, err
	}
	l := s.logFor(svc)
	ch := l.follow(from)
	go func() {
		<-ctx.Done()
		l.unfollow(ch)
	}()
	return ch, nil
}
