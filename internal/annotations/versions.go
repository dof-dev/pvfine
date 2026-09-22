package annotations

import "strings"

// ValidPVFVersion reports whether a value is a supported configuration version.
func ValidPVFVersion(version string) bool {
	return version == "90US" || version == "90CN" || version == "110US"
}

func supportsPVFVersion(versions []string, version string) bool {
	if len(versions) == 0 {
		return true
	}
	for _, candidate := range versions {
		if candidate == version {
			return true
		}
	}
	return false
}

// ForVersion builds an immutable runtime view while retaining the full source
// document for serialization and subsequent archive switches. Unknown versions
// only enable unrestricted definitions. Filtering precedes implicit field rules.
func (e *Engine) ForVersion(version string) *Engine {
	if e == nil {
		return nil
	}
	if !ValidPVFVersion(version) {
		version = ""
	}
	if e.scoped && e.version == version {
		return e
	}
	source := e.Document()
	document := source
	document.Fields = make([]FieldDefinition, 0, len(source.Fields))
	activeFields := make(map[string]bool, len(source.Fields))
	for _, field := range source.Fields {
		if supportsPVFVersion(field.PVFVersions, version) {
			document.Fields = append(document.Fields, field)
			activeFields[field.ID] = true
		}
	}
	document.Rules = make([]Rule, 0, len(source.Rules))
	for _, rule := range source.Rules {
		if !supportsPVFVersion(rule.PVFVersions, version) {
			continue
		}
		if id := strings.TrimSpace(rule.Field); id != "" && !activeFields[id] {
			continue
		}
		document.Rules = append(document.Rules, rule)
	}
	result := compileValidated(document)
	result.source, result.version, result.scoped = &source, version, true
	return result
}
