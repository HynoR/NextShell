# NextShell scoped mygo patch

Base: github.com/egoist/mygo **v0.8.3**, sum
`h1:aTLEhS221YhsX+gKGNLQ5xEKMLCcYYr3pH9AkTwIRs4=`.
The require remains exactly v0.8.3; Go replace selects this checked-in copy.
The MIT LICENSE and retained source/assets are unchanged except as listed below.
Unused website/docs/examples/commands/scripts/benchmarks/package tooling and upstream
`*_test.go` files were omitted; no competing SSH-client source is included.

Production patch: `ui/inspector.go` only. Native inspector keys are restricted to
**Ctrl+Alt+F12**. Ctrl+Shift+C/V, plain F12 and Ctrl+Shift+I are not intercepted.
NextShell release binaries disable DevTools unconditionally. Development requires
both `-tags nextshell_devtools` and `NEXTSHELL_DEVTOOLS=1`.

Test helpers only: `ui/nextshell_testapi.go` and `ui/headless.go` extend the in-memory
Tester with native text-client commits, grouped events before rendering, and an
inspector enable switch. No native OS input injection is implemented.
Regression tests live in NextShell's appui/cmd packages so default `go test ./...`
runs them.

No native input ordering/rendering production change is claimed. A proposed native
commit invalidation patch was investigated and rejected: the existing text-input
adapter already invalidates and the baseline reproducer passed. Do not mistake
these test helpers for a fix of the reported native Linux lag/drop.
