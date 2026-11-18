package common

import (
	"testing"

	"github.com/dangduoc08/ginject/internal/test"
)

func TestParseFuncNameToURL_ServeTrailingAnyIsSegment(t *testing.T) {
	cases := []struct{ fn, wantRoute string }{
		{"SERVE_static_ANY", "/static/*/"},
		{"SERVE_a_b_ANY", "/a-b/*/"},
		{"SERVE_assets_ANY_OF_public", "/public/assets/*/"},
	}
	for _, c := range cases {
		method, route, version := ParseFuncNameToURL(c.fn, PathStyleKebab)
		if method != "SERVE" {
			t.Error(test.DiffMessage(method, "SERVE", "V1: "+c.fn+" method"))
		}
		if route != c.wantRoute {
			t.Error(test.DiffMessage(route, c.wantRoute, "V1: "+c.fn+" route"))
		}
		if version != "" {
			t.Error(test.DiffMessage(version, "", "V1: "+c.fn+" version"))
		}
	}
}

func TestParseFuncNameToURL_ServeAnyBetweenNamesIsGlob(t *testing.T) {
	cases := []struct{ fn, wantRoute string }{
		{"SERVE_ANY", "/*/"},
		{"SERVE_ANY_OF_static", "/static/*/"},
		{"SERVE_me_ANY_bers", "/me*bers/"},
	}
	for _, c := range cases {
		_, route, _ := ParseFuncNameToURL(c.fn, PathStyleKebab)
		if route != c.wantRoute {
			t.Error(test.DiffMessage(route, c.wantRoute, "V2: "+c.fn+" route"))
		}
	}
}

func TestParseFuncNameToURL_ReadTrailingAnyStaysGlob(t *testing.T) {
	cases := []struct{ fn, wantRoute string }{
		{"READ_assets_ANY", "/assets*/"},
		{"CREATE_assets_ANY", "/assets*/"},
		{"READ_a_b_ANY", "/a-b*/"},
	}
	for _, c := range cases {
		_, route, _ := ParseFuncNameToURL(c.fn, PathStyleKebab)
		if route != c.wantRoute {
			t.Error(test.DiffMessage(route, c.wantRoute, "V2: "+c.fn+" route"))
		}
	}
}
