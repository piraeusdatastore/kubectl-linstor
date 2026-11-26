# Contributing to kubectl-linstor

The [Piraeus contributing guide](https://github.com/piraeusdatastore/piraeus/blob/master/CONTRIBUTING.md) describes how to report issues, write commits and changelog entries, and open pull requests.
The sections that follow describe what is specific to this repository.

## Target branch

Open pull requests against the `master` branch.

## Build and test

Install [Go](https://go.dev/) in the version declared in `go.mod`.
Build the plugin, vet the code, and run the tests:

```
$ go build -o kubectl-linstor .
$ go vet ./...
$ go test ./...
```

On every pull request, CI runs `go vet ./...` and `go test ./...`.

## Testing on a cluster

`kubectl` runs any executable named `kubectl-linstor` in your `PATH` as the `kubectl linstor` command.
To try a change, put the binary you built first in your `PATH`, and run it against a cluster that Piraeus Operator manages:

```
$ PATH="$PWD:$PATH" kubectl linstor node list
```
