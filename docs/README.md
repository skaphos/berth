# Berth Documentation

This directory is the portable Markdown source for Berth documentation.
It is published with MkDocs Material and `mike` at
`https://skaphos.github.io/berth/`.

## User and Operator Docs

| Document | Purpose |
| --- | --- |
| [Architecture](architecture.md) | System model, runtime shape, storage backends, and failure behavior. |
| [Configuration reference](reference/configuration.md) | API server, operator, broker, Helm values, and environment inputs. |
| [API reference](reference/api.md) | Generated CRD reference for `berth.skaphos.io/v1alpha1`. |
| [Code map](code-map.md) | Package and entrypoint tour for contributors. |
| `test/e2e/fixtures/README.md` | Local three-cluster test topology. |

## Process Docs

| Document | Purpose |
| --- | --- |
| `CONTRIBUTING.md` | Branch, commit, PR, and local validation expectations. |
| `RELEASE.md` | Release Please, tag publishing, artifacts, and required credentials. |
| `CHANGELOG.md` | Release Please managed release notes. |
| `AGENTS.md` | Repository-specific instructions for AI coding agents. |

## References

The CRD reference is generated:

```bash
go -C tools tool task docs:api-ref
```

Do not hand-edit `docs/reference/api.md`; update `api/v1alpha1` comments and
regenerate it. The CLI reference target is still future work because the
current Cobra lease commands are scaffolds.

## Publishing

`mkdocs.yml` defines the site navigation. `.github/workflows/docs.yml`
publishes:

- `main` pushes as the `main` docs version.
- `v*` tags as versioned docs, updating the `latest` alias.
