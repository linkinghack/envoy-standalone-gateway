# Envoy Standalone Gateway protocol bundle

This directory is the self-contained distribution of the `esgw/v1alpha1` gateway configuration protocol. Consumers do not need to import the repository's internal Go packages.

Contents:

- [`SPEC.md`](SPEC.md): normative object, defaulting, validation, compatibility, and diagnostic rules;
- [`schema/v1alpha1.json`](schema/v1alpha1.json): generated JSON Schema 2020-12 bundle for all six document kinds;
- [`examples/valid/`](examples/valid/): directories that must pass `esgw conformance`;
- [`examples/invalid/`](examples/invalid/): negative cases with committed `expected.json` diagnostics.

Validate a configuration directory:

```sh
esgw conformance -f examples/valid/minimal-http
```

Export the exact schema embedded in the binary:

```sh
esgw schema -o schema/v1alpha1.json
```

The repository gate `make protocol-check` regenerates the schema, copies this bundle outside the repository, and compares every conformance report byte-for-byte.
