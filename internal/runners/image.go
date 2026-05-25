package runners

import (
	"context"
	"errors"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// CheckSandboxImageAvailable checks if the podman daemon is running and
// the specified sandbox image exists. Pass the full image name
// (e.g., "localhost/asimi-sandbox-myproject:latest").
func CheckSandboxImageAvailable(ctx context.Context, imageName string) error {
	cmd := exec.CommandContext(ctx, "podman", "image", "exists", imageName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return podmanImageExistsError(ctx.Err(), output, err, imageName)
	}

	return nil
}

// IsPodmanAvailable checks if podman daemon is running AND the specified sandbox image exists.
// Pass the full image name (e.g., "localhost/asimi/sandbox/myproject:latest").
func IsPodmanAvailable(imageName string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := CheckSandboxImageAvailable(ctx, imageName); err != nil {
		slog.Debug("podman not available or image missing", "image", imageName, "error", err)
		return false
	}

	slog.Debug("podman available with image", "image", imageName)
	return true
}

func podmanImageExistsError(ctxErr error, output []byte, err error, imageName string, projectRoot ...string) error {
	if errors.Is(ctxErr, context.DeadlineExceeded) {
		return PodmanUnavailableError{Reason: "Podman did not respond. Start it with `podman machine start`, then try again."}
	}
	if errors.Is(err, exec.ErrNotFound) {
		return PodmanUnavailableError{Reason: "Podman is not installed or not on PATH. Install Podman, then run `:init`."}
	}

	message := strings.TrimSpace(string(output))
	if isPodmanConnectionError(message) {
		return PodmanUnavailableError{Reason: "Podman is not running. Start it with `podman machine start`, then try again."}
	}

	pr := ""
	if len(projectRoot) > 0 {
		pr = projectRoot[0]
	}
	return SandboxMissingError{ImageName: imageName, ProjectRoot: pr}
}

func isPodmanConnectionError(message string) bool {
	lower := strings.ToLower(message)
	needles := []string{
		"cannot connect to podman",
		"unable to connect",
		"connection refused",
		"podman machine",
		"podman.sock",
		"no such file or directory",
	}
	for _, needle := range needles {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}
