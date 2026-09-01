package domain

// This file describes why a repository could not be read.
//
// It follows Diagnosis in state.go, which explains a failed cluster connection
// one layer at a time, and for the same reason: an engineer holding a single
// sentence about a failure has to guess which part of the path produced it,
// and guessing wrong costs an afternoon. The two are separate types rather
// than one because the layers are not the same layers and the outcomes are not
// the same outcomes — a git diagnosis has to be able to say "I do not know",
// and a cluster probe never does.

// GitStep names one thing checked on the way to reading a repository.
type GitStep string

// The steps, in the order they are attempted.
const (
	// StepGit is whether there is a git to run at all. A desktop application
	// on macOS is started by launchd and sees a narrower PATH than a terminal
	// does, so this is a real failure rather than a formality.
	StepGit GitStep = "git"

	// StepRemote is whether the Application's repository URL is one this
	// code will hand to git.
	StepRemote GitStep = "remote"

	// StepHost is whether anything is listening where the URL points.
	StepHost GitStep = "host"

	// StepAgent is whether an ssh-agent is visible to this process. It is
	// never a failure on its own: a key with no passphrase needs no agent.
	StepAgent GitStep = "agent"

	// StepRepository is whether the server will hand over this repository.
	// The one that usually carries the answer.
	StepRepository GitStep = "repository"
)

// GitStepResult is how one step turned out.
type GitStepResult string

// Step outcomes.
const (
	StepPassed GitStepResult = "passed"
	StepFailed GitStepResult = "failed"

	// StepUnknown is a check that ran and did not settle anything. It exists
	// so that "no agent, which may be fine" does not have to be recorded as
	// either a pass or a failure, both of which would be claims this code
	// cannot support.
	StepUnknown GitStepResult = "unknown"

	// StepSkipped is a check not attempted, because an earlier one failed in
	// a way that makes its answer meaningless. Asking a server for a
	// repository after failing to resolve its name would produce a second
	// failure and imply two problems where there is one.
	StepSkipped GitStepResult = "skipped"
)

// GitCheck is one line of the diagnosis.
type GitCheck struct {
	Step    GitStep       `json:"step"`
	Result  GitStepResult `json:"result"`
	Detail  string        `json:"detail,omitempty"`
	Elapsed int64         `json:"elapsedMs,omitempty"`
}

// GitFault is what the diagnosis concluded, for choosing which actions to
// offer rather than for wording.
type GitFault string

// The conclusions, none of which claims more than the checks established.
const (
	// FaultNone is every check passing. The repository was readable at the
	// moment it was asked, whatever happened when the comparison ran.
	FaultNone GitFault = ""

	FaultGitMissing   GitFault = "gitMissing"
	FaultBadRemote    GitFault = "badRemote"
	FaultUnreachable  GitFault = "unreachable"
	FaultTimeout      GitFault = "timeout"
	FaultHostKey      GitFault = "hostKey"
	FaultAuth         GitFault = "auth"
	FaultNoRepository GitFault = "noRepository"
	FaultUnknown      GitFault = "unknown"
)

// GitAccess is the whole answer to "why can I not read this repository".
type GitAccess struct {
	// Repository is the URL with anything credential-shaped removed, and
	// Transport is ssh or https. Both are shown, because the first question a
	// person asks of a failure like this is whether it is even pointed at the
	// repository they think it is.
	Repository string `json:"repository"`
	Transport  string `json:"transport"`

	Checks []GitCheck `json:"checks"`

	Fault GitFault `json:"fault"`

	// Summary is one sentence about what was concluded, and Causes are the
	// things that could produce it. There is more than one cause on purpose:
	// several servers answer "no such repository" and "not yours" with the
	// same words so that a stranger cannot discover private names by reading
	// error messages, and a panel that picked one of them would be inventing
	// the half the server withheld.
	Summary string   `json:"summary"`
	Causes  []string `json:"causes,omitempty"`

	// Command is the equivalent question for somebody to ask from their own
	// terminal. Running it answers something this application cannot answer
	// about itself: whether a shell can read what this window cannot.
	Command string `json:"command"`

	// Output is what git said, scrubbed, for the reader who wants to see it
	// rather than be told about it.
	Output string `json:"output,omitempty"`

	// SSHConfig is where this platform keeps the per-user ssh configuration
	// and whether there is one, so the panel can offer to show it without
	// claiming a file exists that does not.
	SSHConfig SSHConfigFile `json:"sshConfig"`
}

// SSHConfigFile is the location of ~/.ssh/config, resolved for this platform.
type SSHConfigFile struct {
	Path   string `json:"path,omitempty"`
	Exists bool   `json:"exists"`
}

// GitIdentity is who a git host says is calling.
//
// Asked separately from the diagnosis and only for ssh, because it is a
// different question: authentication is not authorisation, and a server that
// greets somebody by name has still said nothing about whether they may read
// any particular repository.
type GitIdentity struct {
	// Account is the name the server used, empty when the server did not give
	// one in a form this code recognises. It is never inferred.
	Account string `json:"account,omitempty"`

	// Greeting is what the server actually said, scrubbed.
	Greeting string `json:"greeting,omitempty"`

	// Summary is the sentence to show, which for an unrecognised reply says
	// that authentication worked and the identity could not be determined.
	Summary string `json:"summary"`
}
