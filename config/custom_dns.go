package config

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"

	"github.com/miekg/dns"
	"github.com/sirupsen/logrus"
)

// CustomDNS custom DNS configuration
type CustomDNS struct {
	RewriterConfig `yaml:",inline"`

	// Global settings
	CustomTTL           Duration `default:"1h"   yaml:"customTTL"`
	FilterUnmappedTypes bool     `default:"true" yaml:"filterUnmappedTypes"`

	// New client groups
	ClientGroups map[string]CustomDNSGroup `yaml:"clientGroups"`

	// Backward compatibility (deprecated)
	Mapping CustomDNSMapping `yaml:"mapping"`
	Zone    ZoneFileDNS      `default:""     yaml:"zone"`
}

// CustomDNSGroup represents DNS configuration for a specific client group
type CustomDNSGroup struct {
	RewriterConfig `yaml:",inline"`
	Mapping        CustomDNSMapping `yaml:"mapping"`
	Zone           ZoneFileDNS      `default:"" yaml:"zone"`
}

// migrate migrates old configuration format to new client groups format
func (c *CustomDNS) migrate(logger *logrus.Entry) bool {
	migrated := false

	// Conservative migration approach:
	// Only migrate legacy fields if client groups are explicitly defined
	// This preserves backward compatibility for pure legacy configurations

	hasLegacyFields := len(c.Mapping) > 0 || len(c.Rewrite) > 0 || len(c.Zone.RRs) > 0
	hasClientGroups := len(c.ClientGroups) > 0

	if hasClientGroups && hasLegacyFields {
		// User has adopted client groups but still has legacy fields - migrate them
		logger.Warn("migrating CustomDNS configuration from old format to client groups format")
		logger.Warn("consider updating your configuration to use 'clientGroups.default' instead of top-level 'mapping'")

		// Create or update default group with existing configuration
		if _, hasDefault := c.ClientGroups["default"]; !hasDefault {
			// Create new default group
			defaultGroup := CustomDNSGroup{
				RewriterConfig: c.RewriterConfig,
				Mapping:        c.Mapping,
				Zone:           c.Zone,
			}
			c.ClientGroups["default"] = defaultGroup
		} else {
			// Merge with existing default group
			defaultGroup := c.ClientGroups["default"]
			if defaultGroup.Mapping == nil {
				defaultGroup.Mapping = make(CustomDNSMapping)
			}
			for k, v := range c.Mapping {
				defaultGroup.Mapping[k] = v
			}
			if defaultGroup.RewriterConfig.Rewrite == nil {
				defaultGroup.RewriterConfig.Rewrite = make(map[string]string)
			}
			for k, v := range c.Rewrite {
				defaultGroup.RewriterConfig.Rewrite[k] = v
			}
			c.ClientGroups["default"] = defaultGroup
		}

		migrated = true

		// Clear old fields after migration to avoid confusion
		c.Mapping = make(CustomDNSMapping)
		c.RewriterConfig = RewriterConfig{}
		c.Zone = ZoneFileDNS{}
	}

	// Ensure we always have a default group if client groups are used
	if len(c.ClientGroups) > 0 {
		if _, hasDefault := c.ClientGroups["default"]; !hasDefault {
			// Create an empty default group
			c.ClientGroups["default"] = CustomDNSGroup{}
		}
	}

	return migrated
}

// validateClientGroups validates client group configuration
func (c *CustomDNS) validateClientGroups() error {
	for groupName := range c.ClientGroups {
		if err := c.validateClientGroupName(groupName); err != nil {
			return fmt.Errorf("invalid client group name '%s': %w", groupName, err)
		}
	}
	return nil
}

