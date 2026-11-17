package versioning

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

func makeCtxWithQuery(key, val string) *ctx.HTTPContext {
	c := ctx.NewHTTPContext()
	u := &url.URL{RawQuery: url.Values{key: {val}}.Encode()}
	c.Request = &http.Request{URL: u}
	return c
}

func makeCtxWithHeader(key, val string) *ctx.HTTPContext {
	c := ctx.NewHTTPContext()
	h := http.Header{key: {val}}
	c.Request = &http.Request{Header: h}
	return c
}

func makeCtxEmpty() *ctx.HTTPContext {
	c := ctx.NewHTTPContext()
	u := &url.URL{RawQuery: ""}
	c.Request = &http.Request{URL: u, Header: http.Header{}}
	return c
}

func TestGetVersion_Query(t *testing.T) {
	v := &Versioning{Type: QueryVersion, Key: "version", DefaultVersion: "v1"}

	got := v.GetVersion(makeCtxWithQuery("version", "v2"))
	if got != "v2" {
		t.Error(test.DiffMessage(got, "v2", "QueryVersion: key present"))
	}

	got = v.GetVersion(makeCtxEmpty())
	if got != "v1" {
		t.Error(test.DiffMessage(got, "v1", "QueryVersion: key absent uses default"))
	}
}

func TestGetVersion_QueryDefaultKey(t *testing.T) {
	v := &Versioning{Type: QueryVersion, Key: "v", DefaultVersion: "v3"}

	got := v.GetVersion(makeCtxWithQuery("v", "v5"))
	if got != "v5" {
		t.Error(test.DiffMessage(got, "v5", "QueryVersion: default key 'v'"))
	}
}

func TestGetVersion_Header(t *testing.T) {
	v := &Versioning{Type: HeaderVersion, Key: "X-Api-Version", DefaultVersion: "v1"}

	got := v.GetVersion(makeCtxWithHeader("X-Api-Version", "v2"))
	if got != "v2" {
		t.Error(test.DiffMessage(got, "v2", "HeaderVersion: key present"))
	}

	got = v.GetVersion(makeCtxEmpty())
	if got != "v1" {
		t.Error(test.DiffMessage(got, "v1", "HeaderVersion: key absent uses default"))
	}
}

func TestGetVersion_QueryAbsentUsesDefault(t *testing.T) {
	v := &Versioning{Type: QueryVersion, Key: "version", DefaultVersion: "v1"}

	got := v.GetVersion(makeCtxWithQuery("version", ""))
	if got != "v1" {
		t.Error(test.DiffMessage(got, "v1", "V1: empty value uses default"))
	}
	got = v.GetVersion(makeCtxWithQuery("other", "v2"))
	if got != "v1" {
		t.Error(test.DiffMessage(got, "v1", "V1: other key uses default"))
	}
}

func TestGetVersion_HeaderKeyCaseInsensitive(t *testing.T) {
	v := &Versioning{Type: HeaderVersion, Key: "x-api-version", DefaultVersion: "v1"}

	got := v.GetVersion(makeCtxWithHeader("X-Api-Version", "v2"))
	if got != "v2" {
		t.Error(test.DiffMessage(got, "v2", "V2: header key in any case"))
	}
	got = v.GetVersion(makeCtxWithHeader("X-Api-Version", ""))
	if got != "v1" {
		t.Error(test.DiffMessage(got, "v1", "V2: empty header uses default"))
	}
}

func TestGetVersion_Custom(t *testing.T) {
	v := &Versioning{
		Type:           CustomVersion,
		DefaultVersion: "v1",
		Extractor:      func(c *ctx.HTTPContext) string { return "v9" },
	}

	got := v.GetVersion(makeCtxEmpty())
	if got != "v9" {
		t.Error(test.DiffMessage(got, "v9", "CustomVersion: extractor called"))
	}
}

func TestGetVersion_CustomEmptyUsesDefault(t *testing.T) {
	v := &Versioning{
		Type:           CustomVersion,
		DefaultVersion: "v1",
		Extractor:      func(c *ctx.HTTPContext) string { return "" },
	}

	got := v.GetVersion(makeCtxEmpty())
	if got != "v1" {
		t.Error(test.DiffMessage(got, "v1", "V4: an empty result uses default"))
	}
}

func TestGetVersion_CustomNilExtractor(t *testing.T) {
	v := &Versioning{Type: CustomVersion, DefaultVersion: "v1"}

	got := v.GetVersion(makeCtxEmpty())
	if got != "v1" {
		t.Error(test.DiffMessage(got, "v1", "V4: nil extractor uses default"))
	}
}

func acceptVersion(accept, key string) string {
	v := &Versioning{Type: MediaType, Key: key, DefaultVersion: "dflt"}
	return v.GetVersion(makeCtxWithHeader("Accept", accept))
}

func TestGetVersion_MediaType(t *testing.T) {
	cases := []struct{ accept, want string }{
		{"application/json;v=2", "2"},
		{"application/json;v=2;q=0.9", "2"},
		{"application/json", "dflt"},
		{"", "dflt"},
	}
	for _, c := range cases {
		if got := acceptVersion(c.accept, "v"); got != c.want {
			t.Error(test.DiffMessage(got, c.want, "V3: Accept "+c.accept))
		}
	}

	v := &Versioning{Type: MediaType, Key: "v", DefaultVersion: "v1"}
	if got := v.GetVersion(makeCtxEmpty()); got != "v1" {
		t.Error(test.DiffMessage(got, "v1", "V3: no Accept header uses default"))
	}
}

