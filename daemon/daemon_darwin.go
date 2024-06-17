// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package daemon

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/runetale/runetale/runelog"
)

type daemon struct {
	// binary path
	binPath string
	// daemon file path
	daemonFilePath string
	// daemon name
	serviceName string
	// daemon system confi
	systemConfig string

	runelog *runelog.Runelog
}

func newDaemon(
	binPath, serviceName, daemonFilePath, systemConfig string,
	wl *runelog.Runelog,
) Daemon {
	return &daemon{
		binPath:        binPath,
		serviceName:    serviceName,
		daemonFilePath: daemonFilePath,
		systemConfig:   systemConfig,

		runelog: wl,
	}
}

func (d *daemon) Install() (err error) {
	defer func() {
		if os.Getuid() != 0 && err != nil {
			d.runelog.Logger.Errorf("run it again with sudo privileges: %s", err.Error())
			err = fmt.Errorf("run it again with sudo privileges: %s", err.Error())
		}
	}()

	err = d.Uninstall()
	if err != nil {
		d.runelog.Logger.Errorf("uninstallation of %s failed. plist file is here %s. because %s\n", d.serviceName, d.daemonFilePath, err.Error())
		return err
	}

	// seriously copy the binary
	// - create binary path => "/usr/local/bin/<binary-name>"
	// - execution path at build time => exeFile
	// - create tmp file => "/usr/local/bin/<binary-name>.tmp"
	// - copy exeFile to tmp file
	// - setting permisiion to tmpBin
	// - tmpBin to a real executable file

	if err := os.MkdirAll(filepath.Dir(d.binPath), 0755); err != nil {
		d.runelog.Logger.Errorf("failed to create %s. because %s\n", d.binPath, err.Error())
		return err
	}

	exePath, err := os.Executable()
	if err != nil {
		d.runelog.Logger.Errorf("failed to get executablePath. because %s\n", err.Error())
		return err
	}

	tmpBin := d.binPath + ".tmp"
	f, err := os.Create(tmpBin)
	if err != nil {
		d.runelog.Logger.Errorf("failed to create %s. because %s\n", tmpBin, err.Error())
		return err
	}

	exeFile, err := os.Open(exePath)
	if err != nil {
		f.Close()
		d.runelog.Logger.Errorf("failed to open %s. because %s\n", exePath, err.Error())
		return err
	}

	_, err = io.Copy(f, exeFile)
	exeFile.Close()
	if err != nil {
		f.Close()
		d.runelog.Logger.Errorf("failed to copy %s to %s. because %s\n", f, exePath, err.Error())
		return err
	}

	if err := f.Close(); err != nil {
		d.runelog.Logger.Errorf("failed to close the %s. because %s\n", f.Name(), err.Error())
		return err
	}

	if err := os.Chmod(tmpBin, 0755); err != nil {
		d.runelog.Logger.Errorf("failed to grant permission for %s. because %s\n", tmpBin, err.Error())
		return err
	}

	if err := os.Rename(tmpBin, d.binPath); err != nil {
		d.runelog.Logger.Errorf("failed to rename %s to %s. because %s\n", tmpBin, d.binPath, err.Error())
		return err
	}

	if err := os.WriteFile(d.daemonFilePath, []byte(d.systemConfig), 0755); err != nil {
		d.runelog.Logger.Errorf("failed to write %s to %s. because %s\n", d.daemonFilePath, d.systemConfig, err.Error())
		return err
	}

	err = d.load()
	if err != nil {
		d.runelog.Logger.Errorf("failed to load %s. plist path is here %s. because %s\n", d.serviceName, d.daemonFilePath, err.Error())
		return err
	}

	return nil
}

