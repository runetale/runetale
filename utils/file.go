// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package utils

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
)

func AtomicWriteFile(filename string, data []byte, perm os.FileMode) (err error) {
	f, err := ioutil.TempFile(filepath.Dir(filename), filepath.Base(filename)+".tmp")
	if err != nil {
		return err
	}

	tmpName := f.Name()
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmpName)
		}
	}()

	if _, err := f.Write(data); err != nil {
		return err
	}

	if runtime.GOOS != "windows" {
		if err := f.Chmod(perm); err != nil {
			return err
		}
	}

	if err := f.Sync(); err != nil {
		return err
	}

	if err := f.Close(); err != nil {
		return err
	}

	return os.Rename(tmpName, filename)
}
