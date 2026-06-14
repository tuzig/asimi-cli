package runners

import (
	"crypto/md5"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
)

func TestMd5Hash(t *testing.T) {
	// Verify deterministic hex output
	got := md5Hash("hello")
	expected := fmt.Sprintf("%x", md5.Sum([]byte("hello")))
	assert.Equal(t, expected, got, "md5Hash should produce standard hex MD5")

	// Same input must always produce the same output
	assert.Equal(t, md5Hash("hello"), md5Hash("hello"), "md5Hash must be deterministic")

	// Different inputs must produce different outputs
	assert.NotEqual(t, md5Hash("hello"), md5Hash("world"), "different inputs must yield different hashes")

	// Empty string
	assert.Equal(t, fmt.Sprintf("%x", md5.Sum(nil)), md5Hash(""), "md5Hash of empty string")
}

func TestSanitizePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"alphanumeric", "node_modules", "node_modules"},
		{"with slashes", "path/to/dir", "path_to_dir"},
		{"with dots", "yarn.lock", "yarn_lock"},
		{"mixed special chars", "a/b.c-d", "a_b_c_d"},
		{"already clean", "abc123", "abc123"},
		{"empty string", "", ""},
		{"only special chars", "/.-", "___"},
		{"uppercase mixed", "Foo/Bar.lock", "Foo_Bar_lock"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, sanitizePath(tt.input))
		})
	}
}

func TestOverlayVolumeName(t *testing.T) {
	absPath := "/home/user/projects/myapp"
	relPath := "node_modules"

	volumeName := fmt.Sprintf("asimi-overlay-%s-%s", md5Hash(absPath), sanitizePath(relPath))

	// Must have the correct prefix
	assert.True(t, strings.HasPrefix(volumeName, "asimi-overlay-"), "volume name must have asimi-overlay- prefix")

	// Must contain the hash portion
	hash := md5Hash(absPath)
	assert.Contains(t, volumeName, hash, "volume name must contain the project root hash")

	// Must contain the sanitized path
	sanitized := sanitizePath(relPath)
	assert.True(t, strings.HasSuffix(volumeName, "-"+sanitized), "volume name must end with -<sanitized-path>")

	// Must be deterministic
	volumeName2 := fmt.Sprintf("asimi-overlay-%s-%s", md5Hash(absPath), sanitizePath(relPath))
	assert.Equal(t, volumeName, volumeName2, "volume naming must be deterministic")

	// Different project roots must produce different volume names
	otherPath := "/home/user/projects/otherapp"
	volumeNameOther := fmt.Sprintf("asimi-overlay-%s-%s", md5Hash(otherPath), sanitizePath(relPath))
	assert.NotEqual(t, volumeName, volumeNameOther, "different project roots must yield different volume names")
}

func TestOverlayMountOrder(t *testing.T) {
	absPath := "/home/user/projects/myapp"

	// Build mounts in the same order as createContainer
	mounts := []spec.Mount{
		{
			Type:        "bind",
			Source:      absPath,
			Destination: absPath,
		},
	}

	// AdditionalMounts (none in this test, but the slot is after bind mount)

	// PlatformOverlays
	overlays := []string{"node_modules", "yarn.lock"}
	for _, relPath := range overlays {
		overlayDest := filepath.Join(absPath, relPath)
		volumeName := fmt.Sprintf("asimi-overlay-%s-%s", md5Hash(absPath), sanitizePath(relPath))
		mounts = append(mounts, spec.Mount{
			Type:        "volume",
			Source:      volumeName,
			Destination: overlayDest,
		})
	}

	// First mount must be the bind mount for the project root
	assert.Equal(t, "bind", mounts[0].Type, "first mount must be bind type")
	assert.Equal(t, absPath, mounts[0].Destination, "first mount must be project root")

	// Overlay mounts must come after the bind mount
	assert.Equal(t, len(overlays)+1, len(mounts), "total mounts = 1 bind + N overlays")
	for i, relPath := range overlays {
		m := mounts[i+1]
		assert.Equal(t, "volume", m.Type, "overlay mount %d must be volume type", i)
		assert.Equal(t, filepath.Join(absPath, relPath), m.Destination, "overlay mount %d destination", i)
		assert.True(t, strings.HasPrefix(m.Source, "asimi-overlay-"), "overlay mount %d source must have asimi-overlay- prefix", i)
	}
}
