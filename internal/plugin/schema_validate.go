package plugin

import (
	"fmt"
	"math"
	"net"
	"net/url"
	"strings"
)

// Validation aligned with @remnawave/node-plugins@0.8.2 (NodePluginSchema).

func isPlainIP(value string) bool {
	if strings.Contains(value, ":") && strings.Contains(value, ".") {
		return false
	}
	return net.ParseIP(value) != nil
}

func isIPv4CIDR(value string) bool {
	if strings.Contains(value, ":") || !strings.Contains(value, ".") {
		return false
	}
	_, _, err := net.ParseCIDR(value)
	return err == nil
}

func isIPv6CIDR(value string) bool {
	if !strings.Contains(value, ":") || strings.Contains(value, ".") {
		return false
	}
	_, _, err := net.ParseCIDR(value)
	return err == nil
}

func isIPCidrOrExt(value string) bool {
	if strings.HasPrefix(value, "ext:") {
		return true
	}
	return isPlainIP(value) || isIPv4CIDR(value) || isIPv6CIDR(value)
}

func isIPOrExt(value string) bool {
	if strings.HasPrefix(value, "ext:") {
		return true
	}
	return isPlainIP(value)
}

func isSharedListItem(value string) bool {
	return isPlainIP(value) || isIPv4CIDR(value) || isIPv6CIDR(value)
}

func validateStringArrayLimit(field string, raw any, itemCheck func(string) bool, maximum int) error {
	items, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("%s must be an array", field)
	}
	if err := validateArrayLength(field, len(items), maximum); err != nil {
		return err
	}
	for i, item := range items {
		value, ok := item.(string)
		if !ok {
			return fmt.Errorf("%s[%d] must be a string", field, i)
		}
		if err := validateStringLength(fmt.Sprintf("%s[%d]", field, i), value); err != nil {
			return err
		}
		if !itemCheck(value) {
			return fmt.Errorf("%s[%d] has invalid value %s", field, i, quotedForError(value))
		}
	}
	return nil
}

func validateASNArray(field string, raw any) error {
	items, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("%s must be an array", field)
	}
	if err := validateArrayLength(field, len(items), maxASNItems); err != nil {
		return err
	}
	for i, item := range items {
		asn, ok := numberValue(item)
		if !ok || math.Trunc(asn) != asn || asn < 1 || asn > 4294967295 {
			return fmt.Errorf("%s[%d] must be a positive AS number", field, i)
		}
	}
	return nil
}

func validateSharedLists(raw any) error {
	lists, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("sharedLists must be an array")
	}
	if err := validateArrayLength("sharedLists", len(lists), maxSharedLists); err != nil {
		return err
	}
	totalItems := 0
	for i, entry := range lists {
		obj, ok := entry.(map[string]any)
		if !ok {
			return fmt.Errorf("sharedLists[%d] must be an object", i)
		}
		name, _ := obj["name"].(string)
		if err := validateStringLength(fmt.Sprintf("sharedLists[%d].name", i), name); err != nil {
			return err
		}
		if !strings.HasPrefix(name, "ext:") {
			return fmt.Errorf("sharedLists[%d].name must start with ext:", i)
		}
		items, ok := obj["items"].([]any)
		if !ok {
			return fmt.Errorf("sharedLists[%d].items must be an array", i)
		}
		if err := validateArrayLength(fmt.Sprintf("sharedLists[%d].items", i), len(items), maxSharedListItems); err != nil {
			return err
		}
		totalItems += len(items)
		if totalItems > maxTotalSharedListItems {
			return fmt.Errorf("sharedLists contain %d total items; maximum is %d", totalItems, maxTotalSharedListItems)
		}
		switch listType, _ := obj["type"].(string); listType {
		case "ipList":
			if err := validateStringArrayLimit(fmt.Sprintf("sharedLists[%d].items", i), items, isSharedListItem, maxSharedListItems); err != nil {
				return err
			}
		case "asList":
			if err := validateASNArray(fmt.Sprintf("sharedLists[%d].items", i), items); err != nil {
				return err
			}
		default:
			return fmt.Errorf("sharedLists[%d].type must be ipList or asList", i)
		}
	}
	return nil
}

