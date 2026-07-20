package contract

import (
	"context"

	"github.com/luxiaba/remnanode-lite/internal/contract/sourceoracle"
)

const OfficialSourceManifestPath = "internal/contract/official-source-manifest.json"

// GenerateOfficialSourceManifest reconstructs the source evidence manifest
// from the immutable Git tree at OfficialNodeCommit.
func GenerateOfficialSourceManifest(ctx context.Context, gitRepository string) ([]byte, error) {
	return sourceoracle.Generate(ctx, gitRepository, officialSourceExpectation())
}

// ValidateOfficialSourceManifest compares the checked-in machine extraction to
// the hand-distilled executable contract without requiring an official clone.
func ValidateOfficialSourceManifest(raw []byte) error {
	return sourceoracle.Validate(raw, officialSourceExpectation())
}

// VerifyOfficialSourceManifest regenerates the evidence from official Git
// objects and requires an exact match with the checked-in manifest.
func VerifyOfficialSourceManifest(ctx context.Context, gitRepository string, raw []byte) error {
	return sourceoracle.VerifySource(ctx, gitRepository, raw, officialSourceExpectation())
}

func officialSourceExpectation() sourceoracle.Expectation {
	routes := OfficialRoutes()
	expectedRoutes := make([]sourceoracle.ExpectedRoute, 0, len(routes))
	for _, route := range routes {
		expectedRoutes = append(expectedRoutes, sourceoracle.ExpectedRoute{
			Method:           route.Method,
			Path:             route.Path,
			ControllerSource: route.ControllerSource,
		})
	}
	return sourceoracle.Expectation{
		Repository:  OfficialNodeRepository,
		PackageName: "@remnawave/node",
		Version:     OfficialNodeVersion,
		Commit:      OfficialNodeCommit,
		Files:       OfficialSourceFiles(),
		Routes:      expectedRoutes,
		ExcludedControllers: []sourceoracle.ExcludedController{
			{
				Source: "src/modules/internal/internal.controller.ts",
				RequiredPrefixExclusions: []string{
					"XRAY_INTERNAL_FULL_PATH",
					"XRAY_INTERNAL_FULL_WEBHOOK_PATH",
				},
			},
		},
	}
}
