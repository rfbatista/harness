package productowner

import (
	"context"
	"fmt"
	"strings"

	"operators-mcp/internal/domain"
)

// SearchConversations implements ports.ProductOwner. The words are the
// caller's: this expands nothing, and only the product owner expands its own
// queries.
func (s *Service) SearchConversations(ctx context.Context, projectID, query string) ([]domain.ProductOwnerSearchMatch, error) {
	if _, err := s.projects.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	switch {
	case query == "":
		return nil, invalidInput("q is empty")
	case len([]rune(query)) > domain.ProductOwnerMaxSearch:
		return nil, invalidInput(fmt.Sprintf("q is over %d characters", domain.ProductOwnerMaxSearch))
	}
	return s.store.SearchConversations(ctx, projectID, query, domain.ProductOwnerSearchLimit)
}
