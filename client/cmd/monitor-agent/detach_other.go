//go:build !windows

package main

import "log"

func detachIfRequested(enabled bool) error {
	if enabled {
		log.Print("-u is ignored on this OS; agent stays in the foreground")
	}
	return nil
}
