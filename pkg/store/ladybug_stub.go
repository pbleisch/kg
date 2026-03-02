//go:build !ladybug

package store

import "errors"

const ladybugAvailable = false

// LadybugAvailable reports whether this binary was built with LadybugDB support.
func LadybugAvailable() bool { return false }

func openLadybug(_ string) (Indexer, error) {
	return nil, errors.New("ladybug backend not available: build with -tags ladybug")
}
