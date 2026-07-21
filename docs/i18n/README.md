# Documentation Localization

[Back to the documentation index](../README.md)

English is the source language for Remnanode Lite code, automation, runtime
messages, and documentation. Chinese and Russian translations are maintained
for readers, but they may occasionally trail the English source.

## Supported locales

| Locale | Root entry point | Documentation directory | Maintained scope |
| --- | --- | --- | --- |
| Simplified Chinese (`zh-CN`) | [`README.zh-CN.md`](../../README.zh-CN.md) | [`zh-CN/`](zh-CN/README.md) | Operator documentation plus the existing maintainer snapshot |
| Russian (`ru`) | [`README.ru.md`](../../README.ru.md) | [`ru/`](ru/README.md) | Stable operator documentation |

The translated operator set covers the project overview, configuration,
Docker and native deployment, operations, versioning, and security. Localized
indexes may summarize maintainer, contract, and release material while linking
to the English document for the full details.

Keep these files in English only instead of copying them into locale trees:

- `LICENSE`, `CHANGELOG.md`, and generated GitHub Release notes;
- checksums, attestations, and other generated release metadata;
- executable Compose files, service definitions, scripts, and environment
  templates;
- generated source and protocol descriptors.

## Translation metadata

Every translated Markdown file starts with metadata on its first non-empty
line:

```markdown
<!-- translation: locale=zh-CN; source=docs/operations.md; source-sha256=<sha256> -->
```

Within the first 20 lines, the page must also link visibly to its English
source and say that the English version takes precedence. The recorded SHA-256
is the hash of the complete source file, not a Git commit or rendered page.

`go run ./cmd/docs-check` validates locale names, source paths and hashes,
source links, duplicate mappings, local links, heading anchors, and
reachability from the root README. Invalid metadata fails the check. A stale
source hash produces a warning during ordinary development and becomes an
error in the Release gate.

## Updating a translation

1. Update the English canonical document first.
2. Read the complete English diff, then update the translation without changing
   commands, paths, environment names, numeric limits, or support claims.
3. Recompute the source hash with `shasum -a 256 <source>` and update the
   metadata.
4. Keep links inside the same locale when that translation exists. Clearly link
   to English when no localized page exists.
5. Run `go run ./cmd/docs-check` and review every stale warning before opening a
   pull request.

A translation-only change must not alter runtime code, release metadata, or the
meaning of the English source. If a translation disagrees with its source, use
the English document as the reference and fix the drift in the same change.