// validateClientGroupName validates a client group name (IP, CIDR, or wildcard pattern)
func (c *CustomDNS) validateClientGroupName(name string) error {
	// Skip validation for "default" group
	if name == "default" {
		return nil
	}

	// Check if it's a valid IP address
	if net.ParseIP(name) != nil {
		return nil
	}

	// Check if it looks like a CIDR (contains slash)
	if strings.Contains(name, "/") {
		if _, ipNet, err := net.ParseCIDR(name); err != nil {
			return fmt.Errorf("invalid CIDR notation: %w", err)
		} else {
			// Additional validation: check if prefix length is valid for IP version
			ones, bits := ipNet.Mask.Size()
			if ones < 0 || ones > bits {
				return fmt.Errorf("invalid CIDR prefix length in '%s'", name)
			}
			// Check IPv4 and IPv6 prefix length limits
			if ipNet.IP.To4() != nil && ones > 32 {
				return fmt.Errorf("IPv4 CIDR prefix length cannot exceed 32 in '%s'", name)
			}
			if ipNet.IP.To4() == nil && ones > 128 {
				return fmt.Errorf("IPv6 CIDR prefix length cannot exceed 128 in '%s'", name)
			}
		}
		return nil
	}

	// Check if it's a valid wildcard pattern
	if strings.Contains(name, "*") || strings.Contains(name, "?") || strings.Contains(name, "[") {
		if _, err := filepath.Match(name, "test"); err != nil {
			return fmt.Errorf("invalid wildcard pattern: %w", err)
		}
		return nil
	}

	// If it's not IP, CIDR, or wildcard, treat it as a literal client name (which is valid)
	return nil
}

type (
	CustomDNSMapping map[string]CustomDNSEntries
	CustomDNSEntries []dns.RR

	ZoneFileDNS struct {
		RRs        CustomDNSMapping
		configPath string
	}
)

func (z *ZoneFileDNS) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var input string
	if err := unmarshal(&input); err != nil {
		return err
	}

	result := make(CustomDNSMapping)

	zoneParser := dns.NewZoneParser(strings.NewReader(input), "", z.configPath)
	zoneParser.SetIncludeAllowed(true)

	for {
		zoneRR, ok := zoneParser.Next()

		if !ok {
			if zoneParser.Err() != nil {
				return zoneParser.Err()
			}

			// Done
			break
		}

		domain := zoneRR.Header().Name

		if _, ok := result[domain]; !ok {
			result[domain] = make(CustomDNSEntries, 0, 1)
		}

		result[domain] = append(result[domain], zoneRR)
	}

	z.RRs = result

	return nil
}

func (c *CustomDNSEntries) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var input string
	if err := unmarshal(&input); err != nil {
		return err
	}

	parts := strings.Split(input, ",")
	result := make(CustomDNSEntries, len(parts))

	for i, part := range parts {
		rr, err := configToRR(strings.TrimSpace(part))
		if err != nil {
			return err
		}

		result[i] = rr
	}

	*c = result

	return nil
}

// IsEnabled implements `config.Configurable`.
func (c *CustomDNS) IsEnabled() bool {
	return len(c.Mapping) != 0 || len(c.ClientGroups) != 0
}

// LogConfig implements `config.Configurable`.
func (c *CustomDNS) LogConfig(logger *logrus.Entry) {
	logger.Debugf("TTL = %s", c.CustomTTL)
	logger.Debugf("filterUnmappedTypes = %t", c.FilterUnmappedTypes)

	if len(c.ClientGroups) > 0 {
		logger.Info("client groups configured:")
		for groupName, group := range c.ClientGroups {
			logger.Infof("  %s:", groupName)
			if len(group.Mapping) > 0 {
				logger.Info("    mapping:")
				for key, val := range group.Mapping {
					logger.Infof("      %s = %s", key, val)
				}
			}
			if len(group.Rewrite) > 0 {
				logger.Info("    rewrite:")
				for key, val := range group.Rewrite {
					logger.Infof("      %s = %s", key, val)
				}
			}
		}
	}

	if len(c.Mapping) > 0 {
		logger.Info("mapping (deprecated - use clientGroups):")
		for key, val := range c.Mapping {
			logger.Infof("  %s = %s", key, val)
		}
	}
}

func configToRR(ipStr string) (dns.RR, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address '%s'", ipStr)
	}

	if ip.To4() != nil {
		a := new(dns.A)
		a.A = ip

		return a, nil
	}

	aaaa := new(dns.AAAA)
	aaaa.AAAA = ip

	return aaaa, nil
}
