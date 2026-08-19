package access

import "os"

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
