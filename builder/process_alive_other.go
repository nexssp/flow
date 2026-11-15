//go:build !unix && !windows

package builder

func processAlive(int) (bool, error) {
	return false, errProcessLivenessUnavailable
}
