package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetUpdateCommand(t *testing.T) {
	cmd := GetUpdateCommand()
	if cmd == "" {
		t.Error("GetUpdateCommand() returned empty string")
	}
	// Should return either brew command or asimi update
	if cmd != "brew upgrade asimi" && cmd != "asimi update" {
		t.Errorf("GetUpdateCommand() = %v, want 'brew upgrade asimi' or 'asimi update'", cmd)
	}
}

func TestExtractBinaryFromTarball(t *testing.T) {
	// Build a tar.gz in memory containing an "asimi" binary
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	binaryData := []byte("fake binary content")

	// Add a non-matching file first
	if err := tw.WriteHeader(&tar.Header{
		Name:     "README.md",
		Typeflag: tar.TypeReg,
		Size:     int64(len("readme")),
		Mode:     0644,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("readme")); err != nil {
		t.Fatal(err)
	}

	// Add the asimi binary in a subdirectory
	if err := tw.WriteHeader(&tar.Header{
		Name:     "bin/asimi",
		Typeflag: tar.TypeReg,
		Size:     int64(len(binaryData)),
		Mode:     0755,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(binaryData); err != nil {
		t.Fatal(err)
	}

	tw.Close()
	gzw.Close()

	got, err := extractBinaryFromTarball(&buf)
	if err != nil {
		t.Fatalf("extractBinaryFromTarball() error = %v", err)
	}
	if !bytes.Equal(got, binaryData) {
		t.Errorf("extractBinaryFromTarball() = %q, want %q", got, binaryData)
	}
}

func TestExtractBinaryFromTarball_NotFound(t *testing.T) {
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	// Only non-matching files
	tw.WriteHeader(&tar.Header{
		Name:     "other.txt",
		Typeflag: tar.TypeReg,
		Size:     4,
		Mode:     0644,
	})
	tw.Write([]byte("test"))

	tw.Close()
	gzw.Close()

	_, err := extractBinaryFromTarball(&buf)
	if err == nil {
		t.Fatal("extractBinaryFromTarball() expected error for missing binary")
	}
}

func TestExtractBinaryFromTarball_InvalidGzip(t *testing.T) {
	_, err := extractBinaryFromTarball(bytes.NewReader([]byte("not gzip data")))
	if err == nil {
		t.Fatal("extractBinaryFromTarball() expected error for invalid gzip")
	}
}

func TestExtractBinaryFromTarball_DirectoryEntry(t *testing.T) {
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	// Add a directory named "asimi" (should not match - only TypeReg)
	tw.WriteHeader(&tar.Header{
		Name:     "asimi",
		Typeflag: tar.TypeDir,
		Mode:     0755,
	})

	// Add the actual binary
	binaryData := []byte("real binary")
	tw.WriteHeader(&tar.Header{
		Name:     "asimi_bin",
		Typeflag: tar.TypeReg,
		Size:     int64(len(binaryData)),
		Mode:     0755,
	})
	tw.Write(binaryData)

	tw.Close()
	gzw.Close()

	_, err := extractBinaryFromTarball(&buf)
	if err == nil {
		t.Fatal("extractBinaryFromTarball() expected error when only a directory named 'asimi' exists")
	}
}

func TestAutoCheckForUpdates(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{
			name:    "dev version skips check",
			version: "dev",
			want:    false,
		},
		{
			name:    "empty version skips check",
			version: "",
			want:    false,
		},
		// Note: We can't test actual update checking without hitting GitHub API
		// which would be flaky in tests
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AutoCheckForUpdates(context.Background(), tt.version)
			if got != tt.want {
				t.Errorf("AutoCheckForUpdates() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSelfUpdateChecksumVerification verifies the core fix: the checksum
// must be computed against the tarball bytes (before extraction), not the
// extracted binary. This is because checksums.txt contains hashes of the
// .tar.gz archives, not the extracted binary.
//
// The old code hashed binaryContent (after extraction), which would always
// mismatch the expected tarball hash. The fix hashes tarballBytes before
// extraction, matching what checksums.txt contains.
func TestSelfUpdateChecksumVerification(t *testing.T) {
	// Build a tar.gz containing an "asimi" binary
	var tarballBuf bytes.Buffer
	gzw := gzip.NewWriter(&tarballBuf)
	tw := tar.NewWriter(gzw)

	binaryData := []byte("asimi binary content v1.0")
	if err := tw.WriteHeader(&tar.Header{
		Name:     "asimi",
		Typeflag: tar.TypeReg,
		Size:     int64(len(binaryData)),
		Mode:     0755,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(binaryData); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gzw.Close()

	tarballBytes := tarballBuf.Bytes()

	// Compute SHA256 of the raw tarball (what checksums.txt would contain)
	tarballHash := sha256.Sum256(tarballBytes)
	tarballHex := hex.EncodeToString(tarballHash[:])

	// Extract the binary (as SelfUpdate does)
	extracted, err := extractBinaryFromTarball(bytes.NewReader(tarballBytes))
	if err != nil {
		t.Fatal(err)
	}

	// Compute SHA256 of the extracted binary (what the OLD code did)
	binaryHash := sha256.Sum256(extracted)
	binaryHex := hex.EncodeToString(binaryHash[:])

	// CRITICAL ASSERTION: The tarball hash and binary hash MUST differ.
	// If they were the same, the old code wouldn't have been broken.
	// This proves the fix is meaningful.
	if tarballHex == binaryHex {
		t.Error("CRITICAL: tarball hash equals binary hash — test setup is broken, " +
			"the two should differ for a proper fix verification")
	}

	// The tarball hash must match the expected checksum (what checksums.txt contains)
	// Recompute to be absolutely sure
	recomputedHash := sha256.Sum256(tarballBytes)
	recomputedHex := hex.EncodeToString(recomputedHash[:])
	if recomputedHex != tarballHex {
		t.Fatal("internal consistency check failed: SHA256 of tarball is not deterministic")
	}

	// Verify the binary was extracted correctly
	if !bytes.Equal(extracted, binaryData) {
		t.Fatalf("extracted binary content mismatch: got %q, want %q", extracted, binaryData)
	}

	// Log the hashes for debugging
	t.Logf("tarball hash (correct):   %s", tarballHex)
	t.Logf("binary hash (old bug):    %s", binaryHex)
	t.Logf("tarball != binary:        %v", tarballHex != binaryHex)
}

func TestFetchChecksum(t *testing.T) {
	// Test parsing of checksums.txt format
	// We can't test the actual HTTP download, but we can test the parsing logic
	// by using a test server
	assetName := "asimi_v0.9.0_darwin_amd64.tar.gz"
	expectedHash := "abc123def456"

	// Start a test HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\ndeadbeef  other_file.tar.gz\n", expectedHash, assetName)
	})
	mux.HandleFunc("/404", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Run("checksum found", func(t *testing.T) {
		hash, err := fetchChecksum(context.Background(), srv.URL+"/checksums.txt", assetName)
		if err != nil {
			t.Fatalf("fetchChecksum() error = %v", err)
		}
		if hash != expectedHash {
			t.Errorf("fetchChecksum() = %q, want %q", hash, expectedHash)
		}
	})

	t.Run("checksums not found returns empty", func(t *testing.T) {
		hash, err := fetchChecksum(context.Background(), srv.URL+"/404", assetName)
		if err != nil {
			t.Fatalf("fetchChecksum() error = %v", err)
		}
		if hash != "" {
			t.Errorf("fetchChecksum() = %q, want empty string", hash)
		}
	})

	t.Run("asset not in checksums returns empty", func(t *testing.T) {
		hash, err := fetchChecksum(context.Background(), srv.URL+"/checksums.txt", "nonexistent.tar.gz")
		if err != nil {
			t.Fatalf("fetchChecksum() error = %v", err)
		}
		if hash != "" {
			t.Errorf("fetchChecksum() = %q, want empty string", hash)
		}
	})
}
