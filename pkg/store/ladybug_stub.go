//go:build !ladybug

package store

import "errors"

const ladybugAvailable = false

func openLadybug(_ string) (Indexer, error) {
	return nil, errors.New("ladybug backend not available: build with -tags ladybug")
}
