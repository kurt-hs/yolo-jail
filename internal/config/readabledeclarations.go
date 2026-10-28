package config

import (
	"sort"

	"github.com/mschulkind-oss/yolo-jail/internal/loopholedecl"
)

// readableBrokerDeclaration projects only explicitly declared canonical metadata. Pending-only
// grant kinds are added to the detached broker value because ResolveReadableAllowance validates
// pending capabilities against its mapped-grant set; the source declaration remains untouched.
func readableBrokerDeclaration(pack, label string, brokered loopholedecl.Brokered, settings []loopholedecl.Setting) ReadableBrokerDeclaration {
	declaration := ReadableBrokerDeclaration{
		Pack:                pack,
		Label:               label,
		Brokered:            brokered,
		ReadCapability:      brokered.ReadCapability,
		KindCapabilities:    cloneStringMap(brokered.KindCapabilities),
		PendingCapabilities: append([]string(nil), brokered.PendingCapabilities...),
		Settings:            append([]loopholedecl.Setting(nil), settings...),
	}
	declaration.Brokered.CredentialPaths = append([]string(nil), brokered.CredentialPaths...)
	declaration.Brokered.Grants = append([]string(nil), brokered.Grants...)
	declaration.Brokered.PendingCapabilities = append([]string(nil), brokered.PendingCapabilities...)
	declaration.Brokered.KindCapabilities = cloneStringMap(brokered.KindCapabilities)
	pending := make(map[string]bool, len(brokered.PendingCapabilities))
	for _, capability := range brokered.PendingCapabilities {
		pending[capability] = true
	}
	var pendingOnlyKinds []string
	for kind, capability := range brokered.KindCapabilities {
		if pending[capability] && !containsString(declaration.Brokered.Grants, kind) {
			pendingOnlyKinds = append(pendingOnlyKinds, kind)
		}
	}
	sort.Strings(pendingOnlyKinds)
	declaration.Brokered.Grants = append(declaration.Brokered.Grants, pendingOnlyKinds...)
	return declaration
}
