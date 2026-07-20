package sourceoracle

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	apiControllersDirectory = "libs/contract/api/controllers"
	internalConstantsDir    = "libs/contract/constants/internal"
	apiRoutesSource         = "libs/contract/api/routes.ts"
	mainSource              = "src/main.ts"
	rootModuleSource        = "src/app.module.ts"
	sourceDirectory         = "src"
)

var (
	httpDecoratorPattern                = regexp.MustCompile(`@(Get|Post|Put|Patch|Delete|Options|Head|All|Sse)\s*\(\s*([^)]*?)\s*\)`)
	anyGlobalPrefixPattern              = regexp.MustCompile(`\.\s*setGlobalPrefix\s*\(`)
	qualifiedControllerDecoratorPattern = regexp.MustCompile(`@\s*[A-Za-z_$][A-Za-z0-9_$]*\s*\.\s*Controller\s*\(`)
)

func extractRoutes(
	ctx context.Context,
	reader gitObjectReader,
	controllerSources []string,
	excludedControllers []ExcludedController,
	evidenceFiles []string,
) ([]ManifestRoute, error) {
	evidenceSet := make(map[string]struct{}, len(evidenceFiles))
	for _, sourcePath := range evidenceFiles {
		evidenceSet[sourcePath] = struct{}{}
	}

	apiTree, err := reader.listFiles(ctx, apiControllersDirectory)
	if err != nil {
		return nil, err
	}
	constantsSources := make([]string, 0, len(apiTree))
	for _, sourcePath := range apiTree {
		if strings.HasSuffix(sourcePath, "/index.ts") {
			continue
		}
		if strings.HasSuffix(sourcePath, ".ts") {
			constantsSources = append(constantsSources, sourcePath)
			if _, exists := evidenceSet[sourcePath]; !exists {
				return nil, fmt.Errorf("discovered API constants source %s is missing from the evidence catalog", sourcePath)
			}
		}
	}
	constantsSources = sortedUnique(constantsSources)
	if len(constantsSources) == 0 {
		return nil, fmt.Errorf("official Git tree contains no API controller constants")
	}

	sourceTree, err := reader.listFiles(ctx, sourceDirectory)
	if err != nil {
		return nil, err
	}
	if err := verifyControllerSourceConventions(ctx, reader, sourceTree); err != nil {
		return nil, err
	}
	discoveredControllers, moduleSources := discoverNestSources(sourceTree)
	wantControllers := append([]string(nil), controllerSources...)
	for _, controller := range excludedControllers {
		wantControllers = append(wantControllers, controller.Source)
	}
	if difference := compareStrings(discoveredControllers, wantControllers); difference != "" {
		return nil, fmt.Errorf("official controller inventory differs from the audited public/excluded catalog: %s", difference)
	}
	for _, sourcePath := range append(append([]string(nil), discoveredControllers...), moduleSources...) {
		if _, exists := evidenceSet[sourcePath]; !exists {
			return nil, fmt.Errorf("discovered Nest source %s is missing from the evidence catalog", sourcePath)
		}
	}
	if err := verifyNestModuleReachability(ctx, reader, discoveredControllers, moduleSources); err != nil {
		return nil, err
	}

	expressions := make(map[string]string)
	for _, sourcePath := range constantsSources {
		raw, err := reader.readBlob(ctx, sourcePath)
		if err != nil {
			return nil, err
		}
		exported, err := parseExportedConstants(string(raw))
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", sourcePath, err)
		}
		if err := mergeExpressions(expressions, exported, sourcePath); err != nil {
			return nil, err
		}
	}
	internalTree, err := reader.listFiles(ctx, internalConstantsDir)
	if err != nil {
		return nil, err
	}
	for _, sourcePath := range internalTree {
		if strings.HasSuffix(sourcePath, "/index.ts") || !strings.HasSuffix(sourcePath, ".ts") {
			continue
		}
		if _, exists := evidenceSet[sourcePath]; !exists {
			return nil, fmt.Errorf("discovered internal constants source %s is missing from the evidence catalog", sourcePath)
		}
		raw, err := reader.readBlob(ctx, sourcePath)
		if err != nil {
			return nil, err
		}
		exported, err := parseExportedConstants(string(raw))
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", sourcePath, err)
		}
		if err := mergeExpressions(expressions, exported, sourcePath); err != nil {
			return nil, err
		}
	}

	routesRaw, err := reader.readBlob(ctx, apiRoutesSource)
	if err != nil {
		return nil, err
	}
	routesExpressions, err := parseExportedConstants(string(routesRaw))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", apiRoutesSource, err)
	}
	if err := mergeExpressions(expressions, routesExpressions, apiRoutesSource); err != nil {
		return nil, err
	}
	resolver := expressionResolver{expressions: expressions}
	root, err := resolver.resolve("ROOT")
	if err != nil {
		return nil, fmt.Errorf("resolve official API root: %w", err)
	}
	if err := validateAbsoluteRoute(root); err != nil {
		return nil, fmt.Errorf("official API root %q is not a canonical absolute path", root)
	}

	mainRaw, err := reader.readBlob(ctx, mainSource)
	if err != nil {
		return nil, err
	}
	bootstrap, err := parseNestBootstrap(string(mainRaw))
	if err != nil {
		return nil, fmt.Errorf("parse %s bootstrap: %w", mainSource, err)
	}
	prefixExclusions, err := parseGlobalPrefixExclusions(bootstrap)
	if err != nil {
		return nil, fmt.Errorf("parse %s global prefix: %w", mainSource, err)
	}
	for _, controller := range excludedControllers {
		for _, required := range controller.RequiredPrefixExclusions {
			if !containsString(prefixExclusions, required) {
				return nil, fmt.Errorf("%s does not exclude %s required by %s", mainSource, required, controller.Source)
			}
		}
	}
	requiredExclusions := make([]string, 0, len(prefixExclusions))
	for _, controller := range excludedControllers {
		requiredExclusions = append(requiredExclusions, controller.RequiredPrefixExclusions...)
	}
	if difference := compareStrings(prefixExclusions, requiredExclusions); difference != "" {
		return nil, fmt.Errorf("%s global-prefix exclusions differ from the audited internal routes: %s", mainSource, difference)
	}
	if err := verifyExcludedControllerRoutes(ctx, reader, resolver, excludedControllers); err != nil {
		return nil, err
	}

	restPaths := make(map[string]string)
	for name := range expressions {
		if !strings.HasPrefix(name, "REST_API.") {
			continue
		}
		value, err := resolver.resolve(name)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", name, err)
		}
		if !strings.HasPrefix(value, root+"/") {
			return nil, fmt.Errorf("%s resolves outside API root %s: %q", name, root, value)
		}
		if err := validateAbsoluteRoute(value); err != nil {
			return nil, fmt.Errorf("%s resolves to invalid route %q: %w", name, value, err)
		}
		if previous, exists := restPaths[value]; exists {
			return nil, fmt.Errorf("REST_API routes %s and %s both resolve to %s", previous, name, value)
		}
		restPaths[value] = name
	}
	if len(restPaths) == 0 {
		return nil, fmt.Errorf("%s exports no REST_API route leaves", apiRoutesSource)
	}

	result := make([]ManifestRoute, 0, len(restPaths))
	decoratedPaths := make(map[string]string, len(restPaths))
	for _, sourcePath := range controllerSources {
		raw, err := reader.readBlob(ctx, sourcePath)
		if err != nil {
			return nil, err
		}
		controllerClass, err := parseNestController(sourcePath, string(raw))
		if err != nil {
			return nil, err
		}
		controllerSymbol := strings.TrimSpace(controllerClass.decoratorArgument)
		if !symbolPattern.MatchString(controllerSymbol) {
			return nil, fmt.Errorf("%s @Controller argument %q is not a supported constant", sourcePath, controllerSymbol)
		}
		controller, err := resolver.resolve(controllerSymbol)
		if err != nil {
			return nil, fmt.Errorf("resolve %s controller %s: %w", sourcePath, controllerSymbol, err)
		}
		if err := validatePathSegment("controller", controller); err != nil {
			return nil, fmt.Errorf("%s: %w", sourcePath, err)
		}

		decorators := httpDecoratorPattern.FindAllStringSubmatchIndex(controllerClass.code, -1)
		if len(decorators) == 0 {
			return nil, fmt.Errorf("%s has no supported HTTP route decorators", sourcePath)
		}
		for _, match := range decorators {
			if match[0] <= controllerClass.bodyStart || match[1] >= controllerClass.bodyEnd {
				return nil, fmt.Errorf("%s has an HTTP route decorator outside exported controller class %s", sourcePath, controllerClass.name)
			}
			decoratorName := controllerClass.code[match[2]:match[3]]
			if err := requireDirectNestDecoratorImport(sourcePath, controllerClass.imports, decoratorName); err != nil {
				return nil, err
			}
			method := strings.ToUpper(decoratorName)
			if decoratorName == "Sse" {
				method = "GET"
			}
			decoratorExpression := strings.TrimSpace(controllerClass.code[match[4]:match[5]])
			if decoratorExpression == "" {
				return nil, fmt.Errorf("%s contains @%s without a route expression", sourcePath, decoratorName)
			}
			routePart, err := resolver.evaluate(decoratorExpression, nil)
			if err != nil {
				return nil, fmt.Errorf("resolve %s decorator %s: %w", sourcePath, decoratorExpression, err)
			}
			if err := validateRelativeRoute(routePart); err != nil {
				return nil, fmt.Errorf("%s decorator %s: %w", sourcePath, decoratorExpression, err)
			}
			fullPath := root + "/" + controller + "/" + routePart
			restName, exists := restPaths[fullPath]
			if !exists {
				return nil, fmt.Errorf("%s decorator %s resolves to %s, which is absent from REST_API", sourcePath, decoratorExpression, fullPath)
			}
			if previous, exists := decoratedPaths[fullPath]; exists {
				return nil, fmt.Errorf("official path %s is implemented by both %s and %s", fullPath, previous, sourcePath)
			}
			decoratedPaths[fullPath] = sourcePath
			result = append(result, ManifestRoute{
				Method:           method,
				Path:             fullPath,
				ControllerSource: sourcePath,
				Decorator:        decoratorExpression + " (" + restName + ")",
			})
		}
	}

	var missing []string
	for restPath, restName := range restPaths {
		if _, exists := decoratedPaths[restPath]; !exists {
			missing = append(missing, restName+"="+restPath)
		}
	}
	if len(missing) != 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("REST_API routes have no matching controller decorator: %s", strings.Join(missing, ", "))
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Path != result[j].Path {
			return result[i].Path < result[j].Path
		}
		if result[i].Method != result[j].Method {
			return result[i].Method < result[j].Method
		}
		return result[i].ControllerSource < result[j].ControllerSource
	})
	return result, nil
}

