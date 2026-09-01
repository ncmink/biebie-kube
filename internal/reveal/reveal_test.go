package reveal

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheFileIsHandedOverAsOneArgumentRatherThanAsACommand(t *testing.T) {
	// The path comes from a home directory, and home directories have spaces
	// in them. Anything that built a command string here would break on the
	// first person called "Anna Müller" and would be a place to inject on the
	// first person whose account name was chosen less innocently.
	var got string
	revealer := &Revealer{show: func(_ context.Context, path string) error {
		got = path
		return nil
	}}

	want := filepath.Join(string(filepath.Separator), "Users", "Anna Müller", ".ssh", "config")
	if err := revealer.Reveal(t.Context(), want); err != nil {
		t.Fatalf("Reveal: %v", err)
	}
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestOnlyAnAbsolutePathIsShown(t *testing.T) {
	// A relative path would be resolved against whatever directory this
	// process happens to be in, which for a desktop application is not a
	// directory anybody chose.
	called := false
	revealer := &Revealer{show: func(context.Context, string) error {
		called = true
		return nil
	}}

	if err := revealer.Reveal(t.Context(), filepath.Join(".ssh", "config")); err == nil {
		t.Fatal("a relative path was shown")
	}
	if called {
		t.Fatal("the file manager was asked for a path that had not been checked")
	}
}

func TestAFileManagerThatWillNotStartIsReported(t *testing.T) {
	// A button that silently did nothing would be worse than one that says it
	// could not, because the reader would go looking for a window that was
	// never opened.
	revealer := &Revealer{show: func(context.Context, string) error {
		return errors.New("xdg-open: not found")
	}}

	err := revealer.Reveal(t.Context(), filepath.Join(string(filepath.Separator), "home", "someone", ".ssh", "config"))
	if err == nil {
		t.Fatal("a failed launch reported success")
	}
	// The reader is told which file, not which of this package's helpers.
	if !strings.Contains(err.Error(), "config") {
		t.Fatalf("error = %q", err)
	}
}
