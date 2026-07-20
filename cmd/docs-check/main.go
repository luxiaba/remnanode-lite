package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var (
	headingPattern             = regexp.MustCompile(`^ {0,3}(#{1,6})[\t ]+(.+?)[\t ]*#*[\t ]*$`)
	htmlPattern                = regexp.MustCompile(`<[^>]+>`)
	inlineLink                 = regexp.MustCompile(`\[([^]]+)\]\([^)]+\)`)
	spacePattern               = regexp.MustCompile(`\s+`)
	referenceDefinitionPattern = regexp.MustCompile(`^ {0,3}\[[^]]+\]:[\t ]*(?:<([^>]+)>|([^\t ]+))`)
	htmlDestinationPattern     = regexp.MustCompile(`(?i)(?:href|src)[\t ]*=[\t ]*["']([^"']+)["']`)
	translationMetadataPattern = regexp.MustCompile(`^<!-- translation: locale=([^;[:space:]]+); source=([^;[:space:]]+); source-sha256=([^;[:space:]]+) -->$`)
	translationHashPattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	rootTranslationPattern     = regexp.MustCompile(`^README\.([^.]+)\.md$`)
)

const translationHeaderMaxLine = 20

var supportedTranslationLocales = map[string]struct{}{
	"ru":    {},
	"zh-CN": {},
}

type codeFence struct {
	marker byte
	length int
}

type document struct {
	path        string
	anchors     map[string]struct{}
	links       []documentLink
	translation *translationMetadata
}

type documentLink struct {
	line   int
	target string
}

type translationMetadata struct {
	line         int
	locale       string
	source       string
	sourceSHA256 string
}

func main() {
	root, err := repositoryRoot()
	if err != nil {
		fatalf("locate repository: %v", err)
	}
	files, err := markdownFiles(root)
	if err != nil {
		fatalf("list Markdown files: %v", err)
	}

	documents := make(map[string]document, len(files))
	var problems []string
	for _, path := range files {
		doc, docProblems, err := parseDocument(root, path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		documents[path] = doc
		problems = append(problems, docProblems...)
	}

	for _, path := range files {
		doc, ok := documents[path]
		if !ok {
			continue
		}
		for _, link := range doc.links {
			problem := validateLink(root, doc, link, documents)
			if problem != "" {
				problems = append(problems, problem)
			}
		}
	}
	translationProblems, translationWarnings := validateTranslations(root, documents)
	problems = append(problems, translationProblems...)
	problems = append(problems, orphanedDocuments(documents)...)

	sort.Strings(translationWarnings)
	for _, warning := range translationWarnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
	}
	if len(problems) != 0 {
		sort.Strings(problems)
		for _, problem := range problems {
			fmt.Fprintln(os.Stderr, problem)
		}
		os.Exit(1)
	}
	fmt.Printf("documentation checks passed (%d Markdown files)\n", len(files))
}

func repositoryRoot() (string, error) {
	command := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return filepath.Clean(strings.TrimSpace(string(output))), nil
}

func markdownFiles(root string) ([]string, error) {
	command := exec.Command("git", "ls-files", "-co", "--exclude-standard", "-z", "--", "*.md")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return nil, err
	}
	parts := bytes.Split(output, []byte{0})
	files := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		files = append(files, filepath.ToSlash(filepath.Clean(string(part))))
	}
	sort.Strings(files)
	return files, nil
}

