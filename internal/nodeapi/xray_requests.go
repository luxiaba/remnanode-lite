package nodeapi

import (
	"bytes"
	"encoding/json"
)

type XrayStartRequest struct {
	Internals  *XrayStartInternals `json:"internals"`
	XrayConfig *map[string]any     `json:"xrayConfig"`
}

type XrayStartInternals struct {
	Metadata     OptionalXrayNodeMetadata `json:"metadata"`
	Integrations OptionalXrayIntegrations `json:"integrations"`
	ForceRestart OptionalBool             `json:"forceRestart"`
	Hashes       *XrayConfigHash          `json:"hashes"`
}

type XrayNodeMetadata struct {
	Name        *string   `json:"name"`
	UUID        *string   `json:"uuid"`
	ID          *float64  `json:"id"`
	Tags        *[]string `json:"tags"`
	CountryCode *string   `json:"countryCode"`
}

type OptionalXrayNodeMetadata struct {
	Value   XrayNodeMetadata
	Present bool
	Null    bool
}

func (o *OptionalXrayNodeMetadata) UnmarshalJSON(raw []byte) error {
	o.Present = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		o.Null = true
		return nil
	}
	return json.Unmarshal(raw, &o.Value)
}

func (*OptionalXrayNodeMetadata) structuralJSONSchema() any {
	return XrayNodeMetadata{}
}

type OptionalXrayIntegrations struct {
	Value   map[string]any
	Present bool
	Null    bool
}

func (o *OptionalXrayIntegrations) UnmarshalJSON(raw []byte) error {
	o.Present = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		o.Null = true
		return nil
	}
	return json.Unmarshal(raw, &o.Value)
}

type OptionalBool struct {
	Value   bool
	Present bool
	Null    bool
}

func (o *OptionalBool) UnmarshalJSON(raw []byte) error {
	o.Present = true
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		o.Null = true
		return nil
	}
	return json.Unmarshal(raw, &o.Value)
}

type XrayConfigHash struct {
	EmptyConfig *string            `json:"emptyConfig"`
	Inbounds    *[]XrayInboundHash `json:"inbounds"`
}

type XrayInboundHash struct {
	UsersCount *float64 `json:"usersCount"`
	Hash       *string  `json:"hash"`
	Tag        *string  `json:"tag"`
}

func (r *XrayStartRequest) Validate() []Issue {
	issues := make([]Issue, 0)
	if r.Internals == nil {
		issues = appendValidationIssues(issues, MissingIssue([]any{"internals"}, "object"))
	} else {
		if r.Internals.Metadata.Present {
			if r.Internals.Metadata.Null {
				issues = appendValidationIssues(issues, InvalidTypeIssue([]any{"internals", "metadata"}, "object", "null"))
			} else {
				metadata := r.Internals.Metadata.Value
				issues = appendValidationIssues(issues, requireString(metadata.Name, []any{"internals", "metadata", "name"})...)
				issues = appendValidationIssues(issues, requireString(metadata.UUID, []any{"internals", "metadata", "uuid"})...)
				if metadata.ID == nil {
					issues = appendValidationIssues(issues, MissingIssue([]any{"internals", "metadata", "id"}, "number"))
				}
				if metadata.Tags == nil {
					issues = appendValidationIssues(issues, MissingIssue([]any{"internals", "metadata", "tags"}, "array"))
				}
				issues = appendValidationIssues(issues, requireString(metadata.CountryCode, []any{"internals", "metadata", "countryCode"})...)
			}
		}
		if r.Internals.Integrations.Present && r.Internals.Integrations.Null {
			issues = appendValidationIssues(issues, InvalidTypeIssue([]any{"internals", "integrations"}, "object", "null"))
		}
		if r.Internals.ForceRestart.Present && r.Internals.ForceRestart.Null {
			issues = appendValidationIssues(issues, InvalidTypeIssue([]any{"internals", "forceRestart"}, "boolean", "null"))
		}
		if r.Internals.Hashes == nil {
			issues = appendValidationIssues(issues, MissingIssue([]any{"internals", "hashes"}, "object"))
		} else {
			issues = appendValidationIssues(issues, requireString(
				r.Internals.Hashes.EmptyConfig,
				[]any{"internals", "hashes", "emptyConfig"},
			)...)
			if r.Internals.Hashes.Inbounds == nil {
				issues = appendValidationIssues(issues, MissingIssue([]any{"internals", "hashes", "inbounds"}, "array"))
			} else {
				for index, inbound := range *r.Internals.Hashes.Inbounds {
					path := []any{"internals", "hashes", "inbounds", index}
					if inbound.UsersCount == nil {
						issues = appendValidationIssues(issues, MissingIssue(appendPath(path, "usersCount"), "number"))
					}
					issues = appendValidationIssues(issues, requireString(inbound.Hash, appendPath(path, "hash"))...)
					issues = appendValidationIssues(issues, requireString(inbound.Tag, appendPath(path, "tag"))...)
					if validationIssueLimitReached(issues) {
						return issues
					}
				}
			}
		}
	}
	if r.XrayConfig == nil {
		issues = appendValidationIssues(issues, MissingIssue([]any{"xrayConfig"}, "object"))
	}
	return issues
}
