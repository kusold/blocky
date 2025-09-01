package resolver

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"

	"github.com/0xERR0R/blocky/config"
	"github.com/0xERR0R/blocky/model"
	"github.com/0xERR0R/blocky/util"

	"github.com/miekg/dns"
	"github.com/sirupsen/logrus"
)

type createAnswerFunc func(question dns.Question, ip net.IP, ttl uint32) (dns.RR, error)

// CustomDNSResolver resolves passed domain name to ip address defined in domain-IP map
type CustomDNSResolver struct {
	configurable[*config.CustomDNS]
	NextResolver
	typed

	createAnswerFromQuestion createAnswerFunc

	// Client group support
	clientGroups map[string]config.CustomDNSGroup

	// Backward compatibility (for single mapping)
	mapping          config.CustomDNSMapping
	reverseAddresses map[string][]string
}

// NewCustomDNSResolver creates new resolver instance
func NewCustomDNSResolver(cfg config.CustomDNS) *CustomDNSResolver {
	r := &CustomDNSResolver{
		configurable:             withConfig(&cfg),
		typed:                    withType("custom_dns"),
		createAnswerFromQuestion: util.CreateAnswerFromQuestion,
	}

	// Handle client groups
	if len(cfg.ClientGroups) > 0 {
		r.clientGroups = make(map[string]config.CustomDNSGroup, len(cfg.ClientGroups))

		// Copy client groups and process TTL for mapping entries
		for groupName, group := range cfg.ClientGroups {
			// Process TTL for mapping entries
			for _, entries := range group.Mapping {
				for _, entry := range entries {
					entry.Header().Ttl = cfg.CustomTTL.SecondsU32()
				}
			}
			r.clientGroups[groupName] = group
		}
	} else {
		// Backward compatibility: create single mapping from old format
		r.mapping = make(config.CustomDNSMapping, len(cfg.Mapping)+len(cfg.Zone.RRs))

		// Process old-style mapping
		for url, entries := range cfg.Mapping {
			url = util.ExtractDomainOnly(url)
			r.mapping[url] = entries

			for _, entry := range entries {
				entry.Header().Ttl = cfg.CustomTTL.SecondsU32()
			}
		}

		// Process old-style zone
		for url, entries := range cfg.Zone.RRs {
			url = util.ExtractDomainOnly(url)
			r.mapping[url] = entries
		}
	}

	// Build reverse address mapping
	r.reverseAddresses = r.buildReverseAddressMappings()

	return r
}

// buildReverseAddressMappings creates reverse DNS mappings for all groups
func (r *CustomDNSResolver) buildReverseAddressMappings() map[string][]string {
	reverse := make(map[string][]string)

	// Handle client groups
	for _, group := range r.clientGroups {
		r.addReverseMapping(reverse, group.Mapping)
		r.addReverseMapping(reverse, group.Zone.RRs)
	}

	// Handle legacy mapping
	if r.mapping != nil {
		r.addReverseMapping(reverse, r.mapping)
	}

	return reverse
}

// addReverseMapping adds reverse DNS mappings for a DNS mapping
func (r *CustomDNSResolver) addReverseMapping(reverse map[string][]string, mapping config.CustomDNSMapping) {
	for url, entries := range mapping {
		for _, entry := range entries {
			a, isA := entry.(*dns.A)
			if isA {
				reverseAddr, _ := dns.ReverseAddr(a.A.String())
				reverse[reverseAddr] = append(reverse[reverseAddr], url)
			}

			aaaa, isAAAA := entry.(*dns.AAAA)
			if isAAAA {
				reverseAddr, _ := dns.ReverseAddr(aaaa.AAAA.String())
				reverse[reverseAddr] = append(reverse[reverseAddr], url)
			}
		}
	}
}

func isSupportedType(ip net.IP, question dns.Question) bool {
	return (ip.To4() != nil && question.Qtype == dns.TypeA) ||
		(strings.Contains(ip.String(), ":") && question.Qtype == dns.TypeAAAA)
}

