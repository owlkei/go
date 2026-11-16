package tracecontext

import (
	"strings"
	"testing"
	"unsafe"

	"github.com/dangduoc08/ginject/internal/test"
)

func expectRejected(t *testing.T, value, desc string) {
	t.Helper()
	traceID, parentID, flags, ok := ParseTraceparent(value)
	if ok || traceID != "" || parentID != "" || flags != 0 {
		t.Error(test.DiffMessage([]any{traceID, parentID, flags, ok}, []any{"", "", byte(0), false}, desc))
	}
}

func TestParseTraceparent_Valid(t *testing.T) {
	traceID, parentID, flags, ok := ParseTraceparent(validHeader)
	if !ok {
		t.Fatal(test.DiffMessage(ok, true, "P1: the W3C example is accepted"))
	}
	if traceID != validTraceID {
		t.Error(test.DiffMessage(traceID, validTraceID, "P1: trace ID"))
	}
	if parentID != validParentID {
		t.Error(test.DiffMessage(parentID, validParentID, "P1: parent ID"))
	}
	if flags != 1 {
		t.Error(test.DiffMessage(flags, byte(1), "P1: flags"))
	}
}

func TestParseTraceparent_FlagsByte(t *testing.T) {
	for _, tc := range []struct {
		text string
		want byte
	}{{"00", 0x00}, {"01", 0x01}, {"02", 0x02}, {"03", 0x03}, {"a5", 0xa5}, {"ff", 0xff}, {"0f", 0x0f}} {
		_, _, flags, ok := ParseTraceparent("00-" + validTraceID + "-" + validParentID + "-" + tc.text)
		if !ok || flags != tc.want {
			t.Error(test.DiffMessage([]any{flags, ok}, []any{tc.want, true}, "P1: flags "+tc.text+" are returned as a byte"))
		}
	}
}

func TestParseTraceparent_Version00WrongLength(t *testing.T) {
	expectRejected(t, validHeader+"-", "P2: version 00 with a trailing dash is refused")
	expectRejected(t, validHeader+"-extra", "P2: version 00 with a suffix is refused")
	expectRejected(t, validHeader+"0", "P2: version 00 with one more byte is refused")
	expectRejected(t, validHeader[:54], "P2: version 00 one byte short is refused")
}

func TestParseTraceparent_FutureVersionWithSuffix(t *testing.T) {
	value := "01-" + validTraceID + "-" + validParentID + "-01-future-field"
	traceID, parentID, flags, ok := ParseTraceparent(value)
	if !ok || traceID != validTraceID || parentID != validParentID || flags != 1 {
		t.Error(test.DiffMessage([]any{traceID, parentID, flags, ok}, []any{validTraceID, validParentID, byte(1), true}, "P2: a future version with a dash-led suffix reads the first 55 bytes"))
	}
	if _, _, _, ok := ParseTraceparent("cc-" + validTraceID + "-" + validParentID + "-01"); !ok {
		t.Error(test.DiffMessage(ok, true, "P2: a future version of exactly 55 bytes is accepted"))
	}
	if _, _, _, ok := ParseTraceparent("0a-" + validTraceID + "-" + validParentID + "-01-"); !ok {
		t.Error(test.DiffMessage(ok, true, "P2: a future version followed by a lone dash is accepted"))
	}
}

func TestParseTraceparent_FutureVersionWithoutDash(t *testing.T) {
	expectRejected(t, "01-"+validTraceID+"-"+validParentID+"-01x", "P2: byte 55 of a future version must be a dash")
	expectRejected(t, "01-"+validTraceID+"-"+validParentID+"-011", "P2: a three-digit flags field is refused")
}

func TestParseTraceparent_Empty(t *testing.T) {
	expectRejected(t, "", "P3: an empty string")
}

func TestParseTraceparent_Short(t *testing.T) {
	expectRejected(t, "00", "P3: two bytes")
	expectRejected(t, "00-"+validTraceID, "P3: no parent ID")
	expectRejected(t, validHeader[:len(validHeader)-1], "P3: one byte short")
}

