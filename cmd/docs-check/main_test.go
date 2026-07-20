package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestGitHubSlugBase(t *testing.T) {
	tests := map[string]string{
		"Runtime architecture":        "runtime-architecture",
		"Docker Compose 部署":           "docker-compose-部署",
		"`Version` 与 ContractVersion": "version-与-contractversion",
		"[版本模型](versioning.md)":       "版本模型",
		"12.3 Secret、文件与 rw-core":     "123-secret文件与-rw-core",
		"Жизненный цикл Xray":         "жизненный-цикл-xray",
		"  repeated   whitespace  ":   "repeated-whitespace",
	}
	for input, expected := range tests {
		if actual := githubSlugBase(input); actual != expected {
			t.Errorf("githubSlugBase(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestUniqueAnchorAvoidsGeneratedAndLiteralCollisions(t *testing.T) {
	existing := map[string]struct{}{
		"foo":   {},
		"foo-1": {},
	}
	if got := uniqueAnchor("foo", existing); got != "foo-2" {
		t.Fatalf("uniqueAnchor() = %q, want foo-2", got)
	}
}

func TestCodeFenceParsing(t *testing.T) {
	fence, ok := opensCodeFence("  ````go")
	if !ok || fence.marker != '`' || fence.length != 4 {
		t.Fatalf("opensCodeFence() = %#v, %v", fence, ok)
	}
	for _, line := range []string{"````", "  `````  "} {
		if !closesCodeFence(line, fence) {
			t.Errorf("closesCodeFence(%q) = false", line)
		}
	}
	for _, line := range []string{"   ```", "    ````", "````go"} {
		if closesCodeFence(line, fence) {
			t.Errorf("closesCodeFence(%q) = true", line)
		}
	}
	if _, ok := opensCodeFence("    ```go"); ok {
		t.Fatal("four-space indented code was parsed as a fence")
	}
}

func TestExtractLinkTargets(t *testing.T) {
	line := `[one](docs/a.md#part) [two](<docs/a file.md>) [three](docs/a(1).md "title")`
	want := []string{"docs/a.md#part", "docs/a file.md", "docs/a(1).md"}
	if got := extractLinkTargets(line); !slices.Equal(got, want) {
		t.Fatalf("extractLinkTargets() = %q, want %q", got, want)
	}
}

func TestParseDocumentIgnoresFencedCommentedAndInlineCode(t *testing.T) {
	root := t.TempDir()
	content := strings.Join([]string{
		"# Visible heading",
		"",
		"`[inline](missing.md)`",
		"<!-- [comment](missing.md)",
		"# hidden heading -->",
		"````markdown",
		"# fenced heading",
		"[fenced](missing.md)",
		"```",
		"````",
		"[real](target.md#target-heading)",
		"[reference]: <reference.md#reference-heading>",
		`<a href="html.md#html-heading">HTML</a>`,
	}, "\n")
	writeTestFile(t, root, "README.md", content)

	doc, problems, err := parseDocument(root, "README.md")
	if err != nil || len(problems) != 0 {
		t.Fatalf("parseDocument() problems = %v, error = %v", problems, err)
	}
	wantLinks := []string{"target.md#target-heading", "reference.md#reference-heading", "html.md#html-heading"}
	gotLinks := make([]string, 0, len(doc.links))
	for _, link := range doc.links {
		gotLinks = append(gotLinks, link.target)
	}
	if !slices.Equal(gotLinks, wantLinks) {
		t.Fatalf("links = %#v", doc.links)
	}
	if _, ok := doc.anchors["visible-heading"]; !ok || len(doc.anchors) != 1 {
		t.Fatalf("anchors = %#v", doc.anchors)
	}
}

func TestValidateLinkAndOrphanDetection(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "README.md", "# Root\n\n[target](docs/target.md#section)\n")
	writeTestFile(t, root, "docs/target.md", "# Target\n\n## Section\n")
	writeTestFile(t, root, "docs/orphan.md", "# Orphan\n")

	documents := make(map[string]document)
	for _, path := range []string{"README.md", "docs/target.md", "docs/orphan.md"} {
		doc, problems, err := parseDocument(root, path)
		if err != nil || len(problems) != 0 {
			t.Fatalf("parseDocument(%s) problems = %v, error = %v", path, problems, err)
		}
		documents[path] = doc
	}
	if problem := validateLink(root, documents["README.md"], documents["README.md"].links[0], documents); problem != "" {
		t.Fatal(problem)
	}
	problems := orphanedDocuments(documents)
	if len(problems) != 1 || !strings.Contains(problems[0], "docs/orphan.md") {
		t.Fatalf("orphanedDocuments() = %v", problems)
	}
}

func TestValidateTranslationsAcceptsSupportedLocalesAndRootReadmes(t *testing.T) {
	root := t.TempDir()
	rootSource := "# Remnanode Lite\n\nEnglish source.\n"
	guideSource := "# Operations\n\nEnglish source.\n"
	files := map[string]string{
		"README.md":                     rootSource,
		"README.zh-CN.md":               translationTestDocument("zh-CN", "README.md", testSHA256(rootSource), "README.md", "中文"),
		"README.ru.md":                  translationTestDocument("ru", "README.md", testSHA256(rootSource), "README.md", "Русский"),
		"docs/operations.md":            guideSource,
		"docs/i18n/zh-CN/operations.md": translationTestDocument("zh-CN", "docs/operations.md", testSHA256(guideSource), "../../operations.md", "运维"),
		"docs/i18n/ru/operations.md":    translationTestDocument("ru", "docs/operations.md", testSHA256(guideSource), "../../operations.md", "Эксплуатация"),
	}
	documents, parseProblems := parseTestDocuments(t, root, files)
	if len(parseProblems) != 0 {
		t.Fatalf("parse problems = %v", parseProblems)
	}

	problems, warnings := validateTranslations(root, documents)
	if len(problems) != 0 || len(warnings) != 0 {
		t.Fatalf("validateTranslations() problems = %v, warnings = %v", problems, warnings)
	}
}

func TestValidateTranslationsTreatsSourceHashMismatchAsWarning(t *testing.T) {
	root := t.TempDir()
	source := "# Canonical\n"
	files := map[string]string{
		"docs/guide.md":         source,
		"docs/i18n/ru/guide.md": translationTestDocument("ru", "docs/guide.md", strings.Repeat("0", 64), "../../guide.md", "Перевод"),
	}
	documents, parseProblems := parseTestDocuments(t, root, files)
	if len(parseProblems) != 0 {
		t.Fatalf("parse problems = %v", parseProblems)
	}

	problems, warnings := validateTranslations(root, documents)
	if len(problems) != 0 {
		t.Fatalf("validateTranslations() problems = %v", problems)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "translation may be stale") {
		t.Fatalf("validateTranslations() warnings = %v", warnings)
	}
}

func TestValidateTranslationsRejectsInvalidContracts(t *testing.T) {
	canonical := "# Canonical\n"
	hash := testSHA256(canonical)
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			name: "unsupported locale",
			files: map[string]string{
				"docs/guide.md":         canonical,
				"docs/i18n/fr/guide.md": translationTestDocument("fr", "docs/guide.md", hash, "../../guide.md", "Traduction"),
			},
			want: `unsupported translation locale "fr"`,
		},
		{
			name: "locale does not match path",
			files: map[string]string{
				"README.md":    canonical,
				"README.ru.md": translationTestDocument("zh-CN", "README.md", hash, "README.md", "Русский"),
			},
			want: `translation locale "zh-CN" does not match path locale "ru"`,
		},
		{
			name: "missing metadata",
			files: map[string]string{
				"docs/guide.md":            canonical,
				"docs/i18n/zh-CN/guide.md": "# 中文\n",
			},
			want: "translation metadata is missing",
		},
		{
			name: "invalid hash",
			files: map[string]string{
				"docs/guide.md":         canonical,
				"docs/i18n/ru/guide.md": translationTestDocument("ru", "docs/guide.md", strings.Repeat("A", 64), "../../guide.md", "Русский"),
			},
			want: "source-sha256 must be 64 lowercase hexadecimal characters",
		},
		{
			name: "missing source",
			files: map[string]string{
				"docs/i18n/ru/guide.md": translationTestDocument("ru", "docs/missing.md", hash, "../../missing.md", "Русский"),
			},
			want: "translation source does not exist: docs/missing.md",
		},
		{
			name: "source under i18n",
			files: map[string]string{
				"docs/guide.md":            canonical,
				"docs/i18n/zh-CN/guide.md": translationTestDocument("zh-CN", "docs/guide.md", hash, "../../guide.md", "中文"),
				"docs/i18n/ru/derived.md":  translationTestDocument("ru", "docs/i18n/zh-CN/guide.md", hash, "../zh-CN/guide.md", "Русский"),
			},
			want: "translation source must be canonical Markdown outside docs/i18n",
		},
		{
			name: "non-normalized source",
			files: map[string]string{
				"README.md":    canonical,
				"README.ru.md": translationTestDocument("ru", "docs/../README.md", hash, "README.md", "Русский"),
			},
			want: "translation source must be a normalized repository path",
		},
		{
			name: "missing early source link",
			files: map[string]string{
				"docs/guide.md":         canonical,
				"docs/i18n/ru/guide.md": translationTestDocument("ru", "docs/guide.md", hash, "../../other.md", "Русский"),
			},
			want: "translated document must visibly link to docs/guide.md within its first 20 lines",
		},
		{
			name: "duplicate locale and source",
			files: map[string]string{
				"docs/guide.md":          canonical,
				"docs/i18n/zh-CN/one.md": translationTestDocument("zh-CN", "docs/guide.md", hash, "../../guide.md", "一"),
				"docs/i18n/zh-CN/two.md": translationTestDocument("zh-CN", "docs/guide.md", hash, "../../guide.md", "二"),
			},
			want: "duplicate zh-CN translation for docs/guide.md",
		},
		{
			name: "metadata on canonical document",
			files: map[string]string{
				"README.md":     canonical,
				"docs/guide.md": translationTestDocument("ru", "README.md", hash, "../README.md", "Guide"),
			},
			want: "translation metadata is only allowed on localized documents",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			documents, parseProblems := parseTestDocuments(t, root, test.files)
			if len(parseProblems) != 0 {
				t.Fatalf("parse problems = %v", parseProblems)
			}
			problems, _ := validateTranslations(root, documents)
			if !containsProblem(problems, test.want) {
				t.Fatalf("validateTranslations() problems = %v, want substring %q", problems, test.want)
			}
		})
	}
}

