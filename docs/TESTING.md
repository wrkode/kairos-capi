# Testing

## Unit tests (default)
Run all unit tests:
```
go test ./...
```

The envtest integration tests are excluded by default and only run with the `envtest` build tag.

## Envtest integration tests (opt-in)
Run integration tests with envtest assets:
```
make test-envtest
```

This target:
- Installs `setup-envtest` if needed (see Makefile version pin)
- Downloads envtest assets for Kubernetes `1.30.3`
- Sets `KUBEBUILDER_ASSETS` and runs `go test ./... -tags=envtest`

If you only want the asset path:
```
make envtest-assets
```

## Notes
- Envtest assets are cached by `setup-envtest` under your Go tools cache.
- If you change Kubernetes version, update `ENVTEST_K8S_VERSION` in the Makefile.
- If you need a different setup-envtest version, update `SETUP_ENVTEST_VERSION` in the Makefile.
- Note: Using `latest` may trigger a newer Go toolchain download depending on upstream module requirements.