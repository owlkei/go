package core

import (
	"errors"
	"math"
	"strconv"
)

type ByteSize int64

const (
	KB  ByteSize = 1000
	MB           = 1000 * KB
	GB           = 1000 * MB
	KiB ByteSize = 1024
	MiB          = 1024 * KiB
	GiB          = 1024 * MiB
)

const byteSizeText = " is not a size: write bytes or a number with KB, MB, GB (1000ⁿ) or KiB, MiB, GiB (1024ⁿ)"

var byteSizeUnits = [...]struct {
	name string
	size uint64
}{
	{"GiB", uint64(GiB)},
	{"GB", uint64(GB)},
	{"MiB", uint64(MiB)},
	{"MB", uint64(MB)},
	{"KiB", uint64(KiB)},
	{"KB", uint64(KB)},
}

func (s ByteSize) String() string {
	var buf [24]byte
	b := buf[:0]
	magnitude := uint64(s)
	if s < 0 {
		b = append(b, '-')
		magnitude = -magnitude
	}
	if magnitude == 0 {
		return "0"
	}
	for _, unit := range byteSizeUnits {
		if magnitude%unit.size == 0 {
			b = strconv.AppendUint(b, magnitude/unit.size, 10)
			return string(append(b, unit.name...))
		}
	}
	return string(strconv.AppendUint(b, magnitude, 10))
}

func parseByteSize(text string) (ByteSize, error) {
	digits := text
	isNegative := false
	if len(digits) > 0 && digits[0] == '-' {
		isNegative = true
		digits = digits[1:]
	}
	end := 0
	for end < len(digits) && digits[end] >= '0' && digits[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, byteSizeError(text)
	}
	multiplier := uint64(1)
	if unit := digits[end:]; unit != "" {
		multiplier = 0
		for _, u := range byteSizeUnits {
			if u.name == unit {
				multiplier = u.size
				break
			}
		}
		if multiplier == 0 {
			return 0, byteSizeError(text)
		}
	}
	n, err := strconv.ParseUint(digits[:end], 10, 64)
	if err != nil {
		return 0, byteSizeError(text)
	}
	limit := uint64(math.MaxInt64)
	if isNegative {
		limit++
	}
	if n > limit/multiplier {
		return 0, byteSizeError(text)
	}
	magnitude := n * multiplier
	if isNegative {
		return ByteSize(-magnitude), nil
	}
	return ByteSize(magnitude), nil
}

func byteSizeError(text string) error {
	message := strconv.Quote(text) + byteSizeText
	if len(text) > 0 && text[len(text)-1] == 'b' {
		message += "; a lower-case b means bits"
	}
	return errors.New(message)
}
