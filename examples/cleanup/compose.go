//go:build pfw_inject

package main

import pfw "github.com/palma99/palma-framework"

func Initialize() (*Service, func() error, error) {
	return pfw.BuildWithCleanup[*Service](
		pfw.Constructors(OpenDatabase, NewService),
	)
}