func validateTorrentBlockerSection(raw any) error {
	section, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("torrentBlocker must be an object")
	}
	if _, ok := section["enabled"].(bool); !ok {
		return fmt.Errorf("torrentBlocker.enabled is required and must be a boolean")
	}
	duration, durationOK := numberValue(section["blockDuration"])
	if !durationOK || math.Trunc(duration) != duration || duration < 0 || duration > maxJavaScriptSafeInteger {
		return fmt.Errorf("torrentBlocker.blockDuration must be a non-negative safe integer")
	}
	ignoreRaw, ok := section["ignoreLists"]
	if !ok {
		return fmt.Errorf("torrentBlocker.ignoreLists is required")
	}
	ignore, ok := ignoreRaw.(map[string]any)
	if !ok {
		return fmt.Errorf("torrentBlocker.ignoreLists must be an object")
	}
	if ips, ok := ignore["ip"]; ok {
		if err := validateStringArrayLimit("torrentBlocker.ignoreLists.ip", ips, isIPOrExt, maxIgnoreItems); err != nil {
			return err
		}
	}
	if users, ok := ignore["userId"]; ok {
		if err := validateIntArrayLimit("torrentBlocker.ignoreLists.userId", users, maxIgnoreItems); err != nil {
			return err
		}
	}
	if tags, ok := section["includeRuleTags"]; ok {
		items, ok := tags.([]any)
		if !ok {
			return fmt.Errorf("torrentBlocker.includeRuleTags must be an array")
		}
		if len(items) < 1 {
			return fmt.Errorf("torrentBlocker.includeRuleTags must contain at least one item")
		}
		if err := validateArrayLength("torrentBlocker.includeRuleTags", len(items), maxRuleTags); err != nil {
			return err
		}
		for i, item := range items {
			value, ok := item.(string)
			if !ok {
				return fmt.Errorf("torrentBlocker.includeRuleTags[%d] must be a string", i)
			}
			if err := validateStringLength(fmt.Sprintf("torrentBlocker.includeRuleTags[%d]", i), value); err != nil {
				return err
			}
		}
	}
	if rawURL, ok := section["webhookUrl"]; ok {
		value, ok := rawURL.(string)
		if !ok {
			return fmt.Errorf("torrentBlocker.webhookUrl must be a URL")
		}
		if err := validateStringLength("torrentBlocker.webhookUrl", value); err != nil {
			return err
		}
		parsed, err := url.Parse(strings.TrimSpace(value))
		if err != nil || parsed.Scheme == "" {
			return fmt.Errorf("torrentBlocker.webhookUrl must be a URL")
		}
	}
	if rawPosition, ok := section["rulePlacement"]; ok {
		position, valid := numberValue(rawPosition)
		if !valid || math.IsNaN(position) || math.IsInf(position, 0) || position < 0 || position > 1000 {
			return fmt.Errorf("torrentBlocker.rulePlacement must be a number between 0 and 1000")
		}
	}
	return nil
}

func validateConnectionDropSection(raw any) error {
	section, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("connectionDrop must be an object")
	}
	if _, ok := section["enabled"].(bool); !ok {
		return fmt.Errorf("connectionDrop.enabled is required and must be a boolean")
	}
	if err := validateStringArrayLimit("connectionDrop.whitelistIps", section["whitelistIps"], isIPOrExt, maxFilterItems); err != nil {
		return err
	}
	return nil
}

func validateIngressFilterSection(raw any) error {
	section, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("ingressFilter must be an object")
	}
	if _, ok := section["enabled"].(bool); !ok {
		return fmt.Errorf("ingressFilter.enabled is required and must be a boolean")
	}
	if err := validateStringArrayLimit("ingressFilter.blockedIps", section["blockedIps"], isIPCidrOrExt, maxFilterItems); err != nil {
		return err
	}
	return nil
}

