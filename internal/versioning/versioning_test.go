package versioning

import (
	"testing"

	"github.com/dangduoc08/ginject/internal/ctx"
	"github.com/dangduoc08/ginject/internal/test"
)

func TestVersion_Query(t *testing.T) {
	v := &Options{Type: TypeQuery, Key: "version", DefaultVersion: "v1"}

	got := Version(v, makeCtxWithQuery("version", "v2"))
	if got != "v2" {
		t.Error(test.DiffMessage(got, "v2", "QueryVersion: key present"))
	}

	got = Version(v, makeCtxEmpty())
	if got != "v1" {
		t.Error(test.DiffMessage(got, "v1", "QueryVersion: key absent uses default"))
	}
}

func TestVersion_QueryDefaultKey(t *testing.T) {
	v := &Options{Type: TypeQuery, Key: "v", DefaultVersion: "v3"}

	got := Version(v, makeCtxWithQuery("v", "v5"))
	if got != "v5" {
		t.Error(test.DiffMessage(got, "v5", "QueryVersion: default key 'v'"))
	}
}

func TestVersion_Header(t *testing.T) {
	v := &Options{Type: TypeHeader, Key: "X-Api-Version", DefaultVersion: "v1"}

	got := Version(v, makeCtxWithHeader("X-Api-Version", "v2"))
	if got != "v2" {
		t.Error(test.DiffMessage(got, "v2", "HeaderVersion: key present"))
	}

	got = Version(v, makeCtxEmpty())
	if got != "v1" {
		t.Error(test.DiffMessage(got, "v1", "HeaderVersion: key absent uses default"))
	}
}

func TestVersion_QueryAbsentUsesDefault(t *testing.T) {
	v := &Options{Type: TypeQuery, Key: "version", DefaultVersion: "v1"}

	got := Version(v, makeCtxWithQuery("version", ""))
	if got != "v1" {
		t.Error(test.DiffMessage(got, "v1", "V1: empty value uses default"))
	}
	got = Version(v, makeCtxWithQuery("other", "v2"))
	if got != "v1" {
		t.Error(test.DiffMessage(got, "v1", "V1: other key uses default"))
	}
}

func TestVersion_HeaderKeyCaseInsensitive(t *testing.T) {
	v := &Options{Type: TypeHeader, Key: "x-api-version", DefaultVersion: "v1"}

	got := Version(v, makeCtxWithHeader("X-Api-Version", "v2"))
	if got != "v2" {
		t.Error(test.DiffMessage(got, "v2", "V2: header key in any case"))
	}
	got = Version(v, makeCtxWithHeader("X-Api-Version", ""))
	if got != "v1" {
		t.Error(test.DiffMessage(got, "v1", "V2: empty header uses default"))
	}
}

func TestVersion_Custom(t *testing.T) {
	v := &Options{
		Type:           TypeCustom,
		DefaultVersion: "v1",
		Extractor:      func(c *ctx.HTTPContext) string { return "v9" },
	}

	got := Version(v, makeCtxEmpty())
	if got != "v9" {
		t.Error(test.DiffMessage(got, "v9", "CustomVersion: extractor called"))
	}
}

func TestVersion_CustomEmptyUsesDefault(t *testing.T) {
	v := &Options{
		Type:           TypeCustom,
		DefaultVersion: "v1",
		Extractor:      func(c *ctx.HTTPContext) string { return "" },
	}

	got := Version(v, makeCtxEmpty())
	if got != "v1" {
		t.Error(test.DiffMessage(got, "v1", "V4: an empty result uses default"))
	}
}

func TestVersion_CustomNilExtractor(t *testing.T) {
	v := &Options{Type: TypeCustom, DefaultVersion: "v1"}

	got := Version(v, makeCtxEmpty())
	if got != "v1" {
		t.Error(test.DiffMessage(got, "v1", "V4: nil extractor uses default"))
	}
}

func TestVersion_MediaType(t *testing.T) {
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

	v := &Options{Type: TypeMediaType, Key: "v", DefaultVersion: "v1"}
	if got := Version(v, makeCtxEmpty()); got != "v1" {
		t.Error(test.DiffMessage(got, "v1", "V3: no Accept header uses default"))
	}
}

