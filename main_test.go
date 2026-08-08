package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/umpire-tools/umpire-go-gen/internal/cli"
)

func TestFormatSource_ValidGo(t *testing.T) {
	// Input with non-canonical formatting should be normalized.
	input := "package main\n\n\nfunc  main( )  {\n\t_ = 1\n}\n"
	got := formatSource(input)
	if string(got) == input {
		t.Fatal("expected input to be reformatted, got identical output")
	}
	// gofmt should produce canonical spacing.
	if !bytes.Contains(got, []byte("func main() {")) {
		t.Errorf("expected canonical formatting, got:\n%s", got)
	}
}

func TestFormatSource_InvalidGoFallback(t *testing.T) {
	// Input that cannot be parsed as Go should fall back to the raw source.
	invalid := "this is not valid Go code {{{"
	got := formatSource(invalid)
	if string(got) != invalid {
		t.Errorf("expected unformatted fallback for invalid Go, got:\n%s", got)
	}
}

func TestWriteOutput_OutputFileCreatesParentDirs(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "deep", "nested", "result_gen.go")

	cfg := &cli.Config{
		InputPath:  "schema.umpire.json",
		OutputFile: outFile,
	}

	content := []byte("package test\n")
	if err := writeOutput(content, cfg); err != nil {
		t.Fatalf("writeOutput error: %v", err)
	}

	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("file content = %q, want %q", got, content)
	}
}

func TestWriteOutput_DefaultDirDerivesFilename(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &cli.Config{
		InputPath: "checkout.umpire.json",
		OutputDir: tmpDir,
	}

	content := []byte("package test\n")
	if err := writeOutput(content, cfg); err != nil {
		t.Fatalf("writeOutput error: %v", err)
	}

	expected := filepath.Join(tmpDir, "checkout_gen.go")
	got, err := os.ReadFile(expected)
	if err != nil {
		t.Fatalf("ReadFile error for %s: %v", expected, err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("file content = %q, want %q", got, content)
	}
}

func TestWriteOutput_OutputFileOverridesOutputDir(t *testing.T) {
	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "custom_name.go")

	cfg := &cli.Config{
		InputPath:  "checkout.umpire.json",
		OutputDir:  "/should/not/be/used",
		OutputFile: outFile,
	}

	content := []byte("package test\n")
	if err := writeOutput(content, cfg); err != nil {
		t.Fatalf("writeOutput error: %v", err)
	}

	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("file content = %q, want %q", got, content)
	}
}

func TestWriteOutput_StdinWritesToStdout(t *testing.T) {
	// Capture stdout.
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe error: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	cfg := &cli.Config{
		InputPath: "-",
	}

	content := []byte("package test\n")
	if err := writeOutput(content, cfg); err != nil {
		t.Fatalf("writeOutput error: %v", err)
	}

	w.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("ReadFrom error: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), content) {
		t.Errorf("stdout = %q, want %q", buf.Bytes(), content)
	}
}
