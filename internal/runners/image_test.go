package runners

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPodmanImageExistsErrorPodmanMachineDown(t *testing.T) {
	err := podmanImageExistsError(nil, []byte("Cannot connect to Podman. try `podman machine start`"), errors.New("exit status 125"), "test-image")
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := err.(PodmanUnavailableError); !ok {
		t.Fatalf("error = %T, want PodmanUnavailableError", err)
	}
	if !strings.Contains(err.Error(), "podman machine start") {
		t.Fatalf("error = %q, want podman machine start guidance", err)
	}
}

func TestPodmanImageExistsErrorMissingImage(t *testing.T) {
	// Create a temp dir with .agents/ so the error says "build-sandbox"
	// (without .agents/ it would say ":init" instead).
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".agents"), 0o755); err != nil {
		t.Fatalf("mkdir .agents: %v", err)
	}

	err := podmanImageExistsError(nil, nil, errors.New("exit status 1"), "test-image", dir)
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := err.(SandboxMissingError); !ok {
		t.Fatalf("error = %T, want SandboxMissingError", err)
	}
	if !strings.Contains(err.Error(), "just build-sandbox") {
		t.Fatalf("error = %q, want build-sandbox guidance", err)
	}
}

func TestPodmanImageExistsErrorPodmanTimeout(t *testing.T) {
	err := podmanImageExistsError(context.DeadlineExceeded, nil, context.DeadlineExceeded, "test-image")
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := err.(PodmanUnavailableError); !ok {
		t.Fatalf("error = %T, want PodmanUnavailableError", err)
	}
	if !strings.Contains(err.Error(), "podman machine start") {
		t.Fatalf("error = %q, want podman machine start guidance", err)
	}
}

func TestPodmanImageExistsErrorPodmanMissing(t *testing.T) {
	err := podmanImageExistsError(nil, nil, exec.ErrNotFound, "test-image")
	if err == nil {
		t.Fatal("expected error")
	}
	if _, ok := err.(PodmanUnavailableError); !ok {
		t.Fatalf("error = %T, want PodmanUnavailableError", err)
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("error = %q, want install guidance", err)
	}
}
