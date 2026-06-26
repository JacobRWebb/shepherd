package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
)

func TestShouldUpdate(t *testing.T) {
	cases := []struct {
		cur, latest string
		want        bool
	}{
		{"dev", "0.1.0", true},
		{"", "0.1.0", true},
		{"0.1.0", "0.1.0", false},
		{"v0.1.0", "0.1.0", false},
		{"0.1.0", "0.2.0", true},
	}
	for _, c := range cases {
		if got := shouldUpdate(c.cur, c.latest); got != c.want {
			t.Errorf("shouldUpdate(%q,%q) = %v, want %v", c.cur, c.latest, got, c.want)
		}
	}
}

func TestAssetName(t *testing.T) {
	if n := assetName("1.2.3"); !strings.HasPrefix(n, "shepherd_1.2.3_") {
		t.Errorf("assetName = %q", n)
	}
}

func TestIsBinaryName(t *testing.T) {
	for _, ok := range []string{"shepherd", "dir/shepherd", `a\shepherd.exe`} {
		if !isBinaryName(ok) {
			t.Errorf("isBinaryName(%q) = false", ok)
		}
	}
	if isBinaryName("README.md") {
		t.Errorf("README.md should not match")
	}
}

func TestExtractBinaryTarGz(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	bin := []byte("BINARY-CONTENT")
	_ = tw.WriteHeader(&tar.Header{Name: "README.md", Mode: 0o644, Size: 3})
	_, _ = tw.Write([]byte("doc"))
	_ = tw.WriteHeader(&tar.Header{Name: "shepherd", Mode: 0o755, Size: int64(len(bin))})
	_, _ = tw.Write(bin)
	_ = tw.Close()
	_ = gz.Close()

	got, err := extractBinary(buf.Bytes(), false)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "BINARY-CONTENT" {
		t.Errorf("got %q", got)
	}
}

func TestExtractBinaryZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("shepherd.exe")
	_, _ = f.Write([]byte("WIN-BINARY"))
	_ = zw.Close()

	got, err := extractBinary(buf.Bytes(), true)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "WIN-BINARY" {
		t.Errorf("got %q", got)
	}
}

func TestExtractBinaryMissing(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("notes.txt")
	_, _ = f.Write([]byte("x"))
	_ = zw.Close()
	if _, err := extractBinary(buf.Bytes(), true); err == nil {
		t.Errorf("expected error when binary is absent")
	}
}
