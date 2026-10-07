package components

// ReviewsSeed is what the review band and the project's review inbox
// (reviewsInbox, web/src/modules/reviews) start from: the names the page
// knows, to say who a request is about, which task it is on and which
// documents it links. The requests themselves are read from the API.
type ReviewsSeed struct {
	ProjectID string `json:"project_id"`
	// TicketID is the task page's task; "" on the project's inbox.
	TicketID string `json:"ticket_id,omitempty"`
	// Sessions names sessions by id: "go-developer · Server: architect channel".
	Sessions map[string]string `json:"sessions"`
	// Tasks names tasks by id, with their page.
	Tasks map[string]ReviewTask `json:"tasks"`
	// Documents names documents by id.
	Documents map[string]string `json:"documents"`
}

// ReviewTask is a task as the reviews name it.
type ReviewTask struct {
	Title string `json:"title"`
	Href  string `json:"href"`
}