// resolveClientGroup determines which client group to use for a request
func (r *CustomDNSResolver) resolveClientGroup(request *model.Request) string {
	// If no client groups configured, use legacy mode
	if len(r.clientGroups) == 0 {
		return ""
	}

	clientIP := request.ClientIP.String()
	clientName := request.RequestClientID

	// 1. Check exact IP match
	if _, exists := r.clientGroups[clientIP]; exists {
		return clientIP
	}

	// 2. Check client name patterns (with wildcards)
	for groupName := range r.clientGroups {
		if util.ClientNameMatchesGroupName(groupName, clientName) {
			return groupName
		}
	}

	// 3. Check CIDR subnet matches
	for groupName := range r.clientGroups {
		if util.CidrContainsIP(groupName, request.ClientIP) {
			return groupName
		}
	}

	// 4. Fall back to default group
	return "default"
}

// getClientGroupConfig returns the appropriate DNS mapping and rewrite config for a client group
func (r *CustomDNSResolver) getClientGroupConfig(groupName string) (config.CustomDNSMapping, config.RewriterConfig) {
	// Legacy mode: use the single mapping
	if groupName == "" {
		return r.mapping, r.cfg.RewriterConfig
	}

	// Client group mode: get group-specific config
	if group, exists := r.clientGroups[groupName]; exists {
		// Combine group mapping with zone mapping
		combined := make(config.CustomDNSMapping)
		for domain, entries := range group.Mapping {
			combined[domain] = entries
		}
		for domain, entries := range group.Zone.RRs {
			combined[domain] = entries
		}
		return combined, group.RewriterConfig
	}

	// Fallback to empty config
	return make(config.CustomDNSMapping), config.RewriterConfig{}
}

func (r *CustomDNSResolver) handleReverseDNS(request *model.Request) *model.Response {
	question := request.Req.Question[0]
	if question.Qtype == dns.TypePTR {
		urls, found := r.reverseAddresses[question.Name]
		if found {
			response := new(dns.Msg)
			response.SetReply(request.Req)

			for _, url := range urls {
				h := util.CreateHeader(question, r.cfg.CustomTTL.SecondsU32())
				ptr := new(dns.PTR)
				ptr.Ptr = dns.Fqdn(url)
				ptr.Hdr = h
				response.Answer = append(response.Answer, ptr)
			}

			return &model.Response{Res: response, RType: model.ResponseTypeCUSTOMDNS, Reason: "CUSTOM DNS"}
		}
	}

	return nil
}

func (r *CustomDNSResolver) processRequest(
	ctx context.Context,
	logger *logrus.Entry,
	request *model.Request,
	resolvedCnames []string,
) (*model.Response, error) {
	response := new(dns.Msg)
	response.SetReply(request.Req)

	question := request.Req.Question[0]
	domain := util.ExtractDomain(question)

	// Resolve client group and get appropriate mapping
	clientGroup := r.resolveClientGroup(request)
	mapping, rewriterConfig := r.getClientGroupConfig(clientGroup)

	// Apply domain rewriting if configured
	originalDomain := domain
	for rewriteFrom, rewriteTo := range rewriterConfig.Rewrite {
		if strings.Contains(domain, rewriteFrom) {
			domain = strings.ReplaceAll(domain, rewriteFrom, rewriteTo)
			logger.WithFields(logrus.Fields{
				"originalDomain":  originalDomain,
				"rewrittenDomain": domain,
				"clientGroup":     clientGroup,
			}).Debugf("domain rewritten")
			break
		}
	}

	for len(domain) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		entries, found := mapping[domain]

		if found {
			for _, entry := range entries {
				result, err := r.processDNSEntry(ctx, logger, request, resolvedCnames, question, entry)
				if err != nil {
					return nil, err
				}

				response.Answer = append(response.Answer, result...)
			}

			if len(response.Answer) > 0 {
				logger.WithFields(logrus.Fields{
					"answer":      util.AnswerToString(response.Answer),
					"domain":      domain,
					"clientGroup": clientGroup,
				}).Debugf("returning custom dns entry")

				return &model.Response{Res: response, RType: model.ResponseTypeCUSTOMDNS, Reason: "CUSTOM DNS"}, nil
			}

			// Mapping exists for this domain, but for another type
			if !r.cfg.FilterUnmappedTypes {
				// go to next resolver
				break
			}

			// return NOERROR with empty result
			return &model.Response{Res: response, RType: model.ResponseTypeCUSTOMDNS, Reason: "CUSTOM DNS"}, nil
		}

		if i := strings.IndexRune(domain, '.'); i >= 0 {
			domain = domain[i+1:]
		} else {
			break
		}
	}

	logger.WithField("next_resolver", Name(r.next)).Trace("go to next resolver")

	return r.next.Resolve(ctx, request)
}

