// Package pullrequests is the pages' side of a workspace's pull request: the
// history page's toolbar control and the task page's badges, rendered for the
// first paint from ports.PullRequests (which never waits on the host) and
// handed to the browser's pullrequests module with a seed in the Web UI
// contract's JSON shape.
package pullrequests
