# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.0] - 2026-07-12

### Added
- `WithForceQuit(fn)` overrides what happens on a second shutdown signal (the
  default still calls `os.Exit(130)`), so an app can run its own last-resort
  cleanup and the behavior is testable.

### Fixed
- `Use`, `OnStart` and `OnStop` now ignore a call made after `Run` has started
  (with a logged warning) instead of racing the slices `Run` reads.
- `stopBounded` no longer spawns a goroutine when the shutdown deadline is
  already spent, so a stop hook or component that ignores its context cannot
  leak a goroutine that outlives `Run`; stop hooks are skipped loudly once the
  deadline is exceeded.
- The `Worker` and `HTTPServer` components reject a second `Start` instead of
  overwriting their internal state, so direct (non-App) use cannot orphan the
  first goroutine or listener.
- The `HTTPServer` component's lifecycle flags are guarded by a mutex, so direct
  concurrent `Start`/`Stop` is no longer a data race.

## [0.1.0]

First release: the lifecycle core (ordered start, signal-aware wait, graceful
reverse shutdown) with HTTPServer, Worker and Closer components, on the standard
library.
