package sourceoracle

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	exportConstPattern = regexp.MustCompile(`\bexport\s+const\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=`)
	symbolPattern      = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*(?:\.[A-Za-z_$][A-Za-z0-9_$]*)*$`)
)

// parseExportedConstants intentionally supports only the TypeScript constant
// subset used by the pinned API catalog. Unsupported syntax is an error rather
// than a guessed extraction.
func parseExportedConstants(source string) (map[string]string, error) {
	result := make(map[string]string)
	matches := exportConstPattern.FindAllStringSubmatchIndex(maskTypeScriptNonCode(source), -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("no exported constants found")
	}
	seenExports := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		name := source[match[2]:match[3]]
		if _, exists := seenExports[name]; exists {
			return nil, fmt.Errorf("exported constant %s is declared more than once", name)
		}
		seenExports[name] = struct{}{}
		parser := typescriptValueParser{source: source, offset: match[1]}
		parser.skipTrivia()
		if parser.atEnd() {
			return nil, fmt.Errorf("%s has no value", name)
		}
		if parser.peek() == '{' {
			if err := parser.parseObject(name, result); err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			if err := parser.parseConstTerminator(); err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			continue
		}
		expression, err := parser.parseScalarExpression()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if err := parser.parseConstTerminator(); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		result[name] = expression
	}
	return result, nil
}

func (parser *typescriptValueParser) parseConstTerminator() error {
	parser.skipTrivia()
	if parser.consumeKeyword("as") {
		parser.skipTrivia()
		if !parser.consumeKeyword("const") {
			return fmt.Errorf("unsupported type assertion after exported constant")
		}
		parser.skipTrivia()
	}
	if !parser.consume(';') {
		return fmt.Errorf("unsupported expression suffix at byte %d", parser.offset)
	}
	return nil
}

func (parser *typescriptValueParser) consumeKeyword(keyword string) bool {
	if !strings.HasPrefix(parser.source[parser.offset:], keyword) {
		return false
	}
	end := parser.offset + len(keyword)
	if end < len(parser.source) && isIdentifierContinue(parser.source[end]) {
		return false
	}
	parser.offset = end
	return true
}

func maskTypeScriptNonCode(source string) string {
	return maskTypeScript(source)
}

// maskTypeScript preserves byte offsets while hiding comments and quoted
// content from regular expressions that locate code tokens.
func maskTypeScript(source string) string {
	result := []byte(source)
	for index := 0; index < len(result); {
		switch {
		case strings.HasPrefix(source[index:], "//"):
			for index < len(result) && result[index] != '\n' {
				result[index] = ' '
				index++
			}
		case strings.HasPrefix(source[index:], "/*"):
			result[index] = ' '
			result[index+1] = ' '
			index += 2
			for index < len(result) && !strings.HasPrefix(source[index:], "*/") {
				if result[index] != '\n' && result[index] != '\r' {
					result[index] = ' '
				}
				index++
			}
			if index < len(result) {
				result[index] = ' '
				result[index+1] = ' '
				index += 2
			}
		case source[index] == '\'' || source[index] == '"' || source[index] == '`':
			quote := source[index]
			result[index] = ' '
			index++
			for index < len(result) {
				current := source[index]
				if current != '\n' && current != '\r' {
					result[index] = ' '
				}
				index++
				if current == '\\' && index < len(result) {
					if result[index] != '\n' && result[index] != '\r' {
						result[index] = ' '
					}
					index++
					continue
				}
				if current == quote {
					break
				}
			}
		case source[index] == '/' && isTypeScriptRegexpStart(source, index):
			index = maskTypeScriptRegexp(source, result, index)
		default:
			index++
		}
	}
	return string(result)
}

// isTypeScriptRegexpStart deliberately recognizes the expression contexts
// used by regular-expression literals. A slash after a value remains visible
// as a division operator; a slash after an operator or statement keyword is
// masked as the start of a regexp literal.
func isTypeScriptRegexpStart(source string, slash int) bool {
	previous := slash - 1
	for previous >= 0 && isSpace(source[previous]) {
		previous--
	}
	if previous < 0 {
		return true
	}
	if strings.ContainsRune("=([{,:;!&|?+-*%^~<>", rune(source[previous])) {
		return true
	}
	if !isIdentifierContinue(source[previous]) {
		return false
	}
	start := previous
	for start > 0 && isIdentifierContinue(source[start-1]) {
		start--
	}
	switch source[start : previous+1] {
	case "await", "case", "delete", "do", "else", "in", "instanceof", "new", "return", "throw", "typeof", "void", "yield":
		return true
	default:
		return false
	}
}

