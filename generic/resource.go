/*
resource.go - Resource type registration and lookup

PURPOSE:
  Provides a registry for domain packages to register their resource types.
  This enables deserialization from storage/JSON back to concrete types
  while maintaining proper encapsulation.

HOW IT WORKS:
  1. Domain packages define their ResourceType implementations
  2. Domain packages register them on init() or explicit registration
  3. Factory/storage uses the registry to reconstruct types

USAGE:
  // In timeoff/init.go
  func init() {
      generic.RegisterResource(ResourcePTO)
      generic.RegisterResource(ResourceSick)
  }

  // In factory
  resourceType := generic.LookupResource("pto")  // returns timeoff.ResourcePTO

WHY A REGISTRY:
  - Generic package stays domain-agnostic
  - Type safety maintained at compile time
  - Clean deserialization from strings
  - Domains own their types

SEE ALSO:
  - types.go: ResourceType interface definition
  - timeoff/types.go: TimeOff resource implementation
  - rewards/types.go: Rewards resource implementation
*/
package generic

import (
	"fmt"
	"sync"
)

// =============================================================================
// RESOURCE REGISTRY
// =============================================================================

var (
	resourceRegistry = make(map[string]ResourceType)
	registryMu       sync.RWMutex
)

// RegisterResource adds a resource type to the global registry.
// Call this from domain package init() functions.
func RegisterResource(r ResourceType) {
	registryMu.Lock()
	defer registryMu.Unlock()
	resourceRegistry[r.ResourceID()] = r
}

// LookupResource finds a registered resource type by ID.
// Returns nil if not found.
func LookupResource(id string) ResourceType {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return resourceRegistry[id]
}

// MustLookupResource finds a registered resource type or panics.
// Use in tests or when you're certain the resource exists.
func MustLookupResource(id string) ResourceType {
	r := LookupResource(id)
	if r == nil {
		panic(fmt.Sprintf("resource type not registered: %s", id))
	}
	return r
}

// ListResources returns all registered resource types.
func ListResources() []ResourceType {
	registryMu.RLock()
	defer registryMu.RUnlock()
	result := make([]ResourceType, 0, len(resourceRegistry))
	for _, r := range resourceRegistry {
		result = append(result, r)
	}
	return result
}

// ListResourcesByDomain returns resources for a specific domain.
func ListResourcesByDomain(domain string) []ResourceType {
	registryMu.RLock()
	defer registryMu.RUnlock()
	var result []ResourceType
	for _, r := range resourceRegistry {
		if r.ResourceDomain() == domain {
			result = append(result, r)
		}
	}
	return result
}

// =============================================================================
// STRING RESOURCE - For testing and fallback
// =============================================================================

// StringResource is a simple string-based resource type.
// Use only for testing or as a fallback when domain types aren't available.
type StringResource struct {
	ID     string
	Domain string
}

func (r StringResource) ResourceID() string     { return r.ID }
func (r StringResource) ResourceDomain() string { return r.Domain }

// NewStringResource creates a StringResource with "unknown" domain.
// This is a fallback for when we have an ID but no registered type.
func NewStringResource(id string) StringResource {
	return StringResource{ID: id, Domain: "unknown"}
}

// GetOrCreateResource looks up a resource type, or creates a StringResource fallback.
// Use this in deserialization when the domain might not be loaded.
func GetOrCreateResource(id string) ResourceType {
	if r := LookupResource(id); r != nil {
		return r
	}
	return NewStringResource(id)
}
