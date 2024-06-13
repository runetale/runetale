// Copyright (c) 2022 Runetale Inc & AUTHORS All rights reserved.
// Use of this source code is governed by a BSD 3-Clause License
// license that can be found in the LICENSE file.

package daemon

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/runetale/runetale/log"
)

type systemDRecord struct {
	// binary path
	binPath string
	// daemon name
	serviceName string
	// daemon file path
	daemonFilePath string
	// daemon system config
	systemConfig string

	log *log.Logger
}

// in effect, all it does is call load and start.
func (s *systemDRecord) Install() (err error) {
	defer func() {
		if os.Getuid() != 0 && err != nil {
			s.log.Logger.Errorf("run it again with sudo privileges: %s", err.Error())
			err = fmt.Errorf("run it again with sudo privileges: %s", err.Error())
		}
	}()

	err = s.checkPrivileges()
	if err != nil {
		return err
	}

	if s.isInstalled() {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(s.binPath), 0755); err != nil {
		s.log.Logger.Errorf("failed to create %s. because %s\n", s.binPath, err.Error())
		return err
	}

	exePath, err := os.Executable()
	if err != nil {
		s.log.Logger.Errorf("failed to get executablePath. because %s\n", err.Error())
		return err
	}

	tmpBin := s.binPath + ".tmp"
	f, err := os.Create(tmpBin)
	if err != nil {
		s.log.Logger.Errorf("failed to create %s. because %s\n", tmpBin, err.Error())
		return err
	}

	exeFile, err := os.Open(exePath)
	if err != nil {
		f.Close()
		s.log.Logger.Errorf("failed to open %s. because %s\n", exePath, err.Error())
		return err
	}

	_, err = io.Copy(f, exeFile)
	exeFile.Close()
	if err != nil {
		f.Close()
		s.log.Logger.Errorf("failed to copy %s to %s. because %s\n", f, exePath, err.Error())
		return err
	}

	if err := f.Close(); err != nil {
		s.log.Logger.Errorf("failed to close the %s. because %s\n", f.Name(), err.Error())
		return err
	}

	if err := os.Chmod(tmpBin, 0755); err != nil {
		s.log.Logger.Errorf("failed to grant permission for %s. because %s\n", tmpBin, err.Error())
		return err
	}

	if err := os.Rename(tmpBin, s.binPath); err != nil {
		s.log.Logger.Errorf("failed to rename %s to %s. because %s\n", tmpBin, s.binPath, err.Error())
		return err
	}

	// todo: skip for nix
	err = s.Uninstall()
	if err != nil {
		return err
	}

	if err := ioutil.WriteFile(s.daemonFilePath, []byte(s.systemConfig), 0700); err != nil {
		s.log.Logger.Errorf("failed to write %s to %s. because %s\n", s.daemonFilePath, s.systemConfig, err.Error())
		return err
	}

	err = s.Load()
	if err != nil {
		return err
	}

	err = s.start()
	if err != nil {
		return err
	}

	return nil
}

// in effect, all it does is call unload and stop.
func (s *systemDRecord) Uninstall() error {
	err := s.checkPrivileges()
	if err != nil {
		return err
	}

	_, isRunnning := s.isRunnning()
	if isRunnning {
		err = s.Unload()
		if err != nil {
			s.log.Logger.Errorf("failed to disable %s. path is here %s. because %s\n", s.serviceName, s.daemonFilePath, err.Error())
			return err
		}
		err := s.Stop()
		if err != nil {
			s.log.Logger.Errorf("failed to stop %s. path is here %s. because %s\n", s.serviceName, s.daemonFilePath, err.Error())
			return err
		}
	}

	err = os.Remove(s.daemonFilePath)
	if os.IsNotExist(err) {
		return nil
	}

	err = os.Remove(s.binPath)
	if os.IsNotExist(err) {
		return nil
	}

	return err
}

func (s *systemDRecord) Load() error {
	err := s.checkPrivileges()
	if err != nil {
		return err
	}

	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		fmt.Printf("failed to running systemctl daemon-reload %s, because %s\n %s\n", s.daemonFilePath, err.Error(), out)
		return err
	}

	if out, err := exec.Command("systemctl", "enable", s.serviceName+".service").CombinedOutput(); err != nil {
		fmt.Printf("failed to running systemctl daemon-reload %s, because %s\n %s\n", s.daemonFilePath, err.Error(), out)
		return err
	}

	return nil
}

func (s *systemDRecord) Unload() error {
	err := s.checkPrivileges()
	if err != nil {
		return err
	}

	if !s.isInstalled() {
		return errors.New("not installed")
	}

	if _, isRunning := s.isRunnning(); !isRunning {
		return errors.New("not running")
	}

	if out, err := exec.Command("systemctl", "disable", s.serviceName+".service").CombinedOutput(); err != nil {
		fmt.Printf("failed to disable systemctl %s, because %s\n %s\n", s.daemonFilePath, err.Error(), out)
		return err
	}

	return nil
}

func (s *systemDRecord) start() error {
	err := s.checkPrivileges()
	if err != nil {
		return err
	}

	if _, isRunning := s.isRunnning(); isRunning {
		return errors.New("already running")
	}

	if out, err := exec.Command("systemctl", "start", s.serviceName+".service").CombinedOutput(); err != nil {
		fmt.Printf("failed to running systemctl daemon-reload %s, because %s\n %s\n", s.daemonFilePath, err.Error(), out)
		return err
	}

	return nil
}

func (s *systemDRecord) Stop() error {
	err := s.checkPrivileges()
	if err != nil {
		return err
	}

	if !s.isInstalled() {
		return errors.New("not installed")
	}

	if mes, isRunning := s.isRunnning(); !isRunning {
		return errors.New(mes)
	}

	if out, err := exec.Command("systemctl", "stop", s.serviceName+".service").CombinedOutput(); err != nil {
		fmt.Printf("failed to stop systemctl %s, because %s\n %s\n", s.daemonFilePath, err.Error(), out)
		return err
	}

	return nil
}

func (s *systemDRecord) Status() (string, bool) {
	err := s.checkPrivileges()
	if err != nil {
		return nonroot, false
	}

	if !s.isInstalled() {
		return notinstalled, false
	}

	_, isRunnning := s.isRunnning()
	if !isRunnning {
		return notrunning, false
	}

	return running, true
}

func (s *systemDRecord) isInstalled() bool {
	if _, err := os.Stat(s.daemonFilePath); err == nil {
		return true
	}

	return false
}

func (s *systemDRecord) isRunnning() (string, bool) {
	output, err := exec.Command("systemctl", "status", s.serviceName+".service").Output()
	if err == nil {
		if matched, err := regexp.MatchString("Active: active", string(output)); err == nil && matched {
			reg := regexp.MustCompile("Main PID: ([0-9]+)")
			data := reg.FindStringSubmatch(string(output))
			if len(data) > 1 {
				return fmt.Sprintf("%s is running on pid: %s", s.serviceName, data[1]), true
			}
			return fmt.Sprintf("%s is running. but cannot get pid. please report it", s.serviceName), true
		}
	}

	return fmt.Sprintf("%s is stopped", s.serviceName), false
}

func (s *systemDRecord) checkPrivileges() error {
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
