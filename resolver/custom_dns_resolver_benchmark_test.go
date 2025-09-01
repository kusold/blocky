package resolver

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/0xERR0R/blocky/config"
	. "github.com/0xERR0R/blocky/model"
	"github.com/miekg/dns"
)

// BenchmarkCustomDNSResolver_ClientGroupResolution benchmarks client group resolution performance
func BenchmarkCustomDNSResolver_ClientGroupResolution(b *testing.B) {
	// Create configuration with many client groups
	cfg := createLargeClientGroupsConfig(100) // 100 different client groups
	resolver := NewCustomDNSResolver(cfg)

	// Prepare test requests
	requests := createTestRequests(1000) // 1000 test requests

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := requests[i%len(requests)]
		resolver.resolveClientGroup(req)
	}
}

// BenchmarkCustomDNSResolver_DNSResolution benchmarks full DNS resolution with client groups
func BenchmarkCustomDNSResolver_DNSResolution(b *testing.B) {
	ctx := context.Background()

	// Create configuration with client groups
	cfg := createMediumClientGroupsConfig(20) // 20 client groups
	resolver := NewCustomDNSResolver(cfg)

	// Mock next resolver
	mockNext := &mockResolver{}
	mockNext.On("Resolve", nil).Return(&Response{Res: new(dns.Msg)}, nil)
	resolver.Next(mockNext)

	// Prepare test requests
	requests := createDNSTestRequests(100)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := requests[i%len(requests)]
		_, _ = resolver.Resolve(ctx, req)
	}
}

// BenchmarkCustomDNSResolver_LegacyMode benchmarks legacy mode performance (no client groups)
func BenchmarkCustomDNSResolver_LegacyMode(b *testing.B) {
	ctx := context.Background()

	// Create legacy configuration (no client groups)
	cfg := config.CustomDNS{
		Mapping: config.CustomDNSMapping{
			"test.domain": {&dns.A{A: net.ParseIP("192.168.1.1")}},
			"app.local":   {&dns.A{A: net.ParseIP("192.168.1.2")}},
			"api.local":   {&dns.A{A: net.ParseIP("192.168.1.3")}},
		},
		CustomTTL:           config.Duration(time.Hour),
		FilterUnmappedTypes: true,
	}
	resolver := NewCustomDNSResolver(cfg)

	// Mock next resolver
	mockNext := &mockResolver{}
	mockNext.On("Resolve", nil).Return(&Response{Res: new(dns.Msg)}, nil)
	resolver.Next(mockNext)

	// Prepare test requests
	requests := createDNSTestRequests(100)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := requests[i%len(requests)]
		_, _ = resolver.Resolve(ctx, req)
	}
}

// BenchmarkCustomDNSResolver_CIDRMatching benchmarks CIDR subnet matching performance
func BenchmarkCustomDNSResolver_CIDRMatching(b *testing.B) {
	// Create configuration with many CIDR groups
	cfg := createCIDRClientGroupsConfig(50) // 50 CIDR subnets
	resolver := NewCustomDNSResolver(cfg)

	// Create requests from various IP addresses
	requests := createCIDRTestRequests(200)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := requests[i%len(requests)]
		resolver.resolveClientGroup(req)
	}
}

// BenchmarkCustomDNSResolver_WildcardMatching benchmarks wildcard pattern matching performance
func BenchmarkCustomDNSResolver_WildcardMatching(b *testing.B) {
	// Create configuration with many wildcard patterns
	cfg := createWildcardClientGroupsConfig(30) // 30 wildcard patterns
	resolver := NewCustomDNSResolver(cfg)

	// Create requests with various client names
	requests := createWildcardTestRequests(150)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req := requests[i%len(requests)]
		resolver.resolveClientGroup(req)
	}
}

// Helper function to create large client groups configuration
func createLargeClientGroupsConfig(numGroups int) config.CustomDNS {
	clientGroups := make(map[string]config.CustomDNSGroup)

	// Add default group
	clientGroups["default"] = config.CustomDNSGroup{
		Mapping: config.CustomDNSMapping{
			"default.test": {&dns.A{A: net.ParseIP("192.168.1.1")}},
		},
	}

	// Add IP-based groups
	for i := 0; i < numGroups/3; i++ {
		ip := fmt.Sprintf("192.168.%d.%d", i/255, i%255)
		clientGroups[ip] = config.CustomDNSGroup{
			Mapping: config.CustomDNSMapping{
				fmt.Sprintf("host%d.test", i): {&dns.A{A: net.ParseIP(ip)}},
			},
		}
	}

	// Add CIDR-based groups
	for i := 0; i < numGroups/3; i++ {
		cidr := fmt.Sprintf("10.%d.0.0/24", i)
		clientGroups[cidr] = config.CustomDNSGroup{
			Mapping: config.CustomDNSMapping{
				fmt.Sprintf("subnet%d.test", i): {&dns.A{A: net.ParseIP(fmt.Sprintf("10.%d.0.1", i))}},
			},
		}
	}

	// Add wildcard-based groups
	for i := 0; i < numGroups/3; i++ {
		pattern := fmt.Sprintf("client%d*", i)
		clientGroups[pattern] = config.CustomDNSGroup{
			Mapping: config.CustomDNSMapping{
				fmt.Sprintf("wildcard%d.test", i): {&dns.A{A: net.ParseIP(fmt.Sprintf("172.16.%d.1", i))}},
			},
		}
	}

	return config.CustomDNS{
		ClientGroups:        clientGroups,
		CustomTTL:           config.Duration(time.Hour),
		FilterUnmappedTypes: true,
	}
}

// Helper function to create medium client groups configuration
func createMediumClientGroupsConfig(numGroups int) config.CustomDNS {
	clientGroups := make(map[string]config.CustomDNSGroup)

	// Add default group
	clientGroups["default"] = config.CustomDNSGroup{
		Mapping: config.CustomDNSMapping{
			"default.test": {&dns.A{A: net.ParseIP("192.168.1.1")}},
			"api.test":     {&dns.A{A: net.ParseIP("192.168.1.2")}},
			"web.test":     {&dns.A{A: net.ParseIP("192.168.1.3")}},
		},
	}

	// Add balanced groups
	for i := 0; i < numGroups; i++ {
		if i%3 == 0 {
			// IP-based group
			ip := fmt.Sprintf("192.168.%d.%d", i/255+1, i%255+1)
			clientGroups[ip] = config.CustomDNSGroup{
				Mapping: config.CustomDNSMapping{
					fmt.Sprintf("host%d.test", i): {&dns.A{A: net.ParseIP(ip)}},
				},
			}
		} else if i%3 == 1 {
			// CIDR-based group
			cidr := fmt.Sprintf("10.%d.0.0/24", i)
			clientGroups[cidr] = config.CustomDNSGroup{
				Mapping: config.CustomDNSMapping{
					fmt.Sprintf("subnet%d.test", i): {&dns.A{A: net.ParseIP(fmt.Sprintf("10.%d.0.1", i))}},
				},
			}
		} else {
			// Wildcard-based group
			pattern := fmt.Sprintf("device%d*", i)
			clientGroups[pattern] = config.CustomDNSGroup{
				Mapping: config.CustomDNSMapping{
					fmt.Sprintf("wildcard%d.test", i): {&dns.A{A: net.ParseIP(fmt.Sprintf("172.16.%d.1", i))}},
				},
			}
		}
	}

	return config.CustomDNS{
		ClientGroups:        clientGroups,
		CustomTTL:           config.Duration(time.Hour),
		FilterUnmappedTypes: true,
	}
}

// Helper function to create CIDR-focused configuration
func createCIDRClientGroupsConfig(numCIDRs int) config.CustomDNS {
	clientGroups := make(map[string]config.CustomDNSGroup)

	// Add default group
	clientGroups["default"] = config.CustomDNSGroup{
		Mapping: config.CustomDNSMapping{
			"default.test": {&dns.A{A: net.ParseIP("192.168.1.1")}},
		},
	}

	// Add CIDR groups with varying specificity
	for i := 0; i < numCIDRs; i++ {
		if i < numCIDRs/2 {
			// /24 subnets
			cidr := fmt.Sprintf("10.%d.0.0/24", i)
			clientGroups[cidr] = config.CustomDNSGroup{
				Mapping: config.CustomDNSMapping{
					fmt.Sprintf("subnet%d.test", i): {&dns.A{A: net.ParseIP(fmt.Sprintf("10.%d.0.1", i))}},
				},
			}
		} else {
			// /16 subnets (less specific)
			cidr := fmt.Sprintf("172.%d.0.0/16", i-numCIDRs/2)
			clientGroups[cidr] = config.CustomDNSGroup{
				Mapping: config.CustomDNSMapping{
					fmt.Sprintf("wide%d.test", i): {&dns.A{A: net.ParseIP(fmt.Sprintf("172.%d.0.1", i-numCIDRs/2))}},
				},
			}
		}
	}

	return config.CustomDNS{
		ClientGroups:        clientGroups,
		CustomTTL:           config.Duration(time.Hour),
		FilterUnmappedTypes: true,
	}
}

