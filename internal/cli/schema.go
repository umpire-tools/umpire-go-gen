package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/umpire-tools/umpire-gen/pkg/schema"
)

// LoadSchema reads and parses a .umpire.json file from the given path.
// If the path is "-", the schema is read from stdin.
func LoadSchema(path string) (*schema.Schema, error) {
	var data []byte
	var err error

	if path == "-" {
		data, err = readStdin()
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}

	var s schema.Schema
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}

	return &s, nil
}

// readStdin reads all of stdin into a byte slice.
func readStdin() ([]byte, error) {
	data, err := os.ReadFile("/dev/stdin")
	if err != nil {
		return nil, err
	}
	return data, nil
}

// SchemaExists checks whether the input file exists.
func SchemaExists(path string) bool {
	if path == "-" {
		return true
	}
	_, err := os.Stat(path)
	return err == nil
}

// DefaultOutputPath returns the default output path derived from the input path.
// E.g. "checkout.umpire.json" -> "checkout_umpire.go"
func DefaultOutputPath(inputPath string) string {
	base := filepath.Base(inputPath)
	for _, suffix := range []string{".umpire.json", ".umpire", ".json"} {
		if strings.HasSuffix(base, suffix) {
			base = base[:len(base)-len(suffix)]
			break
		}
	}
	return base + "_umpire.go"
}
