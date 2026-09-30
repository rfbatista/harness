// Package settings is the settings bounded context: global key/value
// configuration. It validates and stores values and announces every change as
// a SettingsChanged event; what a change means — republishing skills under a
// new root, say — is the business of the context that owns it.
package settings

import (
	"context"
	"errors"

	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

var (
	_ ports.Settings = (*Service)(nil)
)

// Service implements the settings use cases.
type Service struct {
	repo   ports.SettingsRepository // nil: settings unavailable
	events ports.EventPublisher     // nil: changes are not announced
}

// NewService returns the settings context over repo, announcing changes on events.
func NewService(repo ports.SettingsRepository, events ports.EventPublisher) *Service {
	return &Service{repo: repo, events: events}
}

// GetSettings returns all settings.
func (s *Service) GetSettings() (map[string]string, error) {
	if s.repo == nil {
		return map[string]string{}, nil
	}
	all, err := s.repo.All()
	if err != nil {
		return nil, err
	}
	if all == nil {
		all = map[string]string{}
	}
	return all, nil
}

// Setting returns one setting, or "" when it is unset or unreadable.
func (s *Service) Setting(key string) string {
	if s.repo == nil {
		return ""
	}
	v, err := s.repo.Get(key)
	if err != nil {
		return ""
	}
	return v
}

// UpdateSettings validates every value first, then stores them and publishes
// SettingsChanged for each value that actually changed. A subscriber's failure
// is returned after the values are stored: the setting did change.
func (s *Service) UpdateSettings(values map[string]string) (map[string]string, error) {
	if s.repo == nil {
		return nil, &domain.StructuredError{Code: "INVALID_INPUT", Message: "settings are not available"}
	}
	for key, value := range values {
		if err := domain.ValidateSetting(key, value); err != nil {
			return nil, err
		}
	}

	var changed []domain.Event
	for key, value := range values {
		old := s.Setting(key)
		if err := s.repo.Set(key, value); err != nil {
			return nil, err
		}
		if old != value {
			changed = append(changed, domain.SettingsChanged{Key: key, Old: old, New: value})
		}
	}

	var errs []error
	if s.events != nil && len(changed) > 0 {
		errs = append(errs, s.events.Publish(context.Background(), changed...))
	}
	all, err := s.GetSettings()
	errs = append(errs, err)
	if err := errors.Join(errs...); err != nil {
		return all, err
	}
	return all, nil
}
