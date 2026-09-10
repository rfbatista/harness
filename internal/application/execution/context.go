package execution

import (
	"fmt"
	"strings"

	"operators-mcp/internal/application/blueprint"
	"operators-mcp/internal/domain"
)

// ExecutionContext holds the fully resolved state needed for a task execution.
type ExecutionContext struct {
	Zone          *domain.Zone
	Project       *domain.Project
	Agent         *domain.Agent
	SystemPrompt  string
	AllowedRoot   string
	IgnoredPaths  []string
	ZonePattern   string
	ExplicitPaths []string
}

// ContextResolver builds ExecutionContext from project + zone identifiers.
type ContextResolver struct {
	blueprintSvc *blueprint.Service
}

// NewContextResolver returns a resolver backed by the blueprint service.
func NewContextResolver(svc *blueprint.Service) *ContextResolver {
	return &ContextResolver{blueprintSvc: svc}
}

// Resolve builds the full execution context.
// projectID is required and validated against the zone's project.
// agentID is optional — if empty, the zone selects the agent.
func (r *ContextResolver) Resolve(projectID, zoneID, agentID string) (*ExecutionContext, error) {
	project := r.blueprintSvc.GetProject(projectID)
	if project == nil {
		return nil, &domain.StructuredError{Code: "PROJECT_NOT_FOUND", Message: "project not found"}
	}

	zone := r.blueprintSvc.GetZone(zoneID)
	if zone == nil {
		return nil, &domain.StructuredError{Code: "ZONE_NOT_FOUND", Message: "zone not found"}
	}

	if zone.ProjectID != projectID {
		return nil, &domain.StructuredError{
			Code:    "CROSS_PROJECT_ACCESS",
			Message: "zone does not belong to the specified project",
		}
	}

	agent, err := r.selectAgent(zone, agentID)
	if err != nil {
		return nil, err
	}

	r.blueprintSvc.ResolveAgentPrompt(agent)

	systemPrompt := r.buildSystemPrompt(project, zone, agent)

	return &ExecutionContext{
		Zone:          zone,
		Project:       project,
		Agent:         agent,
		SystemPrompt:  systemPrompt,
		AllowedRoot:   project.RootDir,
		IgnoredPaths:  project.IgnoredPaths,
		ZonePattern:   zone.Pattern,
		ExplicitPaths: zone.ExplicitPaths,
	}, nil
}

func (r *ContextResolver) selectAgent(zone *domain.Zone, agentID string) (*domain.Agent, error) {
	if agentID != "" {
		for i := range zone.AssignedAgents {
			if zone.AssignedAgents[i].ID == agentID {
				full := r.blueprintSvc.GetAgent(agentID)
				if full == nil {
					return nil, &domain.StructuredError{Code: "AGENT_NOT_FOUND", Message: "agent not found"}
				}
				return full, nil
			}
		}
		return nil, &domain.StructuredError{
			Code:    "AGENT_NOT_IN_ZONE",
			Message: fmt.Sprintf("agent %s is not assigned to zone %s", agentID, zone.ID),
		}
	}

	switch len(zone.AssignedAgents) {
	case 0:
		return nil, &domain.StructuredError{Code: "NO_AGENTS", Message: "zone has no assigned agents"}
	case 1:
		full := r.blueprintSvc.GetAgent(zone.AssignedAgents[0].ID)
		if full == nil {
			return nil, &domain.StructuredError{Code: "AGENT_NOT_FOUND", Message: "agent not found"}
		}
		return full, nil
	default:
		return nil, &domain.StructuredError{
			Code:    "MULTIPLE_AGENTS",
			Message: "zone has multiple agents, specify agent_id",
		}
	}
}

func (r *ContextResolver) buildSystemPrompt(project *domain.Project, zone *domain.Zone, agent *domain.Agent) string {
	var sb strings.Builder

	if agent.Prompt != nil && agent.Prompt.Content != "" {
		sb.WriteString(agent.Prompt.Content)
		sb.WriteString("\n\n")
	}

	for _, rule := range zone.Rules {
		if rule.Content != "" {
			sb.WriteString("## Constraint: ")
			sb.WriteString(rule.Name)
			sb.WriteString("\n")
			sb.WriteString(rule.Content)
			sb.WriteString("\n\n")
		}
	}

	sb.WriteString("## Execution Boundary\n")
	sb.WriteString(fmt.Sprintf("You operate within zone '%s' of project '%s'.\n", zone.Name, project.Name))
	sb.WriteString(fmt.Sprintf("Working directory: %s\n", project.RootDir))

	if zone.Pattern != "" {
		sb.WriteString(fmt.Sprintf("You may only access files matching pattern `%s`", zone.Pattern))
		if len(zone.ExplicitPaths) > 0 {
			sb.WriteString(fmt.Sprintf(" or in: [%s]", strings.Join(zone.ExplicitPaths, ", ")))
		}
		sb.WriteString(".\n")
	} else if len(zone.ExplicitPaths) > 0 {
		sb.WriteString(fmt.Sprintf("You may only access files in: [%s].\n", strings.Join(zone.ExplicitPaths, ", ")))
	}

	if len(project.IgnoredPaths) > 0 {
		sb.WriteString(fmt.Sprintf("Paths in the ignore list are forbidden: [%s].\n", strings.Join(project.IgnoredPaths, ", ")))
	}

	return sb.String()
}
