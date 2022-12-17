# How to Create Releases with goreleaser

Before you can use goreleaser you first need to install it: https://goreleaser.com/install/

In case you want to create a test release just create a snapshot build with:

`goreleaser release --snapshot --rm-dist`

This will only create a release version locally.

One can only build all the target or only one specific target:

`GOOS=linux GOARCH=amd64 goreleaser build --rm-dist`

Currently the CHANGE.LOG of the goreleaser is enabled to remove it go to the `.goreleaser.yaml` and change the setting.

If you verified that the snapshot version is good to go than you can create a final release 

First you need to get a github token with at least the privilige of write:packages

`export GITHUB_TOKEN="YOUR_GH_TOKEN"`

detailed information how to create a release with a speicific tag can be found here: https://goreleaser.com/quick-start/

```
git tag -a v0.1.0 -m "Release Comment"

goreleaser release

```

Currently Docker Releases are turned off, but can be decided otherwise