func TestParseTraceparent_UpperCase(t *testing.T) {
	expectRejected(t, "00-"+strings.ToUpper(validTraceID)+"-"+validParentID+"-01", "P3: upper-case trace ID")
	expectRejected(t, "00-"+validTraceID+"-"+strings.ToUpper(validParentID)+"-01", "P3: upper-case parent ID")
	expectRejected(t, "00-"+validTraceID+"-"+validParentID+"-0A", "P3: upper-case flags")
	expectRejected(t, "0A-"+validTraceID+"-"+validParentID+"-01-x", "P3: upper-case version")
}

func TestParseTraceparent_ZeroTraceID(t *testing.T) {
	expectRejected(t, "00-"+strings.Repeat("0", 32)+"-"+validParentID+"-01", "P3: an all-zero trace ID")
}

func TestParseTraceparent_ZeroParentID(t *testing.T) {
	expectRejected(t, "00-"+validTraceID+"-"+strings.Repeat("0", 16)+"-01", "P3: an all-zero parent ID")
}

func TestParseTraceparent_VersionFF(t *testing.T) {
	expectRejected(t, "ff-"+validTraceID+"-"+validParentID+"-01", "P3: version ff")
	expectRejected(t, "ff-"+validTraceID+"-"+validParentID+"-01-x", "P3: version ff with a suffix")
}

func TestParseTraceparent_NonHex(t *testing.T) {
	for _, pos := range []int{0, 1, 3, 34, 36, 51, 53, 54} {
		b := []byte(validHeader)
		b[pos] = 'g'
		expectRejected(t, string(b), "P3: a non-hex byte at index "+string(rune('0'+pos/10))+string(rune('0'+pos%10)))
	}
}

func TestParseTraceparent_WrongDashes(t *testing.T) {
	for _, pos := range []int{2, 35, 52} {
		for _, replacement := range []byte{'_', ' ', '0', '+'} {
			b := []byte(validHeader)
			b[pos] = replacement
			expectRejected(t, string(b), "P3: the byte at index "+string(rune('0'+pos/10))+string(rune('0'+pos%10))+" must be a dash")
		}
	}
}

func TestParseTraceparent_Whitespace(t *testing.T) {
	expectRejected(t, " "+validHeader, "P3: leading space")
	expectRejected(t, validHeader+" ", "P3: trailing space")
	expectRejected(t, "\t"+validHeader, "P3: leading tab")
	expectRejected(t, validHeader+"\t", "P3: trailing tab")
	expectRejected(t, "00-"+validTraceID[:10]+" "+validTraceID[11:]+"-"+validParentID+"-01", "P3: a space inside the trace ID")
	expectRejected(t, "00-"+validTraceID+"-"+validParentID[:5]+"\t"+validParentID[6:]+"-01", "P3: a tab inside the parent ID")
	expectRejected(t, "01-"+validTraceID+"-"+validParentID+"-01 -x", "P3: a space before the suffix of a future version")
}

func TestParseTraceparent_ControlCharacters(t *testing.T) {
	expectRejected(t, validHeader+"\r\nX-Injected: yes", "P3: CRLF header injection")
	expectRejected(t, validHeader+"\n", "P3: trailing newline")
	expectRejected(t, "00-"+validTraceID[:10]+"\x00"+validTraceID[11:]+"-"+validParentID+"-01", "P3: NUL inside the trace ID")
	expectRejected(t, "01-"+validTraceID+"-"+validParentID+"-01\x00x", "P3: NUL where the suffix dash must be")
	expectRejected(t, "00-"+validTraceID+"-"+validParentID+"-\x1b[", "P3: escape bytes as flags")
}

func TestParseTraceparent_OneMegabyte(t *testing.T) {
	long := strings.Repeat("a", 1<<20)
	expectRejected(t, long, "P3: one megabyte of junk")
	expectRejected(t, validHeader+strings.Repeat("-", 1<<20), "P2: version 00 with a megabyte of suffix")
	expectRejected(t, "01-"+validTraceID+"-"+validParentID+"-01"+strings.Repeat("x", 1<<20), "P2: a future version whose byte 55 is not a dash")
	if got := testing.AllocsPerRun(20, func() { ParseTraceparent(long) }); got != 0 {
		t.Error(test.DiffMessage(got, 0.0, "P3: refusing a megabyte allocates nothing"))
	}
}

