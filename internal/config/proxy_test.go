package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestLoadTrustedProxies(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  []string
	}{
		{"", nil},
		{" 127.0.0.1 , ::1/128, 10.10.0.0/16 ", []string{"127.0.0.1", "::1/128", "10.10.0.0/16"}},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("TRUSTED_PROXIES", tc.value)
			if got := Load().TrustedProxies; !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("trusted proxies = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestValidateTrustedProxies(t *testing.T) {
	for _, tc := range []struct {
		proxy string
		valid bool
	}{
		{"127.0.0.1", true}, {"::1/128", true}, {"10.1.0.0/16", true},
		{"proxy.example.com", false}, {"10.1.0.0/99", false}, {"127.0.0.1:8080", false},
	} {
		t.Run(tc.proxy, func(t *testing.T) {
			cfg := Config{TrustedProxies: []string{tc.proxy}}
			invalid := false
			for _, err := range cfg.Validate() {
				invalid = invalid || strings.Contains(err.Error(), "TRUSTED_PROXIES")
			}
			if invalid == tc.valid {
				t.Fatalf("proxy %q valid=%v, want %v", tc.proxy, !invalid, tc.valid)
			}
		})
	}
}
