package copilot

import "testing"

func TestAPIEndpointFromToken(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{name: "empty", token: "", want: DefaultAPIEndpoint},
		{name: "missing proxy endpoint", token: "token", want: DefaultAPIEndpoint},
		{name: "host", token: "abc;proxy-ep=individual.githubcopilot.com;def", want: "https://api.individual.githubcopilot.com"},
		{name: "api host", token: "abc;proxy-ep=api.enterprise.githubcopilot.com", want: "https://api.enterprise.githubcopilot.com"},
		{name: "escaped", token: "abc;proxy-ep=individual.githubcopilot.com%2F", want: "https://api.individual.githubcopilot.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := APIEndpointFromToken(tt.token); got != tt.want {
				t.Fatalf("APIEndpointFromToken() = %q, want %q", got, tt.want)
			}
		})
	}
}