func parseDocument(root, path string) (document, []string, error) {
	file, err := os.Open(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return document{}, nil, err
	}
	defer file.Close()

	doc := document{path: path, anchors: make(map[string]struct{})}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	var fence codeFence
	inComment := false
	h1Count := 0
	lineNumber := 0
	firstNonBlankLine := 0
	var problems []string
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine != "" && firstNonBlankLine == 0 {
			firstNonBlankLine = lineNumber
		}
		if fence.length == 0 && !inComment && strings.HasPrefix(trimmedLine, "<!-- translation:") {
			match := translationMetadataPattern.FindStringSubmatch(trimmedLine)
			switch {
			case match == nil:
				problems = append(problems, fmt.Sprintf("%s:%d: malformed translation metadata", path, lineNumber))
			case doc.translation != nil:
				problems = append(problems, fmt.Sprintf("%s:%d: duplicate translation metadata", path, lineNumber))
			default:
				doc.translation = &translationMetadata{
					line:         lineNumber,
					locale:       match[1],
					source:       match[2],
					sourceSHA256: match[3],
				}
				if firstNonBlankLine != lineNumber {
					problems = append(problems, fmt.Sprintf("%s:%d: translation metadata must be the first non-blank line", path, lineNumber))
				}
			}
		}
		if fence.length != 0 {
			if closesCodeFence(line, fence) {
				fence = codeFence{}
			}
			continue
		}
		if opened, ok := opensCodeFence(line); ok {
			fence = opened
			continue
		}
		line = stripHTMLComments(line, &inComment)

		if match := headingPattern.FindStringSubmatch(line); match != nil {
			if len(match[1]) == 1 {
				h1Count++
			}
			anchor := uniqueAnchor(githubSlugBase(match[2]), doc.anchors)
			doc.anchors[anchor] = struct{}{}
		}
		linkLine := stripInlineCode(line)
		for _, target := range extractLinkTargets(linkLine) {
			doc.links = append(doc.links, documentLink{line: lineNumber, target: target})
		}
		if match := referenceDefinitionPattern.FindStringSubmatch(linkLine); match != nil {
			target := match[1]
			if target == "" {
				target = match[2]
			}
			doc.links = append(doc.links, documentLink{line: lineNumber, target: target})
		}
		for _, match := range htmlDestinationPattern.FindAllStringSubmatch(linkLine, -1) {
			doc.links = append(doc.links, documentLink{line: lineNumber, target: match[1]})
		}
	}
	if err := scanner.Err(); err != nil {
		return document{}, nil, err
	}

	if fence.length != 0 {
		problems = append(problems, fmt.Sprintf("%s:%d: unclosed %s code fence", path, lineNumber, strings.Repeat(string(fence.marker), fence.length)))
	}
	if h1Count != 1 {
		problems = append(problems, fmt.Sprintf("%s: expected exactly one H1 heading, found %d", path, h1Count))
	}
	return doc, problems, nil
}

func opensCodeFence(line string) (codeFence, bool) {
	indent := 0
	for indent < len(line) && line[indent] == ' ' {
		indent++
	}
	if indent > 3 || indent == len(line) {
		return codeFence{}, false
	}
	marker := line[indent]
	if marker != '`' && marker != '~' {
		return codeFence{}, false
	}
	end := indent
	for end < len(line) && line[end] == marker {
		end++
	}
	if end-indent < 3 {
		return codeFence{}, false
	}
	return codeFence{marker: marker, length: end - indent}, true
}

func closesCodeFence(line string, fence codeFence) bool {
	indent := 0
	for indent < len(line) && line[indent] == ' ' {
		indent++
	}
	if indent > 3 || indent == len(line) || line[indent] != fence.marker {
		return false
	}
	end := indent
	for end < len(line) && line[end] == fence.marker {
		end++
	}
	return end-indent >= fence.length && strings.TrimSpace(line[end:]) == ""
}

func stripHTMLComments(line string, inComment *bool) string {
	var visible strings.Builder
	for len(line) > 0 {
		if *inComment {
			end := strings.Index(line, "-->")
			if end < 0 {
				return visible.String()
			}
			line = line[end+3:]
			*inComment = false
			continue
		}
		start := strings.Index(line, "<!--")
		if start < 0 {
			visible.WriteString(line)
			break
		}
		visible.WriteString(line[:start])
		line = line[start+4:]
		*inComment = true
	}
	return visible.String()
}

