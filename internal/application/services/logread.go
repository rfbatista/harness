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
	text, start, next, first, runID := s.logFor(svc).read(nil, ports.ServiceLogMaxRead, ports.ServiceLogMaxRead)
	newline := strings.HasSuffix(text, "\n")
	parts := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	truncated := false
	if len(parts) > lines {
		cut := len(parts) - lines
		start += int64(len(strings.Join(parts[:cut], "\n")) + 1)
		text = strings.Join(parts[cut:], "\n")
		if newline {
			text += "\n"
		}
	} else if start > first {
		// The window began after the log's first byte: earlier lines exist.
		truncated = true
	}
	return &ports.ServiceLogRead{Text: text, Offset: start, NextOffset: next, FirstOffset: first, RunID: runID}, truncated, nil
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
