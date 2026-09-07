package lease

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation"
)

// maxEncodedKeyLength bounds len(namespace) + len(".") + len(name) so the
// key's backing-store identity is always a valid DNS-1123 subdomain — the
// k8s backend encodes the pair as a single Lease object name.
const maxEncodedKeyLength = validation.DNS1123SubdomainMaxLength

// MaxHolderBytes is the maximum decoded holder size for acquisition and renewal.
// This conservative byte limit fits every supported backend's holder column.
const MaxHolderBytes = 253

// ValidateHolder validates a live holder without changing its identity. Release
// deliberately accepts older oversized holders so they can be tombstoned.
func ValidateHolder(holder string) error {
	if holder == "" {
		return fmt.Errorf("holder is required")
	}
	if len(holder) > MaxHolderBytes {
		return fmt.Errorf("holder must be at most %d bytes", MaxHolderBytes)
	}
	return nil
}

// ValidateKey reports whether key is well-formed. The namespace must be an
// RFC 1123 DNS label (lowercase alphanumerics and '-', at most 63
// characters — notably, no dots), the name an RFC 1123 DNS subdomain, and
// the dot-joined pair at most 253 characters.
//
// The namespace charset is a safety property, not a style choice: backends
// that encode the pair into one identifier use '.' as the separator, and a
// dot-free namespace makes that encoding injective — two distinct keys can
// never collide into one stored object, so no tenant can address another
// tenant's lease by crafting an ambiguous key. It also matches how
// Kubernetes names namespaces, so keys mirrored from cluster resources are
// always valid.
func ValidateKey(key Key) error {
	if errs := validation.IsDNS1123Label(key.Namespace); len(errs) > 0 {
		return fmt.Errorf("invalid namespace %q: must be an RFC 1123 DNS label (lowercase alphanumerics and '-', at most %d characters): %s",
			key.Namespace, validation.DNS1123LabelMaxLength, errs[0])
	}
	if errs := validation.IsDNS1123Subdomain(key.Name); len(errs) > 0 {
		return fmt.Errorf("invalid name %q: must be an RFC 1123 DNS subdomain (lowercase alphanumerics, '-' and '.', at most %d characters): %s",
			key.Name, validation.DNS1123SubdomainMaxLength, errs[0])
	}
	if n := len(key.Namespace) + 1 + len(key.Name); n > maxEncodedKeyLength {
		return fmt.Errorf("invalid key %s/%s: namespace and name joined with '.' must be at most %d characters, got %d",
			key.Namespace, key.Name, maxEncodedKeyLength, n)
	}
	return nil
}
