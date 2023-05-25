# Changelog
All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic
Versioning](https://semver.org/spec/v2.0.0.html).

## [1.12.2]
### Fixed
- Payment after probing used the global rebalancing context instead of attempt
  context which can result in waiting for the payment to fail for too long (6
  hours by default)
## [1.12.1]
### Changed
- Rapid rebalance now goes all the way down to min amount to squeeze all
  available liquidity on the route
### Fixed
- Silent crash with zero exit code on FEE_INSUFFICIENT in some cases
## [1.12.0]
### Added
- Check route for max htlc during rapid rebalance and limit the max rebalancing
  amount to not hit that cap
- 2% safety margin for channel reserve and commitment fee to prevent failures due to hitting that limit
## [1.11.0]
### Added
- Exclude too young channels by their age in blocks: give liquidity a chance to
  move to the other side by itself. You can now set the minimum channel age to
  be considered for rebalance.
### Changed
- Rapid rebalance now skips some steps if the channel is already constrained by
  liquidity
### Fixed
- Sudden fee changes are now properly handled: we retry rebalance and also see
  if the new fee is still within the limit
## [1.10.2]
### Fixed
- Rapid rebalance summary now shows the correct total fee
## Changed
- Functions doing high-level rebalancing logic are refactored to a separate file
## [1.10.1]
### Fixed
- Rapid rebalance summary now shows the correct total amount
## [1.10.0]
### Added
- Rapid rebalance is now accelerated, it tries to double the amount until it
  fails, then the amount is halved until it becomes lower than the initial one
- Goreleaser and Docker configs
- If any rapid rebalances succeeded, the total amount and fees are displayed at
  the end
- Stat file (CSV) is now also flock'ed to prevent accidental corruption if
  multiple instances try to update it
### Fixed
- Fees in route print and success message are now calculated from the target
  balance (without fees) so for example 50 sat fee and 1 000 000 sat amount
  would be shown as 50ppm, before it was 49ppm.
## [1.9.2]
### Added
- Exit codes for failed rebalances (2 for global timeout and 1 for all
  other reasons)
### Fixed
- Incorrect lost profit description
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