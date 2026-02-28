package resolver

import (
	"context"

	"github.com/docker/docker/api/types/container"
)

type LabelResolver struct {
	label string
}

func NewLabel(label string) *LabelResolver {
	return &LabelResolver{label: label}
}

func (r *LabelResolver) Records(_ context.Context, info container.InspectResponse) ([]Record, error) {
	if info.Config == nil {
		return nil, nil
	}
	hostname, ok := info.Config.Labels[r.label]
	if !ok {
		return nil, nil
	}

	var ip string
	if info.NetworkSettings != nil {
		for _, net := range info.NetworkSettings.Networks {
			if net.IPAddress != "" {
				ip = net.IPAddress
				break
			}
		}
	}
	if ip == "" {
		return nil, nil
	}

	return []Record{{Hostname: hostname, IP: ip}}, nil
}