func TestGetVersion_MediaTypeIgnoresOtherParams(t *testing.T) {
	cases := []string{
		"application/json;rev=7",
		"application/json;q=0.9;charset=utf-8",
		"application/json;xv=2",
		"application/json;vv=2",
	}
	for _, accept := range cases {
		if got := acceptVersion(accept, "v"); got != "dflt" {
			t.Error(test.DiffMessage(got, "dflt", "V3: Accept "+accept))
		}
	}
	if got := acceptVersion("application/json;charset=utf-8; v = 4", "v"); got != "4" {
		t.Error(test.DiffMessage(got, "4", "V3: other params before the version"))
	}
}

func TestGetVersion_MediaTypeStopsAtComma(t *testing.T) {
	if got := acceptVersion("application/json;v=2, text/html", "v"); got != "2" {
		t.Error(test.DiffMessage(got, "2", "V3: the value ends at the comma"))
	}
	if got := acceptVersion("application/json,v=2", "v"); got != "dflt" {
		t.Error(test.DiffMessage(got, "dflt", "V3: a media type is not a parameter"))
	}
}

func TestGetVersion_MediaTypeUnquotes(t *testing.T) {
	if got := acceptVersion(`application/json; v="2"`, "v"); got != "2" {
		t.Error(test.DiffMessage(got, "2", "V3: quotes are removed"))
	}
}

func TestGetVersion_MediaTypeNameCaseInsensitive(t *testing.T) {
	if got := acceptVersion("text/html, application/json;V=3", "v"); got != "3" {
		t.Error(test.DiffMessage(got, "3", "V3: name in upper case"))
	}
	if got := acceptVersion("application/json;v=3", "V"); got != "3" {
		t.Error(test.DiffMessage(got, "3", "V3: Key in upper case"))
	}
	if got := acceptVersion("application/json; v = 4", "v"); got != "4" {
		t.Error(test.DiffMessage(got, "4", "V3: spaces around name and value"))
	}
}

func TestGetVersion_MediaTypeSecondRange(t *testing.T) {
	if got := acceptVersion("text/html, application/json;v=3", "v"); got != "3" {
		t.Error(test.DiffMessage(got, "3", "V3: version on the second range"))
	}
	if got := acceptVersion("a/b;v=1, c/d;v=2", "v"); got != "1" {
		t.Error(test.DiffMessage(got, "1", "V3: the first parameter, left to right, wins"))
	}
}

func TestGetVersion_MediaTypeEmptyValueUsesDefault(t *testing.T) {
	if got := acceptVersion("application/json;v=", "v"); got != "dflt" {
		t.Error(test.DiffMessage(got, "dflt", "V3: empty value"))
	}
	if got := acceptVersion(`application/json;v=""`, "v"); got != "dflt" {
		t.Error(test.DiffMessage(got, "dflt", "V3: empty quoted value"))
	}
	if got := acceptVersion("application/json;v=;v=3", "v"); got != "3" {
		t.Error(test.DiffMessage(got, "3", "V3: the first parameter with a value wins"))
	}
}

func TestGetVersion_UnknownType(t *testing.T) {
	for _, typ := range []int{0, 99, -1} {
		v := &Versioning{Type: typ, Key: "v", DefaultVersion: "v1"}
		got := v.GetVersion(makeCtxWithQuery("v", "v2"))
		if got != "v1" {
			t.Error(test.DiffMessage(got, "v1", "V5: unknown type uses default"))
		}
	}
}

func TestGetTypeString(t *testing.T) {
	cases := map[int]string{
		QueryVersion:  "query",
		HeaderVersion: "header",
		CustomVersion: "custom",
		MediaType:     "media_type",
		0:             "",
		99:            "",
	}
	for typ, want := range cases {
		v := &Versioning{Type: typ}
		if got := v.GetTypeString(); got != want {
			t.Error(test.DiffMessage(got, want, "V5: GetTypeString"))
		}
	}
}

func TestGetVersion_NoAlloc(t *testing.T) {
	header := &Versioning{Type: HeaderVersion, Key: "X-Api-Version", DefaultVersion: "v1"}
	media := &Versioning{Type: MediaType, Key: "v", DefaultVersion: "v1"}
	custom := &Versioning{Type: CustomVersion, DefaultVersion: "v1", Extractor: func(c *ctx.HTTPContext) string { return "v9" }}
	query := &Versioning{Type: QueryVersion, Key: "version", DefaultVersion: "v1"}

	headerCtx := makeCtxWithHeader("X-Api-Version", "v2")
	mediaCtx := makeCtxWithHeader("Accept", `text/html, application/json; charset=utf-8; V = "2"`)
	customCtx := makeCtxEmpty()
	queryCtx := makeCtxWithQuery("version", "v2")
	query.GetVersion(queryCtx)

	cases := []struct {
		name string
		f    func()
	}{
		{"header", func() { header.GetVersion(headerCtx) }},
		{"media type", func() { media.GetVersion(mediaCtx) }},
		{"custom", func() { custom.GetVersion(customCtx) }},
		{"query after the first parse", func() { query.GetVersion(queryCtx) }},
	}
	for _, c := range cases {
		if allocs := testing.AllocsPerRun(100, c.f); allocs != 0 {
			t.Error(test.DiffMessage(allocs, 0.0, "V6: allocations of "+c.name))
		}
	}
}

func TestGetVersion_Neutral(t *testing.T) {
	v := &Versioning{Type: QueryVersion, Key: "v", DefaultVersion: NeutralVersion}

	got := v.GetVersion(makeCtxEmpty())
	if got != NeutralVersion {
		t.Error(test.DiffMessage(got, NeutralVersion, "default version can be NEUTRAL"))
	}
}
