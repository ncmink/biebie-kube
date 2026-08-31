package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Cache is a set of bare mirrors of the repositories that have been read.
//
// A mirror rather than a working copy, for two reasons. Nothing is checked
// out, so there is no file on disk that somebody could edit by accident and
// believe they had changed something; and no branch is ever switched, so a
// repository a person also has open in an editor is not disturbed by this
// application reading a different revision of it.
type Cache struct {
	root string

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// NewCache stores mirrors under one directory.
func NewCache(root string) *Cache {
	return &Cache{root: root, locks: map[string]*sync.Mutex{}}
}

// DefaultRoot is where mirrors live.
//
// The cache directory rather than the configuration directory beside
// data.json: a mirror is large, it can be rebuilt from the remote at any time,
// and a person who deletes it loses nothing but the speed of the next read.
// Configuration that is deleted is configuration that is gone.
func DefaultRoot() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "biebie-kube", "git"), nil
}

// Tree is one repository at one resolved commit.
type Tree struct {
	dir    string
	commit string
}

// Commit is the object every read in this tree came from.
func (t *Tree) Commit() string { return t.commit }

// Entry is one file in a tree.
type Entry struct {
	Path string
	Size int64
}

// Open makes one repository readable at one revision.
//
// The revision is resolved to a commit here and everything afterwards reads
// that commit rather than the name it arrived as. `main` moves while a person
// is looking at it, and an answer that says which commit it came from is one
// that can be checked afterwards.
func (c *Cache) Open(ctx context.Context, remote Remote, revision Revision) (*Tree, error) {
	unlock := c.lock(remote)
	defer unlock()

	dir := filepath.Join(c.root, directoryFor(remote))
	if err := c.mirror(ctx, dir, remote); err != nil {
		return nil, err
	}
	if err := fetch(ctx, dir, remote); err != nil {
		return nil, err
	}

	commit, err := resolve(ctx, dir, revision)
	if err != nil {
		return nil, err
	}
	return &Tree{dir: dir, commit: commit}, nil
}

// mirror creates the bare clone if this repository has not been read before.
func (c *Cache) mirror(ctx context.Context, dir string, remote Remote) error {
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return &Error{Kind: ErrorFailed, Message: "The cache directory could not be created."}
	}

	// A partial clone leaves the file contents on the server until one is
	// actually read, which on a repository with years of history is the
	// difference between seconds and minutes. Servers that do not offer the
	// filter refuse the option outright rather than ignoring it, so the plain
	// clone is tried after it.
	_, err := run(ctx, "", "clone", "--bare", "--filter=blob:none", "--", remote.String(), dir)
	if err == nil {
		return nil
	}

	// A refusal that is about the credentials or the host will not read
	// differently the second time, and trying again would only double how long
	// the person waits to be told.
	var failure *Error
	if errors.As(err, &failure) && failure.Kind != ErrorFailed {
		_ = os.RemoveAll(dir)
		return err
	}

	_ = os.RemoveAll(dir)
	if _, err := run(ctx, "", "clone", "--bare", "--", remote.String(), dir); err != nil {
		// A half-written mirror would be taken for a finished one by the
		// stat above, and every later read would fail in a way that looks
		// like the repository rather than like this.
		_ = os.RemoveAll(dir)
		return err
	}
	return nil
}

// fetch brings the mirror up to date.
//
// The refspec is written out rather than left to the mirror's configuration,
// because a revision that did not exist when the clone was made — a branch
// created this morning — is exactly the one somebody is asking about.
func fetch(ctx context.Context, dir string, remote Remote) error {
	_, err := run(ctx, dir, "fetch", "--prune", "--prune-tags", "--force", "--",
		remote.String(), "+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*")
	return err
}

// resolve turns a branch, tag or commit into the commit it names.
func resolve(ctx context.Context, dir string, revision Revision) (string, error) {
	// `^{commit}` peels an annotated tag to the commit underneath it, so a
	// revision that names a tag reads its tree rather than the tag object.
	// The caret is written here rather than accepted from the cluster, which
	// is why ParseRevision refuses it in input.
	out, err := run(ctx, dir, "rev-parse", "--verify", "--quiet", "--end-of-options",
		revision.String()+"^{commit}")
	missing := &Error{
		Kind:    ErrorNoRevision,
		Message: fmt.Sprintf("This repository has no branch, tag or commit called %q.", revision),
	}
	if err != nil {
		// `--quiet` is what makes rev-parse exit without saying anything when
		// the name resolves to nothing, so an unexplained failure here means
		// exactly one thing. Anything git did bother to explain is passed on
		// as it was.
		var failure *Error
		if errors.As(err, &failure) && failure.Kind == ErrorFailed {
			return "", missing
		}
		return "", err
	}

	commit := strings.TrimSpace(string(out))
	if commit == "" {
		return "", missing
	}
	return commit, nil
}

// List names the files under one directory of the tree, with their sizes.
//
// Sizes come back with the names because the caller reads what it finds, and
// a repository holding a generated bundle of every CRD in the world should
// cost a skipped entry rather than a hundred megabytes through a pipe.
func (t *Tree) List(ctx context.Context, dir Path) ([]Entry, error) {
	args := []string{"ls-tree", "-r", "--long", "-z", "--full-tree", "--end-of-options", t.commit}
	if !dir.IsRoot() {
		args = append(args, "--", dir.String()+"/")
	}

	out, err := run(ctx, t.dir, args...)
	if err != nil {
		return nil, err
	}

	var entries []Entry
	// `-z` separates records with NUL rather than newline, which is what makes
	// a file whose name contains a newline parse as one record.
	for _, record := range strings.Split(string(out), "\x00") {
		if entry, ok := parseEntry(record); ok {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// parseEntry reads one `ls-tree --long` record: mode, type, object and size,
// then a tab, then the name.
func parseEntry(record string) (Entry, bool) {
	head, name, found := strings.Cut(record, "\t")
	if !found || name == "" {
		return Entry{}, false
	}
	fields := strings.Fields(head)
	if len(fields) != 4 || fields[1] != "blob" {
		// Submodules arrive as `commit` and have no contents to read here.
		return Entry{}, false
	}
	size, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return Entry{}, false
	}
	return Entry{Path: name, Size: size}, true
}

// Read returns one file's contents at this tree's commit.
func (t *Tree) Read(ctx context.Context, file string) ([]byte, error) {
	// The argument begins with a resolved commit hash, so a file whose name
	// starts with a dash cannot turn into an option.
	return run(ctx, t.dir, "cat-file", "blob", t.commit+":"+file)
}

// lock serialises work on one mirror.
//
// Two drawers opened at once must not clone the same repository into the same
// directory twice, and the second one usually wants what the first is already
// fetching.
func (c *Cache) lock(remote Remote) func() {
	key := directoryFor(remote)

	c.mu.Lock()
	if c.locks == nil {
		c.locks = map[string]*sync.Mutex{}
	}
	repository, ok := c.locks[key]
	if !ok {
		repository = &sync.Mutex{}
		c.locks[key] = repository
	}
	c.mu.Unlock()

	repository.Lock()
	return repository.Unlock
}

// directoryFor names a mirror's directory by the hash of its URL.
//
// A URL contains slashes, colons and occasionally a case that two filesystems
// disagree about, none of which survive being used as a directory name. A hash
// has one spelling everywhere.
func directoryFor(remote Remote) string {
	sum := sha256.Sum256([]byte(remote.String()))
	return hex.EncodeToString(sum[:16])
}