func TestVersion_MediaTypeIgnoresOtherParams(t *testing.T) {
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

func TestVersion_MediaTypeStopsAtComma(t *testing.T) {
	if got := acceptVersion("application/json;v=2, text/html", "v"); got != "2" {
		t.Error(test.DiffMessage(got, "2", "V3: the value ends at the comma"))
	}
	if got := acceptVersion("application/json,v=2", "v"); got != "dflt" {
		t.Error(test.DiffMessage(got, "dflt", "V3: a media type is not a parameter"))
	}
}

func TestVersion_MediaTypeUnquotes(t *testing.T) {
	if got := acceptVersion(`application/json; v="2"`, "v"); got != "2" {
		t.Error(test.DiffMessage(got, "2", "V3: quotes are removed"))
	}
}

func TestVersion_MediaTypeNameCaseInsensitive(t *testing.T) {
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

func TestVersion_MediaTypeSecondRange(t *testing.T) {
	if got := acceptVersion("text/html, application/json;v=3", "v"); got != "3" {
		t.Error(test.DiffMessage(got, "3", "V3: version on the second range"))
	}
	if got := acceptVersion("a/b;v=1, c/d;v=2", "v"); got != "1" {
		t.Error(test.DiffMessage(got, "1", "V3: the first parameter, left to right, wins"))
	}
}

func TestVersion_MediaTypeEmptyValueUsesDefault(t *testing.T) {
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

func TestVersion_UnknownType(t *testing.T) {
	for _, typ := range []Type{0, 99, -1} {
		v := &Options{Type: typ, Key: "v", DefaultVersion: "v1"}
		got := Version(v, makeCtxWithQuery("v", "v2"))
		if got != "v1" {
			t.Error(test.DiffMessage(got, "v1", "V5: unknown type uses default"))
		}
	}
}

func TestTypeString(t *testing.T) {
	cases := map[Type]string{
		TypeQuery:     "query",
		TypeHeader:    "header",
		TypeCustom:    "custom",
		TypeMediaType: "media_type",
		0:             "",
		99:            "",
	}
	for typ, want := range cases {
		v := &Options{Type: typ}
		if got := TypeString(v); got != want {
			t.Error(test.DiffMessage(got, want, "V5: TypeString"))
		}
	}
}

func TestVersion_NoAlloc(t *testing.T) {
	header := &Options{Type: TypeHeader, Key: "X-Api-Version", DefaultVersion: "v1"}
	media := &Options{Type: TypeMediaType, Key: "v", DefaultVersion: "v1"}
	custom := &Options{Type: TypeCustom, DefaultVersion: "v1", Extractor: func(c *ctx.HTTPContext) string { return "v9" }}
	query := &Options{Type: TypeQuery, Key: "version", DefaultVersion: "v1"}

	headerCtx := makeCtxWithHeader("X-Api-Version", "v2")
	mediaCtx := makeCtxWithHeader("Accept", `text/html, application/json; charset=utf-8; V = "2"`)
	customCtx := makeCtxEmpty()
	queryCtx := makeCtxWithQuery("version", "v2")
	Version(query, queryCtx)

	cases := []struct {
		name string
		f    func()
	}{
		{"header", func() { Version(header, headerCtx) }},
		{"media type", func() { Version(media, mediaCtx) }},
		{"custom", func() { Version(custom, customCtx) }},
		{"query after the first parse", func() { Version(query, queryCtx) }},
	}
	for _, c := range cases {
		if allocs := testing.AllocsPerRun(100, c.f); allocs != 0 {
			t.Error(test.DiffMessage(allocs, 0.0, "V6: allocations of "+c.name))
		}
	}
}

func TestVersion_Neutral(t *testing.T) {
	v := &Options{Type: TypeQuery, Key: "v", DefaultVersion: NeutralVersion}

	got := Version(v, makeCtxEmpty())
	if got != NeutralVersion {
		t.Error(test.DiffMessage(got, NeutralVersion, "default version can be NEUTRAL"))
	}
}
