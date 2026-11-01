//go:build !windows

package main

import "errors"

const ServiceName = "VideoEncoder"
const serviceSupported = false

var isService bool

var errNoSvc = errors.New("serviciul Windows este disponibil doar pe Windows (pe Linux folosește systemd)")

func runService(onStop func()) error         { return errNoSvc }
func stopSelfService()                       {}
func serviceInstall(user, pass string) error { return errNoSvc }
func serviceUninstall() error                { return errNoSvc }
func serviceDeleteOnly() error               { return errNoSvc }
func serviceStart() error                    { return errNoSvc }
func serviceStop() error                     { return errNoSvc }
func serviceState() string                   { return "absent" }
func runElevated(args string) error          { return errNoSvc }
