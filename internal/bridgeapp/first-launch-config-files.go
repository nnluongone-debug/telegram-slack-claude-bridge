package bridgeapp

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// WriteMissingConfig creates .env and projects.json in dir from the examples
// when they don't exist yet, so a freshly downloaded app has files to edit.
// .env holds tokens and is readable by its owner only. It returns the names
// of the files it created.
func WriteMissingConfig(dir string, env, projects []byte) ([]string, error) {
	var created []string
	for _, f := range []struct {
		name string
		data []byte
		perm os.FileMode
	}{{".env", env, 0o600}, {"projects.json", projects, 0o600}} {
		file, err := os.OpenFile(filepath.Join(dir, f.name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, f.perm)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return created, err
		}
		_, err = file.Write(f.data)
		if cerr := file.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(file.Name()) // a partial file would block the next attempt
			return created, err
		}
		created = append(created, f.name)
	}
	return created, nil
}