func verifyControllerSourceConventions(
	ctx context.Context,
	reader gitObjectReader,
	treeFiles []string,
) error {
	for _, sourcePath := range treeFiles {
		if !strings.HasSuffix(sourcePath, ".ts") {
			continue
		}
		raw, err := reader.readBlob(ctx, sourcePath)
		if err != nil {
			return err
		}
		code := maskTypeScriptNonCode(string(raw))
		if qualifiedControllerDecoratorPattern.MatchString(code) {
			return fmt.Errorf("official source %s uses an unsupported qualified @Controller decorator", sourcePath)
		}
		hasController := controllerDecoratorPattern.MatchString(code)
		bindings, err := parseStaticImports(string(raw))
		if err != nil {
			return fmt.Errorf("parse imports in %s: %w", sourcePath, err)
		}
		for _, binding := range bindings {
			if binding.source != "@nestjs/common" {
				continue
			}
			if binding.kind == importNamespace && !strings.HasSuffix(sourcePath, ".controller.ts") {
				return fmt.Errorf("official source %s uses an unsupported namespace import from @nestjs/common outside a controller", sourcePath)
			}
			if binding.imported != "Controller" {
				continue
			}
			if !strings.HasSuffix(sourcePath, ".controller.ts") {
				return fmt.Errorf("official source %s imports Controller outside a *.controller.ts file", sourcePath)
			}
			if binding.local == "Controller" {
				continue
			}
			aliasPattern := regexp.MustCompile(`@\s*` + regexp.QuoteMeta(binding.local) + `\s*\(`)
			if aliasPattern.MatchString(code) {
				return fmt.Errorf("official source %s aliases @Controller as @%s", sourcePath, binding.local)
			}
		}
		if hasController && !strings.HasSuffix(sourcePath, ".controller.ts") {
			return fmt.Errorf("official source %s declares @Controller outside a *.controller.ts file", sourcePath)
		}
	}
	return nil
}

