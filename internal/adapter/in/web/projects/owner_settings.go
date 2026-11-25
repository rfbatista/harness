package projects

import "encoding/json"

// ownerSettingsProps are the Product owner section's island props: the project, as JSON that
// is safe inside a script.
func ownerSettingsProps(projectID string) string {
	props, _ := json.Marshal(struct {
		ProjectID string `json:"project_id"`
	}{projectID})
	return string(props)
}
