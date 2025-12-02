package utils

import (
	"reflect"
	"testing"
)

func TestIPMatcher_DebugInfo(t *testing.T) {
	tests := []struct {
		name            string
		exemptIPs       string
		trustedProxyIPs string
		wantExempt      []string
		wantTrusted     []string
	}{
		{
			name:            "empty",
			exemptIPs:       "",
			trustedProxyIPs: "",
			wantExempt:      nil,
			wantTrusted:     nil,
		},
		{
			name:            "single CIDR",
			exemptIPs:       "192.168.1.0/24",
			trustedProxyIPs: "",
			wantExempt:      []string{"192.168.1.0/24"},
			wantTrusted:     nil,
		},
		{
			name:            "multiple CIDRs",
			exemptIPs:       "10.0.0.0/8, 172.16.0.0/12",
			trustedProxyIPs: "",
			wantExempt:      []string{"10.0.0.0/8", "172.16.0.0/12"},
			wantTrusted:     nil,
		},
		{
			name:            "single IP",
			exemptIPs:       "",
			trustedProxyIPs: "192.168.1.1",
			wantExempt:      nil,
			wantTrusted:     []string{"192.168.1.1"},
		},
		{
			name:            "multiple IPs",
			exemptIPs:       "",
			trustedProxyIPs: "192.168.1.1, 10.0.0.1",
			wantExempt:      nil,
			wantTrusted:     []string{"192.168.1.1", "10.0.0.1"},
		},
		{
			name:            "mixed",
			exemptIPs:       "192.168.1.0/24",
			trustedProxyIPs: "10.0.0.1, 10.0.0.2",
			wantExempt:      []string{"192.168.1.0/24"},
			wantTrusted:     []string{"10.0.0.1", "10.0.0.2"},
		},
		{
			name:            "IPv6 CIDR",
			exemptIPs:       "2001:db8::/32",
			trustedProxyIPs: "",
			wantExempt:      []string{"2001:db8::/32"},
			wantTrusted:     nil,
		},
		{
			name:            "IPv6 IP",
			exemptIPs:       "",
			trustedProxyIPs: "2001:db8::1",
			wantExempt:      nil,
			wantTrusted:     []string{"2001:db8::1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := NewIPMatcher(tt.exemptIPs, tt.trustedProxyIPs)
			if err != nil {
				t.Fatalf("NewIPMatcher() error = %v", err)
			}
			gotExempt, gotTrusted := matcher.DebugInfo()
			if !reflect.DeepEqual(gotExempt, tt.wantExempt) {
				t.Errorf("DebugInfo() exemptCIDRs = %v, want %v", gotExempt, tt.wantExempt)
			}
			if !reflect.DeepEqual(gotTrusted, tt.wantTrusted) {
				t.Errorf("DebugInfo() trustedProxies = %v, want %v", gotTrusted, tt.wantTrusted)
			}
		})
	}
}
