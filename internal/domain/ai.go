package domain

// AICliInfo describes one local AI CLI BiebieKube can invoke.
type AICliInfo struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Command      string   `json:"command,omitempty"`
	RunArgs      []string `json:"runArgs,omitempty"`
	Available    bool     `json:"available"`
	Path         string   `json:"path,omitempty"`
	Version      string   `json:"version,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	Custom       bool     `json:"custom,omitempty"`
	Acknowledged bool     `json:"acknowledged"`
}

// AICustomCommand is a user-defined executable and argument template.
//
// Args may include the literal placeholder {{PROMPT}}; when present the full
// message is passed on stdin instead and that token is omitted from argv.
type AICustomCommand struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Path  string   `json:"path"`
	Args  []string `json:"args"`
}

// AISettings persists Ask AI preferences.
type AISettings struct {
	DefaultCli   string              `json:"defaultCli,omitempty"`
	Custom       []AICustomCommand   `json:"custom,omitempty"`
	Acknowledged []string            `json:"acknowledged,omitempty"`
}

// AskAIRequest starts a headless CLI run for one resource.
type AskAIRequest struct {
	ClusterID string      `json:"clusterId"`
	Ref       ResourceRef `json:"ref"`
	Prompt    string      `json:"prompt"`
	CliID     string      `json:"cliId"`
}

// AskAISession identifies an in-flight Ask AI run.
type AskAISession struct {
	SessionID string `json:"sessionId"`
}

// AIContextSection is one part of the message Ask AI would send.
type AIContextSection struct {
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Bytes    int    `json:"bytes"`
	Included bool   `json:"included"`
}

// AIContextPreview tells the user exactly what leaves the app before they ask.
type AIContextPreview struct {
	Resource   ResourceRef        `json:"resource"`
	Sections   []AIContextSection `json:"sections"`
	Scrubbed   []string           `json:"scrubbed"`
	TotalBytes int                `json:"totalBytes"`
	Truncated  bool               `json:"truncated"`
	Prompt     string             `json:"prompt"`
}

// AIStreamPayload is the common shape for ai:* events.
type AIStreamPayload struct {
	SessionID string `json:"sessionId"`
	Text      string `json:"text,omitempty"`
	Error     string `json:"error,omitempty"`
}