func maskTypeScriptRegexp(source string, result []byte, start int) int {
	result[start] = ' '
	inCharacterClass := false
	for index := start + 1; index < len(result); index++ {
		current := source[index]
		if current == '\n' || current == '\r' {
			return index
		}
		result[index] = ' '
		if current == '\\' && index+1 < len(result) {
			index++
			if source[index] != '\n' && source[index] != '\r' {
				result[index] = ' '
			}
			continue
		}
		switch current {
		case '[':
			inCharacterClass = true
		case ']':
			inCharacterClass = false
		case '/':
			if inCharacterClass {
				continue
			}
			index++
			for index < len(result) && isIdentifierContinue(source[index]) {
				result[index] = ' '
				index++
			}
			return index
		}
	}
	return len(result)
}

type typescriptValueParser struct {
	source string
	offset int
}

func (parser *typescriptValueParser) parseObject(prefix string, result map[string]string) error {
	if !parser.consume('{') {
		return fmt.Errorf("expected object")
	}
	seenKeys := make(map[string]struct{})
	for {
		parser.skipTrivia()
		if parser.atEnd() {
			return fmt.Errorf("unterminated object")
		}
		if parser.consume('}') {
			return nil
		}
		key, err := parser.parseIdentifier()
		if err != nil {
			return err
		}
		if _, exists := seenKeys[key]; exists {
			return fmt.Errorf("object key %s is declared more than once", key)
		}
		seenKeys[key] = struct{}{}
		parser.skipTrivia()
		if !parser.consume(':') {
			return fmt.Errorf("object key %s has no colon", key)
		}
		parser.skipTrivia()
		name := prefix + "." + key
		if !parser.atEnd() && parser.peek() == '{' {
			if err := parser.parseObject(name, result); err != nil {
				return err
			}
		} else {
			expression, err := parser.parseScalarExpression()
			if err != nil {
				return fmt.Errorf("object key %s: %w", key, err)
			}
			result[name] = expression
		}
		parser.skipTrivia()
		if parser.consume(',') {
			continue
		}
		if !parser.atEnd() && parser.peek() == '}' {
			continue
		}
		return fmt.Errorf("object key %s is not followed by a comma or closing brace", key)
	}
}

func (parser *typescriptValueParser) parseScalarExpression() (string, error) {
	parser.skipTrivia()
	if parser.atEnd() {
		return "", fmt.Errorf("missing scalar expression")
	}
	start := parser.offset
	switch parser.peek() {
	case '\'', '"', '`':
		quote := parser.peek()
		parser.offset++
		for !parser.atEnd() {
			current := parser.peek()
			parser.offset++
			if current == '\\' {
				if parser.atEnd() {
					return "", fmt.Errorf("unterminated escape sequence")
				}
				parser.offset++
				continue
			}
			if current == quote {
				return parser.source[start:parser.offset], nil
			}
		}
		return "", fmt.Errorf("unterminated quoted expression")
	default:
		identifier, err := parser.parseDottedIdentifier()
		if err != nil {
			return "", fmt.Errorf("unsupported scalar expression at byte %d", parser.offset)
		}
		return identifier, nil
	}
}

func (parser *typescriptValueParser) parseIdentifier() (string, error) {
	if parser.atEnd() || !isIdentifierStart(parser.peek()) {
		return "", fmt.Errorf("expected identifier at byte %d", parser.offset)
	}
	start := parser.offset
	parser.offset++
	for !parser.atEnd() && isIdentifierContinue(parser.peek()) {
		parser.offset++
	}
	return parser.source[start:parser.offset], nil
}

func (parser *typescriptValueParser) parseDottedIdentifier() (string, error) {
	start := parser.offset
	if _, err := parser.parseIdentifier(); err != nil {
		return "", err
	}
	for !parser.atEnd() && parser.peek() == '.' {
		parser.offset++
		if _, err := parser.parseIdentifier(); err != nil {
			return "", err
		}
	}
	return parser.source[start:parser.offset], nil
}

func (parser *typescriptValueParser) skipTrivia() {
	for !parser.atEnd() {
		switch {
		case isSpace(parser.peek()):
			parser.offset++
		case strings.HasPrefix(parser.source[parser.offset:], "//"):
			if newline := strings.IndexByte(parser.source[parser.offset+2:], '\n'); newline >= 0 {
				parser.offset += newline + 3
			} else {
				parser.offset = len(parser.source)
			}
		case strings.HasPrefix(parser.source[parser.offset:], "/*"):
			end := strings.Index(parser.source[parser.offset+2:], "*/")
			if end < 0 {
				parser.offset = len(parser.source)
				return
			}
			parser.offset += end + 4
		default:
			return
		}
	}
}

