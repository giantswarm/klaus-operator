package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"testing"
)

func TestExtractTarGz(t *testing.T) {
	t.Run("extracts wanted files", func(t *testing.T) {
		buf := buildTestTarGz(t, map[string]string{
			"personality.yaml": "description: test\n",
			"SOUL.md":          "# Test Soul\n",
			"extra.txt":        "ignored",
		})

		files, err := extractTarGz(buf, "personality.yaml", "SOUL.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if string(files["personality.yaml"]) != "description: test\n" {
			t.Errorf("personality.yaml = %q", files["personality.yaml"])
		}
		if string(files["SOUL.md"]) != "# Test Soul\n" {
			t.Errorf("SOUL.md = %q", files["SOUL.md"])
		}
		if _, exists := files["extra.txt"]; exists {
			t.Error("extra.txt should not be extracted")
		}
	})

	t.Run("handles dot-slash prefix in tar paths", func(t *testing.T) {
		buf := buildTestTarGzWithPaths(t, map[string]string{
			"./personality.yaml": "description: prefixed\n",
			"./SOUL.md":          "# Prefixed Soul\n",
		})

		files, err := extractTarGz(buf, "personality.yaml", "SOUL.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(files["personality.yaml"]) != "description: prefixed\n" {
			t.Errorf("personality.yaml = %q", files["personality.yaml"])
		}
		if string(files["SOUL.md"]) != "# Prefixed Soul\n" {
			t.Errorf("SOUL.md = %q", files["SOUL.md"])
		}
	})

	t.Run("missing optional file is ok", func(t *testing.T) {
		buf := buildTestTarGz(t, map[string]string{
			"personality.yaml": "description: no soul\n",
		})

		files, err := extractTarGz(buf, "personality.yaml", "SOUL.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, exists := files["personality.yaml"]; !exists {
			t.Error("personality.yaml should be extracted")
		}
		if _, exists := files["SOUL.md"]; exists {
			t.Error("SOUL.md should not exist")
		}
	})

	t.Run("stops early when all wanted files found", func(t *testing.T) {
		buf := buildTestTarGz(t, map[string]string{
			"personality.yaml": "first",
			"SOUL.md":          "second",
			"should-not-read":  "third",
		})

		files, err := extractTarGz(buf, "personality.yaml", "SOUL.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 2 {
			t.Errorf("expected 2 files, got %d", len(files))
		}
	})
}

func TestCleanTarPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"personality.yaml", "personality.yaml"},
		{"./personality.yaml", "personality.yaml"},
		{"/personality.yaml", "personality.yaml"},
		{"./SOUL.md", "SOUL.md"},
		{"SOUL.md", "SOUL.md"},
	}
	for _, tt := range tests {
		got := cleanTarPath(tt.input)
		if got != tt.want {
			t.Errorf("cleanTarPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func buildTestTarGz(t *testing.T, files map[string]string) *bytes.Buffer {
	t.Helper()
	return buildTestTarGzWithPaths(t, files)
}

func buildTestTarGzWithPaths(t *testing.T, files map[string]string) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Size: int64(len(content)),
			Mode: 0644,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("writing tar header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("writing tar content: %v", err)
		}
	}

	tw.Close()
	gw.Close()
	return &buf
}
