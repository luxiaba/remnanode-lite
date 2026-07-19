package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestGitHubSlugBase(t *testing.T) {
	tests := map[string]string{
		"Docker Compose 部署":           "docker-compose-部署",
		"`Version` 与 ContractVersion": "version-与-contractversion",
		"[版本模型](versioning.md)":       "版本模型",
		"12.3 Secret、文件与 rw-core":     "123-secret文件与-rw-core",
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
