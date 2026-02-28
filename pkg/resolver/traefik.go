package resolver

import (
	"context"
	"fmt"
	"regexp"

	"github.com/docker/docker/api/types/container"
)

var (
	routerRuleLabel = regexp.MustCompile(`^traefik\.http\.routers\.[^.]+\.rule$`)
	hostCall        = regexp.MustCompile(`Host\(([^)]+)\)`)
	quotedString    = regexp.MustCompile(`["` + "`" + `]([^"` + "`" + `]+)["` + "`" + `]`)
)

type TraefikResolver struct {
	ip IPProvider
}

func NewTraefik(ip IPProvider) *TraefikResolver {
	return &TraefikResolver{ip: ip}
}

func (r *TraefikResolver) Records(ctx context.Context, info container.InspectResponse) ([]Record, error) {
	if info.Config == nil {
		return nil, nil
	}

	proxyIP, err := r.ip.ProxyIP(ctx)
	if err != nil {
		return nil, fmt.Errorf("proxy IP: %w", err)
	}

	var records []Record
	for key, val := range info.Config.Labels {
		if !routerRuleLabel.MatchString(key) {
			continue
		}
		for _, match := range hostCall.FindAllStringSubmatch(val, -1) {
			args := match[1]
			for _, qm := range quotedString.FindAllStringSubmatch(args, -1) {
				records = append(records, Record{Hostname: qm[1], IP: proxyIP})
			}
		}
	}
	return records, nil
}
