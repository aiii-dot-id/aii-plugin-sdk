package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func addLicenseFiles(install map[string][]byte, dir string) error {
	for _, name := range []string{"LICENSE", "NOTICE", "THIRD_PARTY_LICENSES"} {
		path := filepath.Join(dir, name)
		before, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if !before.Mode().IsRegular() {
			return fmt.Errorf("%s: must be a regular file, not a symbolic link or other file type", name)
		}
		raw, err := readLicenseFile(path, before)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		install[name] = raw
	}
	return nil
}

func readLicenseFile(path string, before os.FileInfo) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, fmt.Errorf("file changed while opening")
	}
	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	after, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !after.Mode().IsRegular() || !os.SameFile(opened, after) || before.Size() != int64(len(raw)) || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return nil, fmt.Errorf("file changed while reading")
	}
	return raw, nil
}
