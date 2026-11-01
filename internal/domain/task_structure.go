package domain

// TaskRef names a task in the structure of another: its id, title and status.
type TaskRef struct {
	ID     string       `json:"id"`
	Title  string       `json:"title"`
	Status TicketStatus `json:"status"`
}

// TaskChild is a feature task's child as an agent reads it: archived ones are
// listed too, flagged.
type TaskChild struct {
	TaskRef
	Archived bool `json:"archived"`
}

// TaskStructure is what an agent is told of how its task relates to others.
// It is derived when read, never stored. Parent is set only on a child task;
// Children is never nil, and empty unless the task is a feature task.
type TaskStructure struct {
	Parent   *TaskRef    `json:"parent"`
	Children []TaskChild `json:"children"`
}
