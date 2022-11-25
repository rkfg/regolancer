# Changelog
All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.9.2]
### Added
- Exit codes for failed rebalances (2 for global timeout and 1 for all
  other reasons)
## [1.9.1]
### Fixed
- Probing doesn't return actual learned value and as such payment never happens
## [1.9.0]
### Added
- `--exclude-from`/`--exclude-to` parameters that support both channel and node
  IDs
- `--info` parameter to see the channels selected and other effective settings
  that will be used during rebalance
- .gitignore to prevent commiting sensitive info accidentally

### Changed
- `--exclude-channel-in`/`--exclude-channel-out` are deprecated and should be
  replaced with `--exclude-from`/`--exclude-to` (or respective config
  parameters)
### Fixed
- Channel that receives Ctrl-C signal is now buffered to satisfy the
  signal.Notify requirement
## [1.8.2]
### Added
- Improved help message with grouped settings
## [1.8.1]
### Fixed
- offline nodes used in `--from`/`--to` were silently ignored and channels were
  chosen based on percentages
## [1.8.0]
### Added
- Timeouts can be customized
### Changed
- Config files updated and fixed to showcase the latest changes
- `--allow-unbalance-from` and `--allow-unbalance-to` are deprecated and enabled
  by default.
- Zero amount is no longer supported, specify it explicitly or use
  `--rel-amount-to`/`--rel-amount-from`
- `--fail-tolerance` now also affects probing the same way it affects failed
  channels: if the next step is too close to the failed or successful step it's
  not tried and probing ends with the best known outcome
- Use separate contexts for probing attempts and rapid rebalances
## [1.7.0]
### Added
- This changelog
- Mission control helper to quickly skip routes that are likely to fail
- Discussion room in Matrix in README
### Changed
- `--exclude-channel` and `--exclude-node` are deprecated in favor of `--exclude`
  which accepts both channel and node IDs
- `--from` and `--to` now accept node ids as well and will use all channels open
    to the specified nodes
### Fixed
- Channels excluded as targets are also excluded as sources in some situations