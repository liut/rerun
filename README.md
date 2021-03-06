Use like ```rerun github.com/skelterjohn/go.uik/uiktest```

Usage: ```rerun [--test] [--build] [--ignore 'tmp*'] <import path> [arg]*```

For any go executable in a normal GOPATH workspace, rerun will watch its source,
rebuild, retest, and rerun. As long as ```go install <import path>``` works,
rerun will be able to find it.

Along with the target's source, rerun also watches the source of all
the target's non-GOROOT dependencies.

When using flag `--test`, rerun executes `go test`. If tests fail, rerun will not continue to build and/or run the program.

Flag `--build` makes rerun execute `go build` in the local folder, creating a executable.
