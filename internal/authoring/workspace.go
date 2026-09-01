package authoring

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Workspace is the directory tree cdk8s authoring happens in.
//
// It lives in the cache directory rather than beside data.json for the same
// reason repository mirrors do: it is large, it is rebuildable, and a person
// who deletes it loses the time of the next install and nothing else.
//
// The shape is one shared dependency directory with the sessions inside it:
//
//	authoring/
//	  runtime/
//	    package.json      fixed, written by this application
//	    tsconfig.json     fixed, written by this application
//	    node_modules/     installed once by Prepare
//	    sessions/
//	      <id>/
//	        cdk8s.yaml
//	        main.ts
//	        dist/
//
// The sessions sit under the dependency directory on purpose. Node resolves
// `require` by walking up from the file, so a session finds runtime/node_modules
// without any environment variable, symlink or per-session install — which is
// what keeps `npm install` from running behind a Preview button.
type Workspace struct{ root string }

// NewWorkspace names the tree. The directory is not created until it is used.
func NewWorkspace(root string) *Workspace { return &Workspace{root: root} }

// runtimeDir is the shared dependency project.
func (w *Workspace) runtimeDir() string { return filepath.Join(w.root, "runtime") }

func (w *Workspace) sessionsDir() string { return filepath.Join(w.runtimeDir(), "sessions") }

// Prepared reports that the dependencies are installed.
//
// The marker is the installed package itself rather than a file this
// application writes, so a half-finished or hand-deleted node_modules reads as
// unprepared instead of as ready-and-broken.
func (w *Workspace) Prepared() bool {
	_, err := os.Stat(filepath.Join(w.runtimeDir(), "node_modules", "cdk8s", "package.json"))
	return err == nil
}

// dependencies are the packages the TypeScript surface may use.
//
// The list is fixed and written by this application. Nothing typed into the
// editor can add to it, which is the whole of the dependency policy for this
// slice: authoring a Kubernetes object needs cdk8s and the TypeScript that
// runs it, and a general-purpose package manager is a different product.
//
// The versions are ranges rather than exact pins because a lockfile pinned in
// a Go string is a lockfile nobody will ever update, and cdk8s 2.x has not
// broken ApiObject. The range is bounded at the major version so a future 3.0
// cannot arrive silently.
const packageJSON = `{
  "name": "biebie-kube-authoring",
  "private": true,
  "version": "0.0.0",
  "dependencies": {
    "cdk8s": "^2.68.0",
    "constructs": "^10.3.0"
  },
  "devDependencies": {
    "@types/node": "^20.11.0",
    "ts-node": "^10.9.2",
    "typescript": "^5.4.0"
  }
}
`

const tsconfigJSON = `{
  "compilerOptions": {
    "target": "ES2020",
    "module": "CommonJS",
    "lib": ["ES2020"],
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "moduleResolution": "node",
    "types": ["node"]
  }
}
`

// cdk8sYAML tells the cdk8s CLI how to run one session.
//
// `ts-node/register` rather than a compile step, so the file in the editor is
// the file that runs and a stack trace points at a line the person can see.
const cdk8sYAML = `language: typescript
app: node -r ts-node/register main.ts
output: dist
`

// ensureRuntime writes the fixed project files.
//
// They are rewritten on every call rather than written once. They are this
// application's files, not the user's, and a package.json edited by hand into
// something that no longer installs is a failure nobody would connect to the
// button they pressed.
func (w *Workspace) ensureRuntime() error {
	dir := w.runtimeDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create the authoring workspace: %w", err)
	}
	for name, body := range map[string]string{
		"package.json":  packageJSON,
		"tsconfig.json": tsconfigJSON,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}

// Session is one authoring attempt with a directory of its own.
type Session struct {
	ID  string
	dir string
}

// Dir is where this session's files live.
func (s *Session) Dir() string { return s.dir }

// NewSession makes a directory for one editor.
//
// The identifier is random rather than derived from anything the user typed.
// A session named after a namespace would be a path built from a value that
// arrived over the binding, and `../` is a legal namespace character in
// exactly the sense that matters here — it is not, but the code that would
// have to know that is code this avoids writing.
func (w *Workspace) NewSession() (*Session, error) {
	if err := w.ensureRuntime(); err != nil {
		return nil, err
	}

	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, fmt.Errorf("name a session: %w", err)
	}
	id := hex.EncodeToString(raw[:])

	dir := filepath.Join(w.sessionsDir(), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create a session directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cdk8s.yaml"), []byte(cdk8sYAML), 0o644); err != nil {
		return nil, fmt.Errorf("write cdk8s.yaml: %w", err)
	}
	return &Session{ID: id, dir: dir}, nil
}

// Session resolves an identifier the frontend sent back.
//
// The identifier crossed a binding, so it is treated as untrusted: it must be
// a plain name, and the directory it resolves to must be inside the sessions
// directory. Both checks are kept — the first rejects the obvious attempt and
// the second is what holds if the first is ever loosened.
func (w *Workspace) Session(id string) (*Session, error) {
	if id == "" {
		return nil, errors.New("no authoring session was named")
	}
	if id != filepath.Base(id) || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return nil, errors.New("that is not an authoring session")
	}

	dir := filepath.Join(w.sessionsDir(), id)
	if !within(w.sessionsDir(), dir) {
		return nil, errors.New("that is not an authoring session")
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, errors.New("that authoring session no longer exists")
	}
	return &Session{ID: id, dir: dir}, nil
}

// Discard removes one session's directory.
func (w *Workspace) Discard(id string) {
	session, err := w.Session(id)
	if err != nil {
		return
	}
	_ = os.RemoveAll(session.dir)
}

// within reports whether path is inside root, after both are resolved.
//
// filepath.Rel is used rather than a string prefix, because a prefix test says
// /tmp/authoring-evil is inside /tmp/authoring.
func within(root, path string) bool {
	root, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
