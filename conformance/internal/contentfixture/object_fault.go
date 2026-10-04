package contentfixture

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
)

var exactObjectName = regexp.MustCompile(`^[a-f0-9]{64}$`)

// WriteIndependentObject is an owned native-byte fault/control seam. It cannot
// address an arbitrary path, and every file's first Close cause stays owned.
func (w *World) WriteIndependentObject(name string, bytes []byte) {
	w.t.Helper()
	if !exactObjectName.MatchString(name) || w.closing {
		w.t.Fatal("invalid exact owned object fault")
	}
	file, err := os.OpenFile(filepath.Join(w.Directory, name), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		w.t.Fatal(err)
	}
	closeFile := w.ownSetupFile(file)
	if err = w.register("fixture_object_file " + w.Directory + "/" + name); err != nil {
		w.t.Fatal(errors.Join(err, closeFile()))
	}
	_, writeErr := file.Write(bytes)
	if err = errors.Join(writeErr, file.Sync(), closeFile()); err != nil {
		w.t.Fatal(err)
	}
	directory, err := os.Open(w.Directory)
	if err != nil {
		w.t.Fatal(err)
	}
	closeDirectory := w.ownSetupFile(directory)
	if err = errors.Join(directory.Sync(), closeDirectory()); err != nil {
		w.t.Fatal(err)
	}
}