func parseGlobalPrefixExclusions(bootstrap nestBootstrap) ([]string, error) {
	if binding, exists := bootstrap.bindings["ROOT"]; !exists || !binding.isDirectNamed("ROOT", "@libs/contracts/api") {
		return nil, fmt.Errorf("ROOT must be a direct named import from @libs/contracts/api")
	}
	allCalls := anyGlobalPrefixPattern.FindAllStringIndex(bootstrap.code, -1)
	if len(allCalls) != 1 {
		return nil, fmt.Errorf("found %d setGlobalPrefix calls, want exactly one", len(allCalls))
	}
	receiverPattern := regexp.MustCompile(
		`\b` + regexp.QuoteMeta(bootstrap.applicationVariable) + `\s*\.\s*setGlobalPrefix\s*\(`,
	)
	matches := receiverPattern.FindAllStringIndex(bootstrap.code, -1)
	if len(matches) != 1 {
		return nil, fmt.Errorf("setGlobalPrefix is not called on bootstrapped application %s", bootstrap.applicationVariable)
	}
	if matches[0][0] <= bootstrap.functionBodyStart || matches[0][1] >= bootstrap.functionBodyEnd {
		return nil, fmt.Errorf("setGlobalPrefix is not owned by async bootstrap function")
	}
	if depth, err := braceDepth(bootstrap.code, bootstrap.functionBodyStart+1, matches[0][0]); err != nil || depth != 0 {
		return nil, fmt.Errorf("setGlobalPrefix must be a top-level bootstrap statement")
	}
	open := matches[0][1] - 1
	end, err := balancedEnd(bootstrap.code, open, '(', ')')
	if err != nil {
		return nil, err
	}
	arguments, err := splitTopLevel(bootstrap.code[open+1:end], ',')
	if err != nil {
		return nil, err
	}
	if len(arguments) != 2 {
		return nil, fmt.Errorf("setGlobalPrefix has %d arguments, want ROOT and one options object", len(arguments))
	}
	if strings.TrimSpace(arguments[0]) != "ROOT" {
		return nil, fmt.Errorf("setGlobalPrefix first argument is %q, want ROOT", strings.TrimSpace(arguments[0]))
	}
	properties, err := parseObjectLiteral(arguments[1])
	if err != nil {
		return nil, fmt.Errorf("parse setGlobalPrefix options: %w", err)
	}
	exclusions, err := parseBareIdentifierArray(properties["exclude"])
	if err != nil {
		return nil, fmt.Errorf("parse setGlobalPrefix exclude: %w", err)
	}
	for _, exclusion := range exclusions {
		binding, exists := bootstrap.bindings[exclusion]
		if !exists || !binding.isDirectNamed(exclusion, "@libs/contracts/constants") {
			return nil, fmt.Errorf("global-prefix exclusion %s must be a direct named import from @libs/contracts/constants", exclusion)
		}
	}
	return exclusions, nil
}

