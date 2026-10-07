package sessions

import "net/url"

// taskDesignHref is a task's design assets page (tasks.DesignHref; tasks
// imports this package, so the path is spelled here too).
func taskDesignHref(projectID, taskID string) string {
	return "/projects/" + url.PathEscape(projectID) + "/tasks/" + url.PathEscape(taskID) + "/design"
}
