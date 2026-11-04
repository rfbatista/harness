package agentisolation

import (
	"operators-mcp/internal/domain"
	"operators-mcp/internal/ports"
)

// AttributionView is the Agent attribution section (#attribution): the two
// saved choices and the JSON preview that goes with them.
type AttributionView struct {
	State domain.AgentAttribution
}

// AttributionSeed is what the section's component takes over from: the body
// of GET /api/get_agent_attribution.
type AttributionSeed struct {
	domain.AgentAttribution
	Preview string `json:"preview"`
}

// NewAttributionView reads the saved attribution; without settings it is
// Claude default.
func NewAttributionView(settings ports.SettingsReader) AttributionView {
	if settings == nil {
		return AttributionView{}
	}
	return AttributionView{State: domain.ParseAgentAttribution(settings.Setting(domain.SettingAgentAttribution))}
}

// Seed is the section's seed.
func (v AttributionView) Seed() AttributionSeed {
	return AttributionSeed{AgentAttribution: v.State, Preview: v.State.Preview()}
}

// Preview is the JSON agents will get, for the first paint.
func (v AttributionView) Preview() string { return v.State.Preview() }
