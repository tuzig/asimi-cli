package runners

import (
	"context"
	"log/slog"
	"os/exec"
	"time"
)

// IsPodmanAvailable checks if podman daemon is running AND the specified sandbox image exists.
// Pass the full image name (e.g., "localhost/asimi-sandbox-myproject:latest").
func IsPodmanAvailable(imageName string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "podman", "image", "exists", imageName)
	err := cmd.Run()
	if err != nil {
		slog.Debug("podman not available or image missing", "image", imageName, "error", err)
		return false
	}

	slog.Debug("podman available with image", "image", imageName)
	return true
}
