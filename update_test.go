package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
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
