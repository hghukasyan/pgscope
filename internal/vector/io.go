package vector

import (
	"os"

	"gopkg.in/yaml.v3"
)

func loadBytes(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func unmarshalYAML(data []byte, v any) error {
	return yaml.Unmarshal(data, v)
}