func (d *daemon) Uninstall() (err error) {
	err = d.checkPrivileges()
	if err != nil {
		return err
	}

	_, isRunnning := d.isRunnning()
	if isRunnning {
		err := d.Stop()
		if err != nil {
			d.runelog.Logger.Errorf("failed to stop %s. plist path is here %s. because %s\n", d.serviceName, d.daemonFilePath, err.Error())
			return err
		}
		err = d.unload()
		if err != nil {
			d.runelog.Logger.Errorf("failed to unload %s. plist path is here %s. because %s\n", d.serviceName, d.daemonFilePath, err.Error())
			return err
		}
	}

	err = os.Remove(d.daemonFilePath)
	if os.IsNotExist(err) {
		return nil
	}

	err = os.Remove(d.binPath)
	if os.IsNotExist(err) {
		return nil
	}

	return err
}

func (d *daemon) load() error {
	err := d.checkPrivileges()
	if err != nil {
		return err
	}

	if out, err := exec.Command("launchctl", "load", d.daemonFilePath).CombinedOutput(); err != nil {
		fmt.Printf("failed to running launchctl load %s, because %s\n %s\n", d.daemonFilePath, err.Error(), out)
		return err
	}

	return nil
}

func (d *daemon) unload() error {
	err := d.checkPrivileges()
	if err != nil {
		return err
	}

	if !d.isInstalled() {
		return errors.New("not installed")
	}

	if mes, isRunning := d.isRunnning(); !isRunning {
		return errors.New(mes)
	}

	out, err := exec.Command("launchctl", "unload", d.daemonFilePath).CombinedOutput()
	if err != nil {
		fmt.Printf("failed to launchctl unload %s, because %v.\n %s\n", d.daemonFilePath, err.Error(), out)
		return err
	}

	return nil
}

func (d *daemon) start() error {
	err := d.checkPrivileges()
	if err != nil {
		return err
	}

	if status, isRunning := d.isRunnning(); isRunning {
		return errors.New(status)
	}

	if out, err := exec.Command("launchctl", "start", d.serviceName).CombinedOutput(); err != nil {
		fmt.Printf("failed to running launchctl start %s, because %s.\n %s\n", d.serviceName, err.Error(), out)
		return err
	}

	return nil
}

func (d *daemon) Stop() error {
	err := d.checkPrivileges()
	if err != nil {
		return err
	}

	if !d.isInstalled() {
		return errors.New("not installed")
	}

	if mes, isRunning := d.isRunnning(); !isRunning {
		return errors.New(mes)
	}

	out, err := exec.Command("launchctl", "stop", d.serviceName).CombinedOutput()
	if err != nil {
		fmt.Printf("failed to launchctl stop %s, because %v.\n %s\n", d.serviceName, err.Error(), out)
		return err
	}
	return nil
}

func (d *daemon) Status() (string, bool) {
	err := d.checkPrivileges()
	if err != nil {
		return nonroot, false
	}

	if !d.isInstalled() {
		return notinstalled, false
	}

	_, isRunnning := d.isRunnning()
	if !isRunnning {
		return notrunning, false
	}

	return running, true
}

func (d *daemon) isInstalled() bool {
	if _, err := os.Stat(d.daemonFilePath); err == nil {
		return true
	}
	return false
}

func (d *daemon) isRunnning() (string, bool) {
	out, err := exec.Command("launchctl", "list", d.serviceName).CombinedOutput()
	if err == nil {
		if matched, err := regexp.MatchString(d.serviceName, string(out)); err == nil && matched {
			reg := regexp.MustCompile("PID\" = ([0-9]+);")
			data := reg.FindStringSubmatch(string(out))
			if len(data) > 1 {
				return fmt.Sprintf("%s is running on pid: %s", d.serviceName, data[1]), true
			}
			return fmt.Sprintf("%s is running. but cannot get pid. please report it", d.serviceName), true
		}
	}

	return fmt.Sprintf("%s is stopped", d.serviceName), false
}

func (d *daemon) checkPrivileges() error {
	if out, err := exec.Command("id", "-g").CombinedOutput(); err == nil {
		if gid, parseErr := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 32); parseErr == nil {
			if gid == 0 {
				return nil
			}
			return errors.New("run with root privileges")
		}
	}
	return errors.New("unsupport system")
}
