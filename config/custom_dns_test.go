package config

import (
	"errors"
	"net"
	"strings"

	. "github.com/0xERR0R/blocky/helpertest"
	"github.com/creasty/defaults"
	"github.com/miekg/dns"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CustomDNSConfig", func() {
	var cfg CustomDNS

	suiteBeforeEach()

	BeforeEach(func() {
		cfg = CustomDNS{
			Mapping: CustomDNSMapping{
				"custom.domain": {&dns.A{A: net.ParseIP("192.168.143.123")}},
				"ip6.domain":    {&dns.AAAA{AAAA: net.ParseIP("2001:0db8:85a3:0000:0000:8a2e:0370:7334")}},
				"multiple.ips": {
					&dns.A{A: net.ParseIP("192.168.143.123")},
					&dns.A{A: net.ParseIP("192.168.143.125")},
					&dns.AAAA{AAAA: net.ParseIP("2001:0db8:85a3:0000:0000:8a2e:0370:7334")},
				},
			},
		}
	})

	Describe("IsEnabled", func() {
		It("should be false by default", func() {
			cfg := CustomDNS{}
			Expect(defaults.Set(&cfg)).Should(Succeed())

			Expect(cfg.IsEnabled()).Should(BeFalse())
		})

		When("enabled", func() {
			It("should be true", func() {
				Expect(cfg.IsEnabled()).Should(BeTrue())
			})
		})

		When("disabled", func() {
			It("should be false", func() {
				cfg := CustomDNS{}

				Expect(cfg.IsEnabled()).Should(BeFalse())
			})
		})
	})

	Describe("LogConfig", func() {
		It("should log configuration", func() {
			cfg.LogConfig(logger)

			Expect(hook.Calls).ShouldNot(BeEmpty())
			Expect(hook.Messages).Should(ContainElements(
				ContainSubstring("custom.domain = "),
				ContainSubstring("ip6.domain = "),
				ContainSubstring("multiple.ips = "),
			))
		})
	})

	Describe("CustomDNSEntries UnmarshalYAML", func() {
		It("Should parse config as map", func() {
			c := CustomDNSEntries{}
			err := c.UnmarshalYAML(func(i interface{}) error {
				*i.(*string) = "1.2.3.4"

				return nil
			})
			Expect(err).Should(Succeed())
			Expect(c).Should(HaveLen(1))

			aRecord := c[0].(*dns.A)
			Expect(aRecord.A).Should(Equal(net.ParseIP("1.2.3.4")))
		})

		It("Should parse multiple ips as comma separated string", func() {
			c := CustomDNSEntries{}
			err := c.UnmarshalYAML(func(i interface{}) error {
				*i.(*string) = "1.2.3.4,2.3.4.5"

				return nil
			})
			Expect(err).Should(Succeed())
			Expect(c).Should(HaveLen(2))

			Expect(c[0].(*dns.A).A).Should(Equal(net.ParseIP("1.2.3.4")))
			Expect(c[1].(*dns.A).A).Should(Equal(net.ParseIP("2.3.4.5")))
		})

		It("Should parse multiple ips as comma separated string with whitespace", func() {
			c := CustomDNSEntries{}
			err := c.UnmarshalYAML(func(i interface{}) error {
				*i.(*string) = "1.2.3.4, 2.3.4.5 ,   3.4.5.6"

				return nil
			})
			Expect(err).Should(Succeed())
			Expect(c).Should(HaveLen(3))

			Expect(c[0].(*dns.A).A).Should(Equal(net.ParseIP("1.2.3.4")))
			Expect(c[1].(*dns.A).A).Should(Equal(net.ParseIP("2.3.4.5")))
			Expect(c[2].(*dns.A).A).Should(Equal(net.ParseIP("3.4.5.6")))
		})

		It("should fail if wrong YAML format", func() {
			c := &CustomDNSEntries{}
			err := c.UnmarshalYAML(func(i interface{}) error {
				return errors.New("some err")
			})
			Expect(err).Should(HaveOccurred())
			Expect(err).Should(MatchError("some err"))
		})
	})

	Describe("ZoneFileDNS UnmarshalYAML", func() {
		It("Should parse config as map", func() {
			z := ZoneFileDNS{}
			err := z.UnmarshalYAML(func(i interface{}) error {
				*i.(*string) = strings.TrimSpace(`
$ORIGIN example.com.
www 3600 A 1.2.3.4
www 3600 AAAA 2001:0db8:85a3:0000:0000:8a2e:0370:7334
www6 3600 AAAA 2001:0db8:85a3:0000:0000:8a2e:0370:7334
cname 3600 CNAME www
				`)

				return nil
			})
			Expect(err).Should(Succeed())
			Expect(z.RRs).Should(HaveLen(3))

			Expect(z.RRs["www.example.com."]).
				Should(SatisfyAll(
					HaveLen(2),
					ContainElements(
						SatisfyAll(
							BeDNSRecord("www.example.com.", A, "1.2.3.4"),
							HaveTTL(BeNumerically("==", 3600)),
						),
						SatisfyAll(
							BeDNSRecord("www.example.com.", AAAA, "2001:db8:85a3::8a2e:370:7334"),
							HaveTTL(BeNumerically("==", 3600)),
						))))

			Expect(z.RRs["www6.example.com."]).
				Should(SatisfyAll(
					HaveLen(1),
					ContainElements(
						SatisfyAll(
							BeDNSRecord("www6.example.com.", AAAA, "2001:db8:85a3::8a2e:370:7334"),
							HaveTTL(BeNumerically("==", 3600)),
						))))

			Expect(z.RRs["cname.example.com."]).
				Should(SatisfyAll(
					HaveLen(1),
					ContainElements(
						SatisfyAll(
							BeDNSRecord("cname.example.com.", CNAME, "www.example.com."),
							HaveTTL(BeNumerically("==", 3600)),
						))))
		})

		It("Should support the $INCLUDE directive with an absolute path", func() {
			folder := NewTmpFolder("zones")
			file := folder.CreateStringFile("other.zone", "www 3600 A 1.2.3.4")

			z := ZoneFileDNS{}
			err := z.UnmarshalYAML(func(i interface{}) error {
				*i.(*string) = strings.TrimSpace(`
$ORIGIN example.com.
$INCLUDE ` + file.Path)

				return nil
			})
			Expect(err).Should(Succeed())
			Expect(z.RRs).Should(HaveLen(1))

			Expect(z.RRs["www.example.com."]).
				Should(SatisfyAll(

					HaveLen(1),
					ContainElements(
						SatisfyAll(
							BeDNSRecord("www.example.com.", A, "1.2.3.4"),
							HaveTTL(BeNumerically("==", 3600)),
						)),
				))
		})

		It("Should return an error if the zone file is malformed", func() {
			z := ZoneFileDNS{}
			err := z.UnmarshalYAML(func(i interface{}) error {
				*i.(*string) = strings.TrimSpace(`
$ORIGIN example.com.
www A 1.2.3.4
				`)

				return nil
			})
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("dns: missing TTL with no previous value"))
		})
		It("Should return an error if a relative record is provided without an origin", func() {
			z := ZoneFileDNS{}
			err := z.UnmarshalYAML(func(i interface{}) error {
				*i.(*string) = strings.TrimSpace(`
$TTL 3600
www A 1.2.3.4
				`)

				return nil
			})
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("dns: bad owner name: \"www\""))
		})
		It("Should return an error if the unmarshall function returns an error", func() {
			z := ZoneFileDNS{}
			err := z.UnmarshalYAML(func(i interface{}) error {
				return errors.New("Failed to unmarshal")
			})
			Expect(err).Should(HaveOccurred())
			Expect(err).Should(MatchError("Failed to unmarshal"))
		})
	})

	Describe("ClientGroups", func() {
		var cfgWithGroups CustomDNS

		BeforeEach(func() {
			cfgWithGroups = CustomDNS{
				ClientGroups: map[string]CustomDNSGroup{
					"default": {
						Mapping: CustomDNSMapping{
							"default.domain": {&dns.A{A: net.ParseIP("192.168.1.1")}},
						},
					},
					"laptop*": {
						Mapping: CustomDNSMapping{
							"laptop.domain": {&dns.A{A: net.ParseIP("192.168.1.100")}},
						},
						RewriterConfig: RewriterConfig{
							Rewrite: map[string]string{
								"^laptop-(.*)$": "device-$1.internal",
							},
						},
					},
					"192.168.1.0/24": {
						Mapping: CustomDNSMapping{
							"internal.domain": {&dns.A{A: net.ParseIP("10.0.0.1")}},
						},
					},
					"192.168.1.10": {
						Mapping: CustomDNSMapping{
							"specific.domain": {&dns.A{A: net.ParseIP("192.168.1.99")}},
						},
					},
				},
			}
		})

		Describe("IsEnabled", func() {
			It("should be true when client groups are configured", func() {
				Expect(cfgWithGroups.IsEnabled()).Should(BeTrue())
			})

			It("should be false when no client groups are configured", func() {
				cfg := CustomDNS{ClientGroups: map[string]CustomDNSGroup{}}
				Expect(cfg.IsEnabled()).Should(BeFalse())
			})
		})

		Describe("migrate", func() {
			It("should migrate legacy config to default group when client groups exist", func() {
				legacyCfg := CustomDNS{
					Mapping: CustomDNSMapping{
						"legacy.domain": {&dns.A{A: net.ParseIP("1.2.3.4")}},
					},
					RewriterConfig: RewriterConfig{
						Rewrite: map[string]string{
							"^old-(.*)$": "new-$1.com",
						},
					},
					// Add client groups to trigger migration
					ClientGroups: map[string]CustomDNSGroup{
						"test": {
							Mapping: CustomDNSMapping{
								"test.domain": {&dns.A{A: net.ParseIP("5.6.7.8")}},
							},
						},
					},
				}

				migrated := legacyCfg.migrate(logger)

				Expect(migrated).Should(BeTrue())
				Expect(legacyCfg.ClientGroups).Should(HaveKey("default"))
				Expect(legacyCfg.ClientGroups["default"].Mapping).Should(HaveKey("legacy.domain"))
				Expect(legacyCfg.ClientGroups["default"].RewriterConfig.Rewrite).Should(HaveLen(1))
				Expect(legacyCfg.Mapping).Should(BeEmpty())
				Expect(legacyCfg.RewriterConfig.Rewrite).Should(BeEmpty())
			})

			It("should NOT migrate pure legacy config for backward compatibility", func() {
				legacyCfg := CustomDNS{
					Mapping: CustomDNSMapping{
						"legacy.domain": {&dns.A{A: net.ParseIP("1.2.3.4")}},
					},
					RewriterConfig: RewriterConfig{
						Rewrite: map[string]string{
							"^old-(.*)$": "new-$1.com",
						},
					},
					// No client groups - should preserve legacy format
				}

				migrated := legacyCfg.migrate(logger)

				Expect(migrated).Should(BeFalse())
				Expect(legacyCfg.ClientGroups).Should(BeEmpty())
				Expect(legacyCfg.Mapping).Should(HaveKey("legacy.domain"))
				Expect(legacyCfg.RewriterConfig.Rewrite).Should(HaveLen(1))
			})

			It("should not migrate when client groups already exist", func() {
				originalMapping := cfgWithGroups.ClientGroups["default"].Mapping
				migrated := cfgWithGroups.migrate(logger)

				Expect(migrated).Should(BeFalse())
				Expect(cfgWithGroups.ClientGroups["default"].Mapping).Should(Equal(originalMapping))
			})
		})

		Describe("validateClientGroups", func() {
			It("should pass validation for valid client groups", func() {
				err := cfgWithGroups.validateClientGroups()
				Expect(err).Should(Succeed())
			})

			It("should fail for invalid CIDR", func() {
				cfgWithGroups.ClientGroups["192.168.1.0/33"] = CustomDNSGroup{
					Mapping: CustomDNSMapping{"test": {&dns.A{A: net.ParseIP("1.2.3.4")}}},
				}

				err := cfgWithGroups.validateClientGroups()
				Expect(err).Should(HaveOccurred())
				Expect(err.Error()).Should(ContainSubstring("invalid client group name"))
			})

			It("should fail for invalid wildcard pattern", func() {
				cfgWithGroups.ClientGroups["client[invalid"] = CustomDNSGroup{
					Mapping: CustomDNSMapping{"test": {&dns.A{A: net.ParseIP("1.2.3.4")}}},
				}

				err := cfgWithGroups.validateClientGroups()
				Expect(err).Should(HaveOccurred())
				Expect(err.Error()).Should(ContainSubstring("invalid wildcard pattern"))
			})

			It("should accept wildcard patterns", func() {
				cfgWithGroups.ClientGroups["client*"] = CustomDNSGroup{
					Mapping: CustomDNSMapping{"test": {&dns.A{A: net.ParseIP("1.2.3.4")}}},
				}

				err := cfgWithGroups.validateClientGroups()
				Expect(err).Should(Succeed())
			})

			It("should accept arbitrary names for client groups", func() {
				cfgWithGroups.ClientGroups["mydevices"] = CustomDNSGroup{
					Mapping: CustomDNSMapping{"test": {&dns.A{A: net.ParseIP("1.2.3.4")}}},
				}

				err := cfgWithGroups.validateClientGroups()
				Expect(err).Should(Succeed())
			})
		})

		Describe("LogConfig", func() {
			It("should log client groups configuration", func() {
				cfgWithGroups.LogConfig(logger)

				Expect(hook.Calls).ShouldNot(BeEmpty())
				Expect(hook.Messages).Should(ContainElements(
					ContainSubstring("client groups configured"),
					ContainSubstring("default"),
					ContainSubstring("laptop*"),
					ContainSubstring("192.168.1.0/24"),
					ContainSubstring("192.168.1.10"),
				))
			})
		})
	})
})