func TestParseTraceparent_FutureVersionOneMegabyteSuffixIsBounded(t *testing.T) {
	value := "01-" + validTraceID + "-" + validParentID + "-01-" + strings.Repeat("x", 1<<20)
	traceID, parentID, flags, ok := ParseTraceparent(value)
	if !ok || traceID != validTraceID || parentID != validParentID || flags != 1 {
		t.Error(test.DiffMessage([]any{traceID, parentID, flags, ok}, []any{validTraceID, validParentID, byte(1), true}, "P2: only the first 55 bytes of a long future value are read"))
	}
}

func within(s, container string) bool {
	if s == "" || container == "" {
		return false
	}
	start := uintptr(unsafe.Pointer(unsafe.StringData(container)))
	p := uintptr(unsafe.Pointer(unsafe.StringData(s)))
	return p >= start && p+uintptr(len(s)) <= start+uintptr(len(container))
}

func TestParseTraceparent_LongValueReturnsCopies(t *testing.T) {
	value := "01-" + validTraceID + "-" + validParentID + "-01-" + strings.Repeat("x", 4096)
	traceID, parentID, _, ok := ParseTraceparent(value)
	if !ok {
		t.Fatal(test.DiffMessage(ok, true, "P4: a long future value is accepted"))
	}
	if within(traceID, value) || within(parentID, value) {
		t.Error(test.DiffMessage("substrings of the header value", "copies", "P4: IDs from a value longer than 55 bytes must not keep it alive"))
	}
}

func TestParseTraceparent_ShortValueReturnsSubstrings(t *testing.T) {
	value := strings.Clone(validHeader)
	traceID, parentID, _, ok := ParseTraceparent(value)
	if !ok {
		t.Fatal(test.DiffMessage(ok, true, "P4: the W3C example is accepted"))
	}
	if !within(traceID, value) || !within(parentID, value) {
		t.Error(test.DiffMessage("copies", "substrings of the header value", "P4: a 55-byte value returns substrings, with no allocation"))
	}
	if got := testing.AllocsPerRun(100, func() { ParseTraceparent(value) }); got != 0 {
		t.Error(test.DiffMessage(got, 0.0, "P4: parsing a valid 55-byte value allocates nothing"))
	}
}

func FuzzParseTraceparent(f *testing.F) {
	for _, seed := range []string{
		"",
		validHeader,
		validHeader + "-",
		"01-" + validTraceID + "-" + validParentID + "-01-more",
		"ff-" + validTraceID + "-" + validParentID + "-01",
		"00-" + strings.Repeat("0", 32) + "-" + validParentID + "-01",
		"00-" + strings.ToUpper(validTraceID) + "-" + validParentID + "-01",
		validHeader + "\r\nX: y",
		strings.Repeat("-", 56),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		traceID, parentID, flags, ok := ParseTraceparent(value)
		if !ok {
			if traceID != "" || parentID != "" || flags != 0 {
				t.Fatalf("P3: a refused value returned %q %q %d", traceID, parentID, flags)
			}
			return
		}
		if len(value) < 55 || value[2] != '-' || value[35] != '-' || value[52] != '-' {
			t.Fatalf("P1: accepted %q without the dashes of the grammar", value)
		}
		version := value[:2]
		if !isLowerHex(version) || version == "ff" {
			t.Fatalf("P1: accepted version %q", version)
		}
		if version == "00" && len(value) != 55 {
			t.Fatalf("P2: accepted a version 00 value of %d bytes", len(value))
		}
		if len(value) > 55 && value[55] != '-' {
			t.Fatalf("P2: accepted %q with byte 55 not a dash", value)
		}
		if traceID != value[3:35] || !isLowerHex(traceID) || traceID == strings.Repeat("0", 32) {
			t.Fatalf("P1: trace ID %q from %q", traceID, value)
		}
		if parentID != value[36:52] || !isLowerHex(parentID) || parentID == strings.Repeat("0", 16) {
			t.Fatalf("P1: parent ID %q from %q", parentID, value)
		}
		flagText := value[53:55]
		if !isLowerHex(flagText) {
			t.Fatalf("P1: flags %q are not lower-case hex", flagText)
		}
		want := byte(0)
		for i := 0; i < 2; i++ {
			c := flagText[i]
			digit := c - '0'
			if c >= 'a' {
				digit = c - 'a' + 10
			}
			want = want<<4 | digit
		}
		if flags != want {
			t.Fatalf("P1: flags %d, want %d", flags, want)
		}
	})
}
