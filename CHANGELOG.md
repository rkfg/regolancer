# Changelog
All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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