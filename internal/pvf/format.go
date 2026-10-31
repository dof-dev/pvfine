package pvf

import "errors"

// ContentRules describes client content conventions independently of the
// physical container. Future containers can reuse or combine these rules.
type ContentRules struct {
	UTF16Only                bool `json:"utf16Only"`
	RequiresIndexHash        bool `json:"requiresIndexHash"`
	SupportsStringReferences bool `json:"supportsStringReferences"`
}

type containerKind uint8

const (
	plainContainer containerKind = iota
	pagedContainer
)

type formatProfile struct {
	id        string
	container containerKind
	rules     ContentRules
}

const (
	FormatStandard   = "standard"
	FormatAlternate  = "alternate"
	FormatPaged      = "paged"
	FormatPaged110   = "paged110"
	FormatPaged115   = "paged115"
	FormatPaged115CN = "paged115cn"
	FormatRecovered  = "recovered"
	FormatPaged100CN = "paged100cn"
	FormatTW         = "tw"
	FormatACT60      = "act60"
)

var standardProfile = formatProfile{id: FormatStandard}
var alternateProfile = formatProfile{id: FormatAlternate}
var recoveredProfile = formatProfile{id: FormatRecovered}
var pagedContentRules = ContentRules{UTF16Only: true, RequiresIndexHash: true, SupportsStringReferences: true}
var pagedProfile = formatProfile{
	id:        FormatPaged,
	container: pagedContainer,
	rules:     pagedContentRules,
}
var paged110Profile = formatProfile{
	id:        FormatPaged110,
	container: pagedContainer,
	rules:     pagedContentRules,
}
var paged115Profile = formatProfile{
	id:        FormatPaged115,
	container: pagedContainer,
	rules:     pagedContentRules,
}
var paged115CNProfile = formatProfile{
	id:        FormatPaged115CN,
	container: pagedContainer,
	rules:     pagedContentRules,
}

// 100CN shares page guards and the newer list/reference conventions, but
// retains an ASCII pool. Migrating that pool would rewrite an untouched archive.
var paged100CNProfile = formatProfile{
	id:        FormatPaged100CN,
	container: pagedContainer,
	rules:     ContentRules{RequiresIndexHash: true, SupportsStringReferences: true},
}

var act60Profile = formatProfile{id: FormatACT60, rules: ContentRules{SupportsStringReferences: true}}

var twProfile = formatProfile{id: FormatTW, rules: ContentRules{SupportsStringReferences: true}}

// WriteCapabilities is derived from both the format and recovered archive
// state. Structural edits include appending or remapping string-pool entries.
type WriteCapabilities struct {
	CanSave            bool   `json:"canSave"`
	CanChangeStructure bool   `json:"canChangeStructure"`
	Reason             string `json:"reason"`
}

var ErrStructureLocked = errors.New("pvf: archive structure cannot be changed until the HASH key is known")

// Format returns a stable identifier for diagnostics, not feature gating.
func (a *Archive) Format() string { return a.format.id }

func (a *Archive) ContentRules() ContentRules { return a.format.rules }

func (a *Archive) canRebuildHash() bool {
	return a.keys.hashKnown || a.data == nil || a.hashSize == 0
}

func (a *Archive) WriteCapabilities() WriteCapabilities {
	if a.IsPaged() && len(a.pageKeys) == 0 {
		return WriteCapabilities{Reason: ErrPagedReadOnly.Error()}
	}
	if !a.canRebuildHash() {
		return WriteCapabilities{CanSave: true, Reason: ErrStructureLocked.Error()}
	}
	return WriteCapabilities{CanSave: true, CanChangeStructure: true}
}

func (a *Archive) validateWrite() error {
	capabilities := a.WriteCapabilities()
	if !capabilities.CanSave {
		return ErrPagedReadOnly
	}
	if (a.structuralDirty || a.poolsDirty) && !capabilities.CanChangeStructure {
		return a.structureLockedError()
	}
	return nil
}

func (a *Archive) structureLockedError() error {
	if a.IsPaged() {
		return ErrPagedStructureLocked
	}
	return ErrStructureLocked
}

// ClientVersion identifies the known client family for configuration applicability.
// It must not be used to determine parsing or writing capabilities.
func (a *Archive) ClientVersion() string {
	if a == nil {
		return ""
	}
	switch a.Format() {
	case FormatStandard:
		return "90US"
	case FormatAlternate:
		return "90CN"
	case FormatPaged110:
		return "110US"
	case FormatPaged115:
		return "115US"
	case FormatPaged115CN:
		return "115CN"
	case FormatPaged100CN:
		return "100CN"
	case FormatTW:
		return "TW"
	case FormatACT60:
		return "60ACT"
	default:
		return ""
	}
}
