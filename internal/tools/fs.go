package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var homeDir string

func init() {
	homeDir, _ = os.UserHomeDir()
}

func validatePath(p string) (string, error) {
	resolved := filepath.Clean(filepath.Join(homeDir, p))
	if !strings.HasPrefix(resolved, homeDir) {
		return "", fmt.Errorf("path outside home directory")
	}
	return resolved, nil
}

func ReadFile(path string) (string, error) {
	resolved, err := validatePath(path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func ListDir(path string) ([]map[string]any, error) {
	resolved, err := validatePath(path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		info, _ := e.Info()
		result = append(result, map[string]any{
			"name":  e.Name(),
			"isDir": e.IsDir(),
			"size":  info.Size(),
		})
	}
	return result, nil
}

func WriteFile(path string, content string) error {
	resolved, err := validatePath(path)
	if err != nil {
		return err
	}
	return os.WriteFile(resolved, []byte(content), 0644)
}
