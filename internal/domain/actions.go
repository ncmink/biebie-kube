package domain

// ResourceAction is a change to a live object the UI can make without editing
// its manifest.
//
// The set is closed and deliberately small. Every entry is something kubectl
// exposes as a verb rather than as an edit, which is the line: a scale and a
// cordon are operations an engineer performs, while everything else about an
// object is a manifest they own.
type ResourceAction string

// The actions a cluster can be asked for.
const (
	ActionScale    ResourceAction = "scale"
	ActionRestart  ResourceAction = "restart"
	ActionCordon   ResourceAction = "cordon"
	ActionUncordon ResourceAction = "uncordon"
	ActionSuspend  ResourceAction = "suspend"
	ActionResume   ResourceAction = "resume"
	ActionTrigger  ResourceAction = "trigger"
)

// ActionRequest asks for one action against one object.
type ActionRequest struct {
	Ref    ResourceRef    `json:"ref"`
	Action ResourceAction `json:"action"`

	// Replicas is read only by ActionScale.
	Replicas int32 `json:"replicas"`
}

// ActionResult is what to tell the engineer happened.
//
// The sentence is built where the action ran rather than by the UI, because
// only that side knows what the cluster did with the request: triggering a
// cron job creates a job whose name nothing above could have known.
type ActionResult struct {
	Message string `json:"message"`
}

// Supports reports whether this kind accepts an action.
//
// Both halves of a toggle are listed on the kind — a node offers cordon and
// uncordon — because which one applies is a property of the object in front of
// the user, not of the type.
func (i KindInfo) Supports(action ResourceAction) bool {
	for _, candidate := range i.Actions {
		if candidate == action {
			return true
		}
	}
	return false
}
