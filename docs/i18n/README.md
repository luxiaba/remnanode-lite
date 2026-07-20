# Documentation Localization

[Back to the documentation index](../README.md)

English is the only authoritative language for Remnanode Lite source code,
automation, runtime output, and documentation. Translations are maintained as a
convenience for operators and may lag behind their English sources.

## Supported locales

| Locale | Root entry point | Documentation directory | Maintained scope |
| --- | --- | --- | --- |
| Simplified Chinese (`zh-CN`) | [`README.zh-CN.md`](../../README.zh-CN.md) | [`zh-CN/`](zh-CN/README.md) | Operator documentation plus the existing maintainer snapshot |
| Russian (`ru`) | [`README.ru.md`](../../README.ru.md) | [`ru/`](ru/README.md) | Stable operator documentation |

The stable operator set consists of the project overview, configuration
reference, Docker and native deployment guides, operations guide, versioning
policy, and security policy. Maintainer, contract, release, and acceptance
documents may be summarized by a localized index while linking to the English
source.

The following files remain English-only truth sources and must not be copied
into locale trees:

- `LICENSE`, `CHANGELOG.md`, and GitHub Release notes;
- release acceptance JSON, checksums, attestations, and other machine evidence;
- executable Compose files, service definitions, scripts, and environment
  templates;
- generated source and protocol descriptors.

## Translation contract

Every translated Markdown file starts with metadata on its first non-empty
line:

```markdown
<!-- translation: locale=zh-CN; source=docs/operations.md; source-sha256=<sha256> -->
```

Within the first 20 lines, the page must also visibly state that English is
authoritative and link to the canonical source. The recorded SHA-256 is the
hash of the complete English source file, not a Git commit or a rendered page.

`go run ./cmd/docs-check` validates locale names, source paths, hashes, source
links, duplicate mappings, local links, heading anchors, and reachability from
the root README. An invalid translation contract fails the check. A source hash
mismatch emits a stale-translation warning but does not block ordinary code
changes.

## Updating a translation

1. Update the English canonical document first.
2. Review the complete English diff and update the translation without changing
   commands, paths, environment names, numeric limits, or support claims.
3. Recompute the source hash with `shasum -a 256 <source>` and update the
   metadata.
4. Keep links inside the same locale when that translation exists. Clearly link
   to English when no localized page exists.
5. Run `go run ./cmd/docs-check` and review every stale warning before opening a
   pull request.

Translation-only changes must not alter runtime code, release evidence, or the
meaning of the English source. When a translation and its source disagree,
follow the English source and report the drift in the same change that fixes it.
