//
// Copyright 2020 IBM Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package util

import (
	"bytes"
	"os/exec"
	"strings"
	"syscall"

	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("ip_cmd")
var iproute_bin = "/usr/sbin/ip"

func ExecIpCmd(cmdStr string) (string, int, error) {
	// tokenize cmdStr
	cmdStrArr := strings.Split(cmdStr, " ")

	log.Info("Executing ip command", "command", strings.Join(cmdStrArr, " "))
	cmd := exec.Command(iproute_bin, cmdStrArr...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Start()
	if err != nil {
		return "", 1, err
	}

	if err := cmd.Wait(); err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			// The program has exited with an exit code != 0

			// This works on both Unix and Windows. Although package
			// syscall is generally platform dependent, WaitStatus is
			// defined for both Unix and Windows and in both cases has
			// an ExitStatus() method with the same signature.
			if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
				return string(stderr.Bytes()), status.ExitStatus(), nil
			}
		} else {
			return string(stderr.Bytes()), 1, err
		}
	}

	log.Info("ip command executed", "command", strings.Join(cmdStrArr, " "), "output", string(stdout.Bytes()), "error", string(stderr.Bytes()))

	return string(stdout.Bytes()), 0, nil
}