func validateEgressFilterSection(raw any) error {
	section, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("egressFilter must be an object")
	}
	if _, ok := section["enabled"].(bool); !ok {
		return fmt.Errorf("egressFilter.enabled is required and must be a boolean")
	}
	if ips, ok := section["blockedIps"]; ok {
		if err := validateStringArrayLimit("egressFilter.blockedIps", ips, isIPCidrOrExt, maxFilterItems); err != nil {
			return err
		}
	}
	if ports, ok := section["blockedPorts"]; ok {
		if err := validatePortArray("egressFilter.blockedPorts", ports); err != nil {
			return err
		}
	}
	return nil
}

func validatePreStartSection(raw any) error {
	section, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("preStart must be an object")
	}
	if enabled, present := section["enabled"]; present {
		if _, ok := enabled.(bool); !ok {
			return fmt.Errorf("preStart.enabled must be a boolean")
		}
	}

	cleanupRaw, present := section["cleanupSockets"]
	if !present {
		return nil
	}
	cleanup, ok := cleanupRaw.(map[string]any)
	if !ok {
		return fmt.Errorf("preStart.cleanupSockets must be an object")
	}
	if _, ok := cleanup["enabled"].(bool); !ok {
		return fmt.Errorf("preStart.cleanupSockets.enabled is required and must be a boolean")
	}
	files, ok := cleanup["files"].([]any)
	if !ok {
		return fmt.Errorf("preStart.cleanupSockets.files is required and must be an array")
	}
	if err := validateArrayLength("preStart.cleanupSockets.files", len(files), maxPreStartSocketFiles); err != nil {
		return err
	}
	for index, item := range files {
		path, ok := item.(string)
		if !ok {
			return fmt.Errorf("preStart.cleanupSockets.files[%d] must be a string", index)
		}
		path = strings.TrimSpace(path)
		field := fmt.Sprintf("preStart.cleanupSockets.files[%d]", index)
		if path == "" {
			return fmt.Errorf("%s must not be empty", field)
		}
		if !strings.HasPrefix(path, "/") {
			return fmt.Errorf("%s must be an absolute path", field)
		}
		if strings.ContainsRune(path, '\x00') {
			return fmt.Errorf("%s must not contain null bytes", field)
		}
	}
	return nil
}

func isIntNumber(value any) bool {
	number, ok := numberValue(value)
	return ok && math.Trunc(number) == number && math.Abs(number) <= maxJavaScriptSafeInteger
}

func numberValue(value any) (float64, bool) {
	var number float64
	switch v := value.(type) {
	case float64:
		number = v
	case int:
		number = float64(v)
	case int64:
		number = float64(v)
	default:
		return 0, false
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false
	}
	return number, true
}

func validateIntArrayLimit(field string, raw any, maximum int) error {
	items, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("%s must be an array", field)
	}
	if err := validateArrayLength(field, len(items), maximum); err != nil {
		return err
	}
	for i, item := range items {
		if !isIntNumber(item) {
			return fmt.Errorf("%s[%d] must be an integer", field, i)
		}
	}
	return nil
}

func validatePortArray(field string, raw any) error {
	items, ok := raw.([]any)
	if !ok {
		return fmt.Errorf("%s must be an array", field)
	}
	if err := validateArrayLength(field, len(items), maxFilterItems); err != nil {
		return err
	}
	for i, item := range items {
		port, ok := asInt(item)
		if !ok || port < 1 || port > 65535 {
			return fmt.Errorf("%s[%d] must be an integer between 1 and 65535", field, i)
		}
	}
	return nil
}

func asInt(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		if v != float64(int(v)) {
			return 0, false
		}
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	default:
		return 0, false
	}
}
