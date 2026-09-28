package dnsmgr

import (
	"reflect"
	"testing"
)

func TestIsPrivateIPv4(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"10.0.0.1", true},
		{"10.255.255.254", true},
		{"172.16.0.1", true},
		{"172.31.255.254", true},
		{"172.32.0.1", false},
		{"192.168.1.1", true},
		{"192.168.100.1", true},
		{"100.64.0.1", true},
		{"100.127.255.254", true},
		{"100.128.0.1", false},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"118.238.201.33", false},
		{"invalid-ip", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			if got := isPrivateIPv4(tt.ip); got != tt.want {
				t.Errorf("isPrivateIPv4(%q) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

func TestPrioritizeDNSServers(t *testing.T) {
	tests := []struct {
		name    string
		servers []string
		want    []string
	}{
		{
			name:    "public before private",
			servers: []string{"118.238.201.33", "192.168.100.1"},
			want:    []string{"192.168.100.1", "118.238.201.33"},
		},
		{
			name:    "already prioritized with duplicates and invalid",
			servers: []string{"192.168.100.1", "10.0.0.1", "118.238.201.33", "192.168.100.1", "", "0.0.0.0", "8.8.8.8"},
			want:    []string{"192.168.100.1", "10.0.0.1", "118.238.201.33", "8.8.8.8"},
		},
		{
			name:    "all private",
			servers: []string{"10.1.2.3", "192.168.1.1"},
			want:    []string{"10.1.2.3", "192.168.1.1"},
		},
		{
			name:    "all public",
			servers: []string{"8.8.8.8", "1.1.1.1"},
			want:    []string{"8.8.8.8", "1.1.1.1"},
		},
		{
			name:    "empty",
			servers: []string{},
			want:    []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PrioritizeDNSServers(tt.servers)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("PrioritizeDNSServers(%v) = %v, want %v", tt.servers, got, tt.want)
			}
		})
	}
}