// Helper function to create wildcard-focused configuration
func createWildcardClientGroupsConfig(numWildcards int) config.CustomDNS {
	clientGroups := make(map[string]config.CustomDNSGroup)

	// Add default group
	clientGroups["default"] = config.CustomDNSGroup{
		Mapping: config.CustomDNSMapping{
			"default.test": {&dns.A{A: net.ParseIP("192.168.1.1")}},
		},
	}

	// Add wildcard groups
	patterns := []string{"laptop*", "desktop*", "server*", "mobile*", "iot*", "printer*", "camera*", "sensor*"}
	for i := 0; i < numWildcards; i++ {
		pattern := fmt.Sprintf("%s%d*", patterns[i%len(patterns)], i/len(patterns))
		clientGroups[pattern] = config.CustomDNSGroup{
			Mapping: config.CustomDNSMapping{
				fmt.Sprintf("device%d.test", i): {&dns.A{A: net.ParseIP(fmt.Sprintf("192.168.%d.%d", i/255+1, i%255+1))}},
			},
		}
	}

	return config.CustomDNS{
		ClientGroups:        clientGroups,
		CustomTTL:           config.Duration(time.Hour),
		FilterUnmappedTypes: true,
	}
}

// Helper function to create test requests for client group resolution
func createTestRequests(numRequests int) []*Request {
	requests := make([]*Request, numRequests)

	for i := 0; i < numRequests; i++ {
		var ip string
		var clientName string

		// Vary the request types to test different matching scenarios
		switch i % 4 {
		case 0:
			// Exact IP match
			ip = fmt.Sprintf("192.168.%d.%d", i/255, i%255)
			clientName = fmt.Sprintf("host%d", i)
		case 1:
			// CIDR match
			ip = fmt.Sprintf("10.%d.0.%d", i%256, i%256)
			clientName = fmt.Sprintf("subnet-device%d", i)
		case 2:
			// Wildcard match
			ip = fmt.Sprintf("172.16.%d.%d", i/255, i%255)
			clientName = fmt.Sprintf("client%d-device", i)
		case 3:
			// Default group
			ip = fmt.Sprintf("203.0.113.%d", i%256)
			clientName = fmt.Sprintf("unknown%d", i)
		}

		requests[i] = newRequestWithClientID(fmt.Sprintf("test%d.domain.", i), dns.Type(dns.TypeA), ip, clientName)
	}

	return requests
}

// Helper function to create DNS test requests
func createDNSTestRequests(numRequests int) []*Request {
	requests := make([]*Request, numRequests)
	domains := []string{"test.domain", "api.test", "web.test", "app.local", "service.internal"}

	for i := 0; i < numRequests; i++ {
		domain := domains[i%len(domains)]
		ip := fmt.Sprintf("192.168.%d.%d", i/255, i%255)
		clientName := fmt.Sprintf("client%d", i)

		requests[i] = newRequestWithClientID(fmt.Sprintf("%s.", domain), dns.Type(dns.TypeA), ip, clientName)
	}

	return requests
}

// Helper function to create CIDR-specific test requests
func createCIDRTestRequests(numRequests int) []*Request {
	requests := make([]*Request, numRequests)

	for i := 0; i < numRequests; i++ {
		// Create IP addresses that will match different CIDR ranges
		var ip string
		if i%3 == 0 {
			// Match /24 subnets
			ip = fmt.Sprintf("10.%d.0.%d", i%50, i%256)
		} else if i%3 == 1 {
			// Match /16 subnets
			ip = fmt.Sprintf("172.%d.%d.%d", i%25, i%256, i%256)
		} else {
			// No match - use default
			ip = fmt.Sprintf("203.0.113.%d", i%256)
		}

		requests[i] = newRequestWithClientID(fmt.Sprintf("test%d.domain.", i), dns.Type(dns.TypeA), ip, fmt.Sprintf("host%d", i))
	}

	return requests
}

// Helper function to create wildcard-specific test requests
func createWildcardTestRequests(numRequests int) []*Request {
	requests := make([]*Request, numRequests)
	patterns := []string{"laptop", "desktop", "server", "mobile", "iot", "printer", "camera", "sensor"}

	for i := 0; i < numRequests; i++ {
		// Create client names that will match different wildcard patterns
		var clientName string
		if i%4 != 3 {
			// Will match a wildcard pattern
			pattern := patterns[i%len(patterns)]
			clientName = fmt.Sprintf("%s%d-device", pattern, i/len(patterns))
		} else {
			// Won't match any pattern
			clientName = fmt.Sprintf("unknown%d", i)
		}

		ip := fmt.Sprintf("192.168.%d.%d", i/255, i%255)
		requests[i] = newRequestWithClientID(fmt.Sprintf("test%d.domain.", i), dns.Type(dns.TypeA), ip, clientName)
	}

	return requests
}
