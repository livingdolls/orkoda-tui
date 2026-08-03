//go:build !windows

package checks

import (
	"os"
	"strconv"
)

func currentDockerIdentity() string {
	return strconv.Itoa(os.Getuid()) + ":" + strconv.Itoa(os.Getgid())
}
