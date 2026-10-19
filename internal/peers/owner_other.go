//go:build !unix

package peers

import "os"

func ownedByUser(os.FileInfo) bool {
	return true
}
