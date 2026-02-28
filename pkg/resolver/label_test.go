package resolver

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

func labelInspect(labels map[string]string, ip string) container.InspectResponse {
	nets := map[string]*network.EndpointSettings{}
	if ip != "" {
		nets["bridge"] = &network.EndpointSettings{IPAddress: ip}
	}
	return container.InspectResponse{
		Config: &container.Config{Labels: labels},
		NetworkSettings: &container.NetworkSettings{
			Networks: nets,
		},
	}
}

func TestLabelResolver_LabelAndIP(t *testing.T) {
	r := NewLabel("dns.hostname")
	records, err := r.Records(context.Background(), labelInspect(map[string]string{"dns.hostname": "app.example.com"}, "10.0.0.1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 || records[0].Hostname != "app.example.com" || records[0].IP != "10.0.0.1" {
		t.Fatalf("unexpected records: %v", records)
	}
}

func TestLabelResolver_MissingLabel(t *testing.T) {
	r := NewLabel("dns.hostname")
	records, err := r.Records(context.Background(), labelInspect(map[string]string{}, "10.0.0.1"))
	if err != nil || records != nil {
		t.Fatalf("expected nil nil, got %v %v", records, err)
	}
}

func TestLabelResolver_NoIP(t *testing.T) {
	r := NewLabel("dns.hostname")
	records, err := r.Records(context.Background(), labelInspect(map[string]string{"dns.hostname": "app.example.com"}, ""))
	if err != nil || records != nil {
		t.Fatalf("expected nil nil, got %v %v", records, err)
	}
}
