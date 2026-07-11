package app

import "errors"

// ErrAlreadyRunning is returned by Run when the App is already running.
var ErrAlreadyRunning = errors.New("app: already running")