func (r *CustomDNSResolver) processDNSEntry(
	ctx context.Context,
	logger *logrus.Entry,
	request *model.Request,
	resolvedCnames []string,
	question dns.Question,
	entry dns.RR,
) ([]dns.RR, error) {
	switch v := entry.(type) {
	case *dns.A:
		return r.processIP(v.A, question, v.Header().Ttl)
	case *dns.AAAA:
		return r.processIP(v.AAAA, question, v.Header().Ttl)
	case *dns.TXT:
		return r.processTXT(v.Txt, question, v.Header().Ttl)
	case *dns.SRV:
		return r.processSRV(*v, question, v.Header().Ttl)
	case *dns.CNAME:
		return r.processCNAME(ctx, logger, request, *v, resolvedCnames, question, v.Header().Ttl)
	}

	return nil, fmt.Errorf("unsupported customDNS RR type %T", entry)
}

// Resolve uses internal mapping to resolve the query
func (r *CustomDNSResolver) Resolve(ctx context.Context, request *model.Request) (*model.Response, error) {
	ctx, logger := r.log(ctx)

	reverseResp := r.handleReverseDNS(request)
	if reverseResp != nil {
		return reverseResp, nil
	}

	return r.processRequest(ctx, logger, request, make([]string, 0, len(r.cfg.Mapping)))
}

func (r *CustomDNSResolver) processIP(ip net.IP, question dns.Question, ttl uint32) (result []dns.RR, err error) {
	result = make([]dns.RR, 0)

	if isSupportedType(ip, question) {
		rr, err := r.createAnswerFromQuestion(question, ip, ttl)
		if err != nil {
			return nil, err
		}

		result = append(result, rr)
	}

	return result, nil
}

func (r *CustomDNSResolver) processTXT(value []string, question dns.Question, ttl uint32) (result []dns.RR, err error) {
	if question.Qtype == dns.TypeTXT {
		txt := new(dns.TXT)
		txt.Hdr = dns.RR_Header{Class: dns.ClassINET, Ttl: ttl, Rrtype: dns.TypeTXT, Name: question.Name}
		txt.Txt = value
		result = append(result, txt)
	}

	return result, nil
}

func (r *CustomDNSResolver) processSRV(
	targetSRV dns.SRV,
	question dns.Question,
	ttl uint32,
) (result []dns.RR, err error) {
	if question.Qtype == dns.TypeSRV {
		srv := new(dns.SRV)
		srv.Hdr = dns.RR_Header{Class: dns.ClassINET, Ttl: ttl, Rrtype: dns.TypeSRV, Name: question.Name}
		srv.Priority = targetSRV.Priority
		srv.Weight = targetSRV.Weight
		srv.Port = targetSRV.Port
		srv.Target = targetSRV.Target
		result = append(result, srv)
	}

	return result, nil
}

func (r *CustomDNSResolver) processCNAME(
	ctx context.Context,
	logger *logrus.Entry,
	request *model.Request,
	targetCname dns.CNAME,
	resolvedCnames []string,
	question dns.Question,
	ttl uint32,
) (result []dns.RR, err error) {
	cname := new(dns.CNAME)
	cname.Hdr = dns.RR_Header{Class: dns.ClassINET, Ttl: ttl, Rrtype: dns.TypeCNAME, Name: question.Name}
	cname.Target = dns.Fqdn(targetCname.Target)
	result = append(result, cname)

	if question.Qtype == dns.TypeCNAME {
		return result, nil
	}

	targetWithoutDot := strings.TrimSuffix(targetCname.Target, ".")

	if slices.Contains(resolvedCnames, targetWithoutDot) {
		return nil, fmt.Errorf("CNAME loop detected: %v", append(resolvedCnames, targetWithoutDot))
	}

	cnames := resolvedCnames
	cnames = append(cnames, targetWithoutDot)

	clientIP := request.ClientIP.String()
	clientID := request.RequestClientID
	targetRequest := newRequestWithClientID(targetWithoutDot, dns.Type(question.Qtype), clientIP, clientID)

	// resolve the target recursively
	targetResp, err := r.processRequest(ctx, logger, targetRequest, cnames)
	if err != nil {
		return nil, err
	}

	result = append(result, targetResp.Res.Answer...)

	return result, nil
}

func (r *CustomDNSResolver) CreateAnswerFromQuestion(newFunc createAnswerFunc) {
	r.createAnswerFromQuestion = newFunc
}