func stripInlineCode(line string) string {
	var visible strings.Builder
	for index := 0; index < len(line); {
		if line[index] != '`' {
			visible.WriteByte(line[index])
			index++
			continue
		}
		endMarker := index
		for endMarker < len(line) && line[endMarker] == '`' {
			endMarker++
		}
		marker := line[index:endMarker]
		closeOffset := strings.Index(line[endMarker:], marker)
		if closeOffset < 0 {
			visible.WriteString(line[index:])
			break
		}
		index = endMarker + closeOffset + len(marker)
	}
	return visible.String()
}

func extractLinkTargets(line string) []string {
	var targets []string
	for cursor := 0; cursor < len(line); {
		relativeStart := strings.Index(line[cursor:], "](")
		if relativeStart < 0 {
			break
		}
		start := cursor + relativeStart + 2
		for start < len(line) && (line[start] == ' ' || line[start] == '\t') {
			start++
		}
		if start >= len(line) {
			break
		}
		if line[start] == '<' {
			end := strings.IndexByte(line[start+1:], '>')
			if end >= 0 {
				targets = append(targets, line[start+1:start+1+end])
				cursor = start + end + 2
				continue
			}
		}

		depth := 0
		end := start
		for end < len(line) {
			switch line[end] {
			case '\\':
				if end+1 < len(line) {
					end += 2
					continue
				}
			case '(':
				depth++
			case ')':
				if depth == 0 {
					if end > start {
						targets = append(targets, line[start:end])
					}
					cursor = end + 1
					goto nextLink
				}
				depth--
			case ' ', '\t':
				if depth == 0 {
					if end > start {
						targets = append(targets, line[start:end])
					}
					cursor = end + 1
					goto nextLink
				}
			}
			end++
		}
		break
	nextLink:
	}
	return targets
}

func githubSlugBase(heading string) string {
	heading = inlineLink.ReplaceAllString(heading, "$1")
	heading = htmlPattern.ReplaceAllString(heading, "")
	heading = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsLetter(r), unicode.IsNumber(r), unicode.IsMark(r):
			return unicode.ToLower(r)
		case unicode.IsSpace(r), r == '-', r == '_':
			return r
		default:
			return -1
		}
	}, heading)
	heading = strings.TrimSpace(heading)
	return spacePattern.ReplaceAllString(heading, "-")
}

func uniqueAnchor(base string, existing map[string]struct{}) string {
	if _, found := existing[base]; !found {
		return base
	}
	for suffix := 1; ; suffix++ {
		candidate := fmt.Sprintf("%s-%d", base, suffix)
		if _, found := existing[candidate]; !found {
			return candidate
		}
	}
}

func validateLink(root string, doc document, link documentLink, documents map[string]document) string {
	target, err := url.Parse(link.target)
	if err != nil {
		return fmt.Sprintf("%s:%d: invalid link %q: %v", doc.path, link.line, link.target, err)
	}
	if target.IsAbs() || target.Host != "" || strings.HasPrefix(link.target, "//") {
		return ""
	}
	if target.Path == "" && target.Fragment == "" {
		return fmt.Sprintf("%s:%d: empty link target", doc.path, link.line)
	}
	if strings.HasPrefix(target.Path, "/") {
		return fmt.Sprintf("%s:%d: repository-local link must be relative: %s", doc.path, link.line, link.target)
	}

	targetPath := doc.path
	if target.Path != "" {
		decoded, err := url.PathUnescape(target.Path)
		if err != nil {
			return fmt.Sprintf("%s:%d: invalid escaped path %q: %v", doc.path, link.line, link.target, err)
		}
		targetPath = filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(doc.path), filepath.FromSlash(decoded))))
		if targetPath == ".." || strings.HasPrefix(targetPath, "../") {
			return fmt.Sprintf("%s:%d: link escapes repository: %s", doc.path, link.line, link.target)
		}
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(targetPath)))
		if err != nil {
			return fmt.Sprintf("%s:%d: missing link target: %s", doc.path, link.line, link.target)
		}
		if info.IsDir() {
			return fmt.Sprintf("%s:%d: link must name a file, not a directory: %s", doc.path, link.line, link.target)
		}
	}

	if target.Fragment == "" || !strings.EqualFold(filepath.Ext(targetPath), ".md") {
		return ""
	}
	targetDoc, ok := documents[targetPath]
	if !ok {
		return fmt.Sprintf("%s:%d: Markdown anchor target is not tracked: %s", doc.path, link.line, link.target)
	}
	fragment, err := url.PathUnescape(target.Fragment)
	if err != nil {
		return fmt.Sprintf("%s:%d: invalid escaped anchor %q: %v", doc.path, link.line, link.target, err)
	}
	if _, ok := targetDoc.anchors[strings.ToLower(fragment)]; !ok {
		return fmt.Sprintf("%s:%d: missing Markdown anchor: %s", doc.path, link.line, link.target)
	}
	return ""
}