func verifyExcludedControllerRoutes(
	ctx context.Context,
	reader gitObjectReader,
	resolver expressionResolver,
	controllers []ExcludedController,
) error {
	for _, excluded := range controllers {
		raw, err := reader.readBlob(ctx, excluded.Source)
		if err != nil {
			return err
		}
		controllerClass, err := parseNestController(excluded.Source, string(raw))
		if err != nil {
			return err
		}
		controllerSymbol := strings.TrimSpace(controllerClass.decoratorArgument)
		if !symbolPattern.MatchString(controllerSymbol) {
			return fmt.Errorf("excluded controller %s @Controller argument %q is unsupported", excluded.Source, controllerSymbol)
		}
		controllerPart, err := resolver.resolve(controllerSymbol)
		if err != nil {
			return fmt.Errorf("resolve excluded controller %s: %w", excluded.Source, err)
		}
		if err := validatePathSegment("excluded controller", controllerPart); err != nil {
			return fmt.Errorf("%s: %w", excluded.Source, err)
		}

		decorators := httpDecoratorPattern.FindAllStringSubmatchIndex(controllerClass.code, -1)
		actualPaths := make([]string, 0, len(decorators))
		for _, match := range decorators {
			if match[0] <= controllerClass.bodyStart || match[1] >= controllerClass.bodyEnd {
				return fmt.Errorf("excluded controller %s has an HTTP route decorator outside exported class %s", excluded.Source, controllerClass.name)
			}
			decoratorName := controllerClass.code[match[2]:match[3]]
			if err := requireDirectNestDecoratorImport(excluded.Source, controllerClass.imports, decoratorName); err != nil {
				return err
			}
			decoratorExpression := strings.TrimSpace(controllerClass.code[match[4]:match[5]])
			routePart, err := resolver.evaluate(decoratorExpression, nil)
			if err != nil {
				return fmt.Errorf("resolve excluded controller %s decorator %s: %w", excluded.Source, decoratorExpression, err)
			}
			if !strings.HasPrefix(routePart, "/") {
				return fmt.Errorf("excluded controller %s has invalid route %q", excluded.Source, routePart)
			}
			relativeRoute := strings.TrimPrefix(routePart, "/")
			if err := validateRelativeRoute(relativeRoute); err != nil {
				return fmt.Errorf("excluded controller %s has invalid route %q: %w", excluded.Source, routePart, err)
			}
			actualPaths = append(actualPaths, "/"+controllerPart+"/"+relativeRoute)
		}
		expectedPaths := make([]string, 0, len(excluded.RequiredPrefixExclusions))
		for _, symbol := range excluded.RequiredPrefixExclusions {
			value, err := resolver.resolve(symbol)
			if err != nil {
				return fmt.Errorf("resolve global-prefix exclusion %s for %s: %w", symbol, excluded.Source, err)
			}
			if err := validateAbsoluteRoute(value); err != nil {
				return fmt.Errorf("global-prefix exclusion %s resolves to invalid route %q: %w", symbol, value, err)
			}
			expectedPaths = append(expectedPaths, value)
		}
		if difference := compareStrings(actualPaths, expectedPaths); difference != "" {
			return fmt.Errorf("excluded controller %s routes differ from global-prefix exclusions: %s", excluded.Source, difference)
		}
	}
	return nil
}

func mergeExpressions(destination, source map[string]string, sourcePath string) error {
	for name, expression := range source {
		if _, exists := destination[name]; exists {
			return fmt.Errorf("%s redefines exported expression %s", sourcePath, name)
		}
		destination[name] = expression
	}
	return nil
}

func validatePathSegment(kind, value string) error {
	if value == "" || strings.ContainsAny(value, "/?#") || value == "." || value == ".." {
		return fmt.Errorf("%s path segment %q is invalid", kind, value)
	}
	return nil
}

func validateRelativeRoute(value string) error {
	if value == "" || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") ||
		strings.ContainsAny(value, "?#") {
		return fmt.Errorf("relative route %q is invalid", value)
	}
	for _, segment := range strings.Split(value, "/") {
		if err := validatePathSegment("route", segment); err != nil {
			return err
		}
	}
	return nil
}

func validateAbsoluteRoute(value string) error {
	if !strings.HasPrefix(value, "/") || value == "/" || strings.HasSuffix(value, "/") || strings.Contains(value, "//") {
		return fmt.Errorf("absolute route %q is invalid", value)
	}
	return validateRelativeRoute(strings.TrimPrefix(value, "/"))
}