func TestParseDocumentValidatesTranslationMetadataPlacementAndSyntax(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "metadata is not first non-blank line",
			content: "# Translation\n\n<!-- translation: locale=ru; source=README.md; source-sha256=" + strings.Repeat("0", 64) + " -->\n",
			want:    "translation metadata must be the first non-blank line",
		},
		{
			name:    "metadata is malformed",
			content: "<!-- translation: locale=ru; source=README.md -->\n# Translation\n",
			want:    "malformed translation metadata",
		},
		{
			name: "metadata is duplicated",
			content: "<!-- translation: locale=ru; source=README.md; source-sha256=" + strings.Repeat("0", 64) + " -->\n" +
				"<!-- translation: locale=ru; source=README.md; source-sha256=" + strings.Repeat("0", 64) + " -->\n# Translation\n",
			want: "duplicate translation metadata",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeTestFile(t, root, "README.ru.md", test.content)
			_, problems, err := parseDocument(root, "README.ru.md")
			if err != nil {
				t.Fatal(err)
			}
			if !containsProblem(problems, test.want) {
				t.Fatalf("parseDocument() problems = %v, want substring %q", problems, test.want)
			}
		})
	}
}

func TestParseDocumentIgnoresTranslationMetadataExamplesInCodeFences(t *testing.T) {
	root := t.TempDir()
	content := "# Translation policy\n\n```markdown\n<!-- translation: locale=ru; source=README.md; source-sha256=" + strings.Repeat("0", 64) + " -->\n```\n"
	writeTestFile(t, root, "docs/i18n/README.md", content)
	doc, problems, err := parseDocument(root, "docs/i18n/README.md")
	if err != nil || len(problems) != 0 {
		t.Fatalf("parseDocument() problems = %v, error = %v", problems, err)
	}
	if doc.translation != nil {
		t.Fatalf("fenced metadata example was parsed: %#v", doc.translation)
	}
}

func parseTestDocuments(t *testing.T, root string, files map[string]string) (map[string]document, []string) {
	t.Helper()
	paths := make([]string, 0, len(files))
	for path, content := range files {
		writeTestFile(t, root, path, content)
		paths = append(paths, path)
	}
	slices.Sort(paths)

	documents := make(map[string]document, len(files))
	var problems []string
	for _, path := range paths {
		doc, docProblems, err := parseDocument(root, path)
		if err != nil {
			t.Fatalf("parseDocument(%s): %v", path, err)
		}
		documents[path] = doc
		problems = append(problems, docProblems...)
	}
	return documents, problems
}

func translationTestDocument(locale, source, sourceHash, link, heading string) string {
	return fmt.Sprintf("<!-- translation: locale=%s; source=%s; source-sha256=%s -->\n# %s\n\n[English source](%s)\n", locale, source, sourceHash, heading, link)
}

func testSHA256(content string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
}

func containsProblem(problems []string, want string) bool {
	for _, problem := range problems {
		if strings.Contains(problem, want) {
			return true
		}
	}
	return false
}

func writeTestFile(t *testing.T, root, path, content string) {
	t.Helper()
	fullPath := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