func validateTranslations(root string, documents map[string]document) ([]string, []string) {
	var problems []string
	var warnings []string
	translations := make(map[string]string)

	paths := make([]string, 0, len(documents))
	for path := range documents {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, documentPath := range paths {
		doc := documents[documentPath]
		expectedLocale, translationPath := translationLocaleForPath(documentPath)
		metadata := doc.translation
		if translationPath {
			if _, ok := supportedTranslationLocales[expectedLocale]; !ok {
				problems = append(problems, fmt.Sprintf("%s: unsupported translation locale %q", documentPath, expectedLocale))
			}
			if metadata == nil {
				problems = append(problems, fmt.Sprintf("%s: translation metadata is missing", documentPath))
				continue
			}
			if metadata.locale != expectedLocale {
				problems = append(problems, fmt.Sprintf("%s:%d: translation locale %q does not match path locale %q", documentPath, metadata.line, metadata.locale, expectedLocale))
			}
		} else if metadata != nil {
			problems = append(problems, fmt.Sprintf("%s:%d: translation metadata is only allowed on localized documents", documentPath, metadata.line))
		}
		if metadata == nil {
			continue
		}
		if _, ok := supportedTranslationLocales[metadata.locale]; !ok {
			problems = append(problems, fmt.Sprintf("%s:%d: unsupported metadata locale %q", documentPath, metadata.line, metadata.locale))
		}
		if !translationHashPattern.MatchString(metadata.sourceSHA256) {
			problems = append(problems, fmt.Sprintf("%s:%d: source-sha256 must be 64 lowercase hexadecimal characters", documentPath, metadata.line))
		}

		source := metadata.source
		if problem := validateCanonicalSourcePath(source); problem != "" {
			problems = append(problems, fmt.Sprintf("%s:%d: %s", documentPath, metadata.line, problem))
			continue
		}
		if _, localized := translationLocaleForPath(source); localized || strings.HasPrefix(source, "docs/i18n/") {
			problems = append(problems, fmt.Sprintf("%s:%d: translation source must be canonical Markdown outside docs/i18n: %s", documentPath, metadata.line, source))
			continue
		}
		sourceDoc, ok := documents[source]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s:%d: translation source does not exist: %s", documentPath, metadata.line, source))
			continue
		}
		if sourceDoc.translation != nil {
			problems = append(problems, fmt.Sprintf("%s:%d: translation source is not canonical: %s", documentPath, metadata.line, source))
			continue
		}

		key := metadata.locale + "\x00" + source
		if previous, duplicate := translations[key]; duplicate {
			problems = append(problems, fmt.Sprintf("%s:%d: duplicate %s translation for %s; already provided by %s", documentPath, metadata.line, metadata.locale, source, previous))
		} else {
			translations[key] = documentPath
		}

		if !hasVisibleCanonicalLink(doc, source) {
			problems = append(problems, fmt.Sprintf("%s: translated document must visibly link to %s within its first %d lines", documentPath, source, translationHeaderMaxLine))
		}

		if translationHashPattern.MatchString(metadata.sourceSHA256) {
			content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(source)))
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s:%d: read translation source %s: %v", documentPath, metadata.line, source, err))
				continue
			}
			actualHash := fmt.Sprintf("%x", sha256.Sum256(content))
			if actualHash != metadata.sourceSHA256 {
				warnings = append(warnings, fmt.Sprintf("%s:%d: translation may be stale: %s has SHA-256 %s, metadata records %s", documentPath, metadata.line, source, actualHash, metadata.sourceSHA256))
			}
		}
	}

	return problems, warnings
}

