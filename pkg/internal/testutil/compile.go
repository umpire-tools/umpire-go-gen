package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// AssertGeneratedPackageCompiles writes source to a temporary module and runs
// `go test` against it, catching invalid generated Go instead of only checking
// emitted text.
func AssertGeneratedPackageCompiles(t *testing.T, source string) {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module generatedsmoke\n\ngo 1.22\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "generated.go"), []byte(source), 0644); err != nil {
		t.Fatalf("write generated.go: %v", err)
	}

	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GOTOOLCHAIN=local",
		"GOCACHE="+filepath.Join(dir, ".gocache"),
		"GOMODCACHE="+filepath.Join(dir, ".gomodcache"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated package did not compile: %v\n%s\n--- source ---\n%s", err, out, source)
	}
}