func (parser *typescriptValueParser) consume(want byte) bool {
	if parser.atEnd() || parser.peek() != want {
		return false
	}
	parser.offset++
	return true
}

func (parser *typescriptValueParser) atEnd() bool {
	return parser.offset >= len(parser.source)
}

func (parser *typescriptValueParser) peek() byte {
	return parser.source[parser.offset]
}

func isSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n'
}

func isIdentifierStart(value byte) bool {
	return value == '_' || value == '$' || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func isIdentifierContinue(value byte) bool {
	return isIdentifierStart(value) || value >= '0' && value <= '9'
}

type expressionResolver struct {
	expressions map[string]string
}

func (resolver expressionResolver) resolve(name string) (string, error) {
	return resolver.resolveWithStack(name, make(map[string]bool))
}

func (resolver expressionResolver) resolveWithStack(name string, stack map[string]bool) (string, error) {
	expression, ok := resolver.expressions[name]
	if !ok && strings.HasPrefix(name, "CONTROLLERS.") {
		name = strings.TrimPrefix(name, "CONTROLLERS.")
		expression, ok = resolver.expressions[name]
	}
	if !ok {
		return "", fmt.Errorf("unknown exported constant %s", name)
	}
	if stack[name] {
		return "", fmt.Errorf("cyclic exported constant %s", name)
	}
	stack[name] = true
	defer delete(stack, name)
	return resolver.evaluate(expression, stack)
}

func (resolver expressionResolver) evaluate(expression string, stack map[string]bool) (string, error) {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return "", fmt.Errorf("empty expression")
	}
	if stack == nil {
		stack = make(map[string]bool)
	}
	if symbolPattern.MatchString(expression) {
		return resolver.resolveWithStack(expression, stack)
	}
	switch expression[0] {
	case '\'', '"':
		return decodeQuotedLiteral(expression)
	case '`':
		return resolver.evaluateTemplate(expression, stack)
	default:
		return "", fmt.Errorf("unsupported expression %q", expression)
	}
}

func (resolver expressionResolver) evaluateTemplate(expression string, stack map[string]bool) (string, error) {
	if len(expression) < 2 || expression[len(expression)-1] != '`' {
		return "", fmt.Errorf("unterminated template expression")
	}
	content := expression[1 : len(expression)-1]
	var result strings.Builder
	for len(content) != 0 {
		placeholder := strings.Index(content, "${")
		if placeholder < 0 {
			literal, err := decodeTemplateLiteral(content)
			if err != nil {
				return "", err
			}
			result.WriteString(literal)
			break
		}
		literal, err := decodeTemplateLiteral(content[:placeholder])
		if err != nil {
			return "", err
		}
		result.WriteString(literal)
		end := strings.IndexByte(content[placeholder+2:], '}')
		if end < 0 {
			return "", fmt.Errorf("unterminated template placeholder")
		}
		end += placeholder + 2
		symbol := strings.TrimSpace(content[placeholder+2 : end])
		if !symbolPattern.MatchString(symbol) {
			return "", fmt.Errorf("unsupported template placeholder %q", symbol)
		}
		value, err := resolver.resolveWithStack(symbol, stack)
		if err != nil {
			return "", err
		}
		result.WriteString(value)
		content = content[end+1:]
	}
	return result.String(), nil
}

func decodeQuotedLiteral(expression string) (string, error) {
	if len(expression) < 2 || expression[len(expression)-1] != expression[0] {
		return "", fmt.Errorf("unterminated quoted literal")
	}
	return decodeEscapes(expression[1 : len(expression)-1])
}

func decodeTemplateLiteral(value string) (string, error) {
	return decodeEscapes(value)
}

func decodeEscapes(value string) (string, error) {
	var result strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] != '\\' {
			result.WriteByte(value[index])
			continue
		}
		index++
		if index == len(value) {
			return "", fmt.Errorf("unterminated escape sequence")
		}
		switch value[index] {
		case '\\', '\'', '"', '`', '$':
			result.WriteByte(value[index])
		case 'n':
			result.WriteByte('\n')
		case 'r':
			result.WriteByte('\r')
		case 't':
			result.WriteByte('\t')
		default:
			return "", fmt.Errorf("unsupported escape sequence \\%c", value[index])
		}
	}
	return result.String(), nil
}