func translationLocaleForPath(documentPath string) (string, bool) {
	if match := rootTranslationPattern.FindStringSubmatch(documentPath); match != nil {
		return match[1], true
	}
	const prefix = "docs/i18n/"
	if !strings.HasPrefix(documentPath, prefix) {
		return "", false
	}
	remainder := strings.TrimPrefix(documentPath, prefix)
	separator := strings.IndexByte(remainder, '/')
	if separator <= 0 || separator == len(remainder)-1 {
		return "", false
	}
	return remainder[:separator], true
}

func validateCanonicalSourcePath(source string) string {
	if source == "" {
		return "translation source is empty"
	}
	if strings.Contains(source, `\`) || strings.HasPrefix(source, "/") {
		return fmt.Sprintf("translation source must be a repository-relative slash path: %s", source)
	}
	cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(source)))
	if cleaned != source || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Sprintf("translation source must be a normalized repository path: %s", source)
	}
	if !strings.EqualFold(filepath.Ext(source), ".md") {
		return fmt.Sprintf("translation source must be Markdown: %s", source)
	}
	return ""
}

func hasVisibleCanonicalLink(doc document, source string) bool {
	for _, link := range doc.links {
		if link.line > translationHeaderMaxLine {
			continue
		}
		if resolvedRepositoryLink(doc.path, link.target) == source {
			return true
		}
	}
	return false
}

func resolvedRepositoryLink(documentPath, rawTarget string) string {
	target, err := url.Parse(rawTarget)
	if err != nil || target.IsAbs() || target.Host != "" || target.Path == "" || strings.HasPrefix(target.Path, "/") {
		return ""
	}
	decoded, err := url.PathUnescape(target.Path)
	if err != nil {
		return ""
	}
	resolved := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(documentPath), filepath.FromSlash(decoded))))
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return ""
	}
	return resolved
}

func orphanedDocuments(documents map[string]document) []string {
	if _, ok := documents["README.md"]; !ok {
		return []string{"README.md: documentation root is missing"}
	}
	visited := map[string]struct{}{"README.md": {}}
	queue := []string{"README.md"}
	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]
		for _, link := range documents[path].links {
			target, err := url.Parse(link.target)
			if err != nil || target.IsAbs() || target.Path == "" {
				continue
			}
			decoded, err := url.PathUnescape(target.Path)
			if err != nil {
				continue
			}
			linkedPath := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(path), filepath.FromSlash(decoded))))
			if _, ok := documents[linkedPath]; !ok {
				continue
			}
			if _, ok := visited[linkedPath]; ok {
				continue
			}
			visited[linkedPath] = struct{}{}
			queue = append(queue, linkedPath)
		}
	}

	var problems []string
	for path := range documents {
		if _, ok := visited[path]; ok || versionedDocument(path) {
			continue
		}
		problems = append(problems, fmt.Sprintf("%s: document is not reachable from README.md", path))
	}
	return problems
}

func versionedDocument(path string) bool {
	return strings.HasPrefix(path, "docs/releases/") ||
		strings.HasPrefix(path, "docs/development/acceptance/")
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
