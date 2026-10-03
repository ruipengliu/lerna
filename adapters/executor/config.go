package executor

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/ruipengliu/lerna/api"
)

func LoadConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	validator, err := api.NewValidator(api.SchemaFor[Config]())
	if err != nil {
		return Config{}, err
	}
	if err = validator.Validate(b); err != nil {
		return Config{}, err
	}
	var c Config
	err = api.Decode(b, &c)
	return c, err
}
func SaveConfig(path string, c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".executor-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(append(b, '\n'))
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}
