package parser

import (
	"fmt"
	"testing"
)

func TestTopUrl(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{url: "http://localhost:9999", want: "/"},
		{url: "http://localhost:9999/", want: "/"},
		{url: "http://localhost:9999/test", want: "/test"},
		{url: "http://localhost:9999/test/", want: "/test"},
		{url: "http://localhost:9999/test/b", want: "/test"},
		{url: "/test", want: "/test"},
		{url: "/test/", want: "/test"},
		{url: "/test/b", want: "/test"},
		{url: "abc", want: "abc"},
		{url: "", want: "unknown"},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("Test-%d", i+1), func(t *testing.T) {
			if got := getTopUrl(tt.url); got != tt.want {
				t.Errorf("url: %s, want %s, got: %s", tt.url, tt.want, got)
			}
		})
	}
}
