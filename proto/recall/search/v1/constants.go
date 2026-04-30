package searchv1

const (
	// SearchProviderService is the fully qualified protobuf service implemented
	// by every recall-compatible search provider.
	SearchProviderService = "recall.search.v1.SearchProvider"

	// SearchProviderSearchMethod is the unary search method name.
	SearchProviderSearchMethod = "Search"

	// SearchProviderSearchPath is the stdio and gRPC full method path for Search.
	SearchProviderSearchPath = "/" + SearchProviderService + "/" + SearchProviderSearchMethod

	// SearchProviderListCapabilitiesMethod is the unary capability listing method name.
	SearchProviderListCapabilitiesMethod = "ListCapabilities"

	// SearchProviderListCapabilitiesPath is the stdio and gRPC full method path for ListCapabilities.
	SearchProviderListCapabilitiesPath = "/" + SearchProviderService + "/" + SearchProviderListCapabilitiesMethod
)
