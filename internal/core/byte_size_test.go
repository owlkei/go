package core

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"go.owlkei.com/owlkei/internal/test"
)

const byteSizeHint = "is not a size: write bytes or a number with KB, MB, GB (1000ⁿ) or KiB, MiB, GiB (1024ⁿ)"

func TestByteSize_Parse(t *testing.T) {
	cases := map[string]ByteSize{
		"0": 0, "1500": 1500, "10KB": 10000, "10MB": 10_000_000, "2GB": 2_000_000_000,
		"10KiB": 10240, "3MiB": 3 << 20, "1GiB": 1073741824, "-1": -1, "-10KB": -10000,
		"9223372036854775807": math.MaxInt64,
	}
	for text, want := range cases {
		got, err := parseByteSize(text)
		if err != nil || got != want {
			t.Error(test.DiffMessage([]any{got, err}, want, "B1: parse "+strconv.Quote(text)))
		}
	}
}

func TestByteSize_ParseRefuses(t *testing.T) {
	bad := []string{"", "10b", "10Mb", "10kb", "10B", "1.5MB", "10 MB", "MB", "0x10", "9223372036854775808", "99999999999GiB", "1TB", "-", "--1", "+1"}
	for _, text := range bad {
		_, err := parseByteSize(text)
		if err == nil {
			t.Error(test.DiffMessage(nil, "an error", "B2: refuse "+strconv.Quote(text)))
			continue
		}
		if !strings.HasPrefix(err.Error(), strconv.Quote(text)+" "+byteSizeHint) {
			t.Error(test.DiffMessage(err.Error(), strconv.Quote(text)+" "+byteSizeHint, "B2: error text for "+strconv.Quote(text)))
		}
	}
}

func TestByteSize_ParseBitsHint(t *testing.T) {
	for _, text := range []string{"10Mb", "10kb", "10Gb", "10Kib", "10b"} {
		_, err := parseByteSize(text)
		if err == nil || !strings.HasSuffix(err.Error(), "; a lower-case b means bits") {
			t.Error(test.DiffMessage(err, "…; a lower-case b means bits", "B2: bits hint for "+text))
		}
	}
	for _, text := range []string{"1.5MB", "10B", "MB", ""} {
		_, err := parseByteSize(text)
		if err != nil && strings.Contains(err.Error(), "bits") {
			t.Error(test.DiffMessage(err.Error(), "no bits hint", "B2: no hint for "+strconv.Quote(text)))
		}
	}
}

func TestByteSize_String(t *testing.T) {
	cases := map[ByteSize]string{
		0: "0", 1000: "1KB", 1024: "1KiB", 1500: "1500", 10 << 20: "10MiB", 1024000: "1000KiB",
		-1: "-1", 1_000_000: "1MB", 1_000_000_000: "1GB", 1 << 30: "1GiB", -1024: "-1KiB",
		2_000_000_000: "2GB", 1001: "1001",
	}
	for size, want := range cases {
		if got := size.String(); got != want {
			t.Error(test.DiffMessage(got, want, "B3: String of "+strconv.FormatInt(int64(size), 10)))
		}
	}
}

func TestByteSize_Constants(t *testing.T) {
	got := []ByteSize{KB, MB, GB, KiB, MiB, GiB}
	want := []ByteSize{1000, 1000 * 1000, 1000 * 1000 * 1000, 1024, 1024 * 1024, 1024 * 1024 * 1024}
	for i := range want {
		if got[i] != want[i] {
			t.Error(test.DiffMessage(got[i], want[i], "B1: unit constant "+strconv.Itoa(i)))
		}
	}
}

func TestByteSize_RoundTrip(t *testing.T) {
	for _, size := range []ByteSize{0, 1, -1, 999, 1000, 1024, 1500, 10 << 20, 1 << 40, math.MaxInt64, math.MinInt64 + 1, 1000 * 1024} {
		got, err := parseByteSize(size.String())
		if err != nil || got != size {
			t.Error(test.DiffMessage([]any{got, err}, size, "B4: round trip of "+size.String()))
		}
	}
}

func FuzzByteSize_RoundTrip(f *testing.F) {
	for _, seed := range []int64{0, 1, -1, 1000, 1024, 1 << 30, math.MaxInt64, math.MinInt64 + 1} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, n int64) {
		if n == math.MinInt64 {
			return
		}
		size := ByteSize(n)
		got, err := parseByteSize(size.String())
		if err != nil || got != size {
			t.Fatal(test.DiffMessage([]any{got, err}, size, "B4: round trip"))
		}
	})
}
