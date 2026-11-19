package str

import (
	"math/rand/v2"
	"strings"
	"testing"

	"go.owlky.com/owlky/internal/test"
)

func TestEnclose_Empty(t *testing.T) {
	for _, in := range []string{"", " ", " \t\n\v\f\r "} {
		got := Enclose(in, '/')
		if got != "" {
			t.Error(test.DiffMessage(got, "", "S1: Enclose of empty or white space only"))
		}
	}
}

func TestEnclose_AddsSeparators(t *testing.T) {
	cases := []struct {
		in   string
		sep  byte
		want string
	}{
		{"users", '/', "/users/"},
		{"/users", '/', "/users/"},
		{"users/", '/', "/users/"},
		{"/users/", '/', "/users/"},
		{"/", '/', "/"},
		{"*", '/', "/*/"},
		{"foo", '.', ".foo."},
		{"foo.bar", '.', ".foo.bar."},
	}
	for _, c := range cases {
		got := Enclose(c.in, c.sep)
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, "S1: Enclose("+c.in+")"))
		}
	}
}

func TestEnclose_CollapsesSeparators(t *testing.T) {
	cases := []struct {
		in   string
		sep  byte
		want string
	}{
		{"//users//", '/', "/users/"},
		{"/a//b///c/", '/', "/a/b/c/"},
		{"foo..bar", '.', ".foo.bar."},
		{"//", '/', "/"},
	}
	for _, c := range cases {
		got := Enclose(c.in, c.sep)
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, "S1: Enclose("+c.in+")"))
		}
	}
}

func TestEnclose_CollapsesStars(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/a/**/b/", "/a/*/b/"},
		{"/a/***/b/", "/a/*/b/"},
		{"**", "/*/"},
	}
	for _, c := range cases {
		got := Enclose(c.in, '/')
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, "S1: Enclose("+c.in+")"))
		}
	}
}

func TestEnclose_StripsASCIISpace(t *testing.T) {
	cases := []struct{ in, want string }{
		{" /users/ ", "/users/"},
		{"/us ers/", "/users/"},
		{"\t/a\n/\vb\f/\r", "/a/b/"},
		{"/ / /", "/"},
	}
	for _, c := range cases {
		got := Enclose(c.in, '/')
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, "S1: Enclose("+c.in+")"))
		}
	}
}

func TestEnclose_KeepsUTF8(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/é/", "/é/"},
		{"é", "/é/"},
		{"/日本//語/", "/日本/語/"},
		{"/a\xffb/", "/a\xffb/"},
	}
	for _, c := range cases {
		got := Enclose(c.in, '/')
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, "S1: Enclose("+c.in+")"))
		}
	}
}

func TestEnclose_CanonicalNoAlloc(t *testing.T) {
	in := "/users/all/"
	got := Enclose(in, '/')
	if got != in {
		t.Error(test.DiffMessage(got, in, "S2: canonical input is returned as is"))
	}
	allocs := testing.AllocsPerRun(100, func() {
		strSink = Enclose(in, '/')
	})
	if allocs != 0 {
		t.Error(test.DiffMessage(allocs, 0.0, "S2: allocations of Enclose on a canonical input"))
	}
}

func TestEnclose_FastEqualsSlow(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	alphabet := []string{"a", "b", "/", "*", " ", "\t", "é"}
	for _, sep := range []byte{'/', '.'} {
		for i := 0; i < 10000; i++ {
			var b strings.Builder
			n := r.IntN(12)
			for j := 0; j < n; j++ {
				b.WriteString(alphabet[r.IntN(len(alphabet))])
			}
			s := b.String()
			if sep == '.' {
				s = strings.ReplaceAll(s, "/", ".")
			}
			got := Enclose(s, sep)
			want := encloseSlow(s, sep)
			if got != want {
				t.Fatal(test.DiffMessage(got, want, "S3: Enclose("+s+") equals encloseSlow"))
			}
		}
	}
}

func TestRemoveDup_CollapsesRuns(t *testing.T) {
	got := RemoveDup("/**/school**/***/***/{subjectId}/***", '*')
	want := "/*/school*/*/*/{subjectId}/*"
	if got != want {
		t.Error(test.DiffMessage(got, want, "S4: RemoveDup of stars"))
	}
	got = RemoveDup("//a///b////", '/')
	want = "/a/b/"
	if got != want {
		t.Error(test.DiffMessage(got, want, "S4: RemoveDup of slashes"))
	}
	got = RemoveDup("aabbcc", 'b')
	want = "aabcc"
	if got != want {
		t.Error(test.DiffMessage(got, want, "S4: other bytes keep their runs"))
	}
}

func TestRemoveDup_KeepsInvalidUTF8(t *testing.T) {
	got := RemoveDup("a\xff//b", '/')
	want := "a\xff/b"
	if got != want {
		t.Error(test.DiffMessage(got, want, "S4: invalid byte survives"))
	}
	got = RemoveDup("\xff\xff//\xc3", '/')
	want = "\xff\xff/\xc3"
	if got != want {
		t.Error(test.DiffMessage(got, want, "S4: several invalid bytes survive"))
	}
}

func TestRemoveDup_Empty(t *testing.T) {
	got := RemoveDup("", '/')
	if got != "" {
		t.Error(test.DiffMessage(got, "", "S4: RemoveDup of empty"))
	}
}

func TestRemoveDup_NoRunNoAlloc(t *testing.T) {
	in := "/a/b/c"
	got := RemoveDup(in, '/')
	if got != in {
		t.Error(test.DiffMessage(got, in, "S5: no run returns the input"))
	}
	allocs := testing.AllocsPerRun(100, func() {
		strSink = RemoveDup(in, '/')
	})
	if allocs != 0 {
		t.Error(test.DiffMessage(allocs, 0.0, "S5: allocations of RemoveDup without a run"))
	}
}

func TestRemoveDup_FastEqualsSlow(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	alphabet := []string{"a", "/", "\xff", "*", "é"}
	for _, c := range []byte{'/', '*', 'a'} {
		for i := 0; i < 10000; i++ {
			var b strings.Builder
			n := r.IntN(12)
			for j := 0; j < n; j++ {
				b.WriteString(alphabet[r.IntN(len(alphabet))])
			}
			s := b.String()
			got := RemoveDup(s, c)
			want := removeDupSlow(s, c)
			if got != want {
				t.Fatal(test.DiffMessage(got, want, "S5: RemoveDup("+s+") equals removeDupSlow"))
			}
		}
	}
}

func TestRemoveSpace_RemovesUnicodeSpace(t *testing.T) {
	cases := []struct{ in, want string }{
		{"A B CDE", "ABCDE"},
		{"a b c\n", "abc"},
		{"a b c\u0085d", "abcd"},
		{"  ", ""},
		{"", ""},
	}
	for _, c := range cases {
		got := RemoveSpace(c.in)
		if got != c.want {
			t.Error(test.DiffMessage(got, c.want, "S6: RemoveSpace("+c.in+")"))
		}
	}
}

func TestRemoveSpace_NoSpaceUnchanged(t *testing.T) {
	in := "abc-é"
	got := RemoveSpace(in)
	if got != in {
		t.Error(test.DiffMessage(got, in, "S6: no space returns the input"))
	}
	allocs := testing.AllocsPerRun(100, func() {
		strSink = RemoveSpace(in)
	})
	if allocs != 0 {
		t.Error(test.DiffMessage(allocs, 0.0, "S6: allocations of RemoveSpace without space"))
	}
}
