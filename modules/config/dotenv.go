package config

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
)

const (
	maxFileSize  = 1 << 20
	maxValueSize = 64 << 10
	// maxExpandedSize bounds all values of a file together after expansion: copies of one large
	// value would otherwise grow far past the file size.
	maxExpandedSize = 16 << 20
)

var byteOrderMark = []byte("\xef\xbb\xbf")

type dotEnv struct {
	path        string
	keys        []string
	valuesByKey map[string]envValue
}

type envValue struct {
	line  int
	parts []valuePart
}

type valuePart struct {
	text string
	ref  string
}

func loadDotEnv(path string, isExpandVariables bool) (map[string]any, error) {
	data, err := readEnvFile(path)
	if err != nil {
		return nil, err
	}
	env, err := parseDotEnv(path, data)
	if err != nil {
		return nil, err
	}
	if isExpandVariables {
		return env.expandAll()
	}
	return env.literalAll()
}

func readEnvFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, errors.New(path + ": " + osErrorText(err))
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New(path + ": not a regular file")
	}
	if info.Size() > maxFileSize {
		return nil, errors.New(path + ": larger than 1 MiB")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New(path + ": " + osErrorText(err))
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if err != nil {
		return nil, errors.New(path + ": " + osErrorText(err))
	}
	if len(data) > maxFileSize {
		return nil, errors.New(path + ": larger than 1 MiB")
	}
	return data, nil
}

func osErrorText(err error) string {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err.Error()
	}
	return err.Error()
}

func parseDotEnv(path string, data []byte) (*dotEnv, error) {
	data = bytes.TrimPrefix(data, byteOrderMark)
	env := &dotEnv{path: path, valuesByKey: map[string]envValue{}}
	if i := bytes.IndexByte(data, 0); i >= 0 {
		return nil, env.lineError(1+bytes.Count(data[:i], []byte{'\n'}), "NUL byte")
	}

	pos, line := 0, 1
	for pos < len(data) {
		lineEnd := endOfLine(data, pos)
		raw := bytes.TrimSuffix(data[pos:lineEnd], []byte{'\r'})
		text := trimLeftBlanks(raw)
		if len(text) == 0 || text[0] == '#' {
			pos, line = lineEnd+1, line+1
			continue
		}
		if rest, ok := bytes.CutPrefix(text, []byte("export")); ok && len(rest) > 0 && isBlank(rest[0]) {
			text = trimLeftBlanks(rest)
		}
		eq := bytes.IndexByte(text, '=')
		if eq < 0 {
			return nil, env.lineError(line, "expected KEY=value")
		}
		key := string(trimRightBlanks(text[:eq]))
		if !isValidKey(key) {
			return nil, env.lineError(line, "invalid key")
		}

		rest := text[eq+1:]
		value := trimLeftBlanks(rest)
		var parts []valuePart
		nextPos, nextLine := lineEnd+1, line+1
		if len(value) > 0 && (value[0] == '"' || value[0] == '\'') {
			start := pos + len(raw) - len(value) + 1
			var end, endLine int
			var err error
			if value[0] == '"' {
				parts, end, endLine, err = env.parseDoubleQuoted(data, start, line)
			} else {
				parts, end, endLine, err = env.parseSingleQuoted(data, start, line)
			}
			if err != nil {
				return nil, err
			}
			closeEnd := endOfLine(data, end)
			after := trimLeftBlanks(bytes.TrimSuffix(data[end:closeEnd], []byte{'\r'}))
			if len(after) > 0 && after[0] != '#' {
				return nil, env.lineError(endLine, "unexpected text after the closing quote")
			}
			nextPos, nextLine = closeEnd+1, endLine+1
		} else {
			parts = splitRefs(string(trimBlanks(cutComment(rest))))
		}

		if _, ok := env.valuesByKey[key]; !ok {
			env.keys = append(env.keys, key)
		}
		env.valuesByKey[key] = envValue{line: line, parts: parts}
		pos, line = nextPos, nextLine
	}
	return env, nil
}

func (env *dotEnv) parseDoubleQuoted(data []byte, start, line int) ([]valuePart, int, int, error) {
	var parts []valuePart
	var text []byte
	current := line
	for i := start; i < len(data); i++ {
		c := data[i]
		switch {
		case c == '"':
			return appendText(parts, text), i + 1, current, nil
		case c == '\\' && i+1 < len(data):
			if escaped, ok := escapeOf(data[i+1]); ok {
				text = append(text, escaped)
				i++
				continue
			}
			text = append(text, c)
		case c == '$' && i+1 < len(data) && data[i+1] == '{':
			name, n := refAt(data[i:])
			if n == 0 {
				text = append(text, c)
				continue
			}
			parts = append(appendText(parts, text), valuePart{ref: name})
			text = text[:0]
			i += n - 1
		case c == '\r' && i+1 < len(data) && data[i+1] == '\n':
		case c == '\n':
			current++
			text = append(text, c)
		default:
			text = append(text, c)
		}
	}
	return nil, 0, 0, env.lineError(line, "unterminated quote")
}

func (env *dotEnv) parseSingleQuoted(data []byte, start, line int) ([]valuePart, int, int, error) {
	end := bytes.IndexByte(data[start:], '\'')
	if end < 0 {
		return nil, 0, 0, env.lineError(line, "unterminated quote")
	}
	raw := data[start : start+end]
	text := bytes.ReplaceAll(raw, []byte("\r\n"), []byte{'\n'})
	return appendText(nil, text), start + end + 1, line + bytes.Count(raw, []byte{'\n'}), nil
}

func escapeOf(c byte) (byte, bool) {
	switch c {
	case 'n':
		return '\n', true
	case 'r':
		return '\r', true
	case 't':
		return '\t', true
	case '\\', '"', '$':
		return c, true
	}
	return 0, false
}

func appendText(parts []valuePart, text []byte) []valuePart {
	if len(text) == 0 {
		return parts
	}
	return append(parts, valuePart{text: string(text)})
}

func splitRefs(value string) []valuePart {
	var parts []valuePart
	start := 0
	for i := 0; i < len(value); i++ {
		if value[i] != '$' {
			continue
		}
		name, n := refAt([]byte(value[i:]))
		if n == 0 {
			continue
		}
		if i > start {
			parts = append(parts, valuePart{text: value[start:i]})
		}
		parts = append(parts, valuePart{ref: name})
		i += n - 1
		start = i + 1
	}
	if start < len(value) {
		parts = append(parts, valuePart{text: value[start:]})
	}
	return parts
}

func refAt(b []byte) (string, int) {
	if len(b) < 3 || b[0] != '$' || b[1] != '{' {
		return "", 0
	}
	end := bytes.IndexByte(b[2:], '}')
	if end < 0 {
		return "", 0
	}
	name := string(b[2 : 2+end])
	if !isValidKey(name) {
		return "", 0
	}
	return name, end + 3
}

func cutComment(rest []byte) []byte {
	for i := 1; i < len(rest); i++ {
		if rest[i] == '#' && isBlank(rest[i-1]) {
			return rest[:i]
		}
	}
	return rest
}

func endOfLine(data []byte, pos int) int {
	if i := bytes.IndexByte(data[pos:], '\n'); i >= 0 {
		return pos + i
	}
	return len(data)
}

func isBlank(c byte) bool {
	return c == ' ' || c == '\t'
}

func trimLeftBlanks(b []byte) []byte {
	return bytes.TrimLeft(b, " \t")
}

func trimRightBlanks(b []byte) []byte {
	return bytes.TrimRight(b, " \t")
}

func trimBlanks(b []byte) []byte {
	return bytes.Trim(b, " \t")
}

func (env *dotEnv) lineError(line int, detail string) error {
	return errors.New(env.path + ":" + strconv.Itoa(line) + ": " + detail)
}

func (env *dotEnv) literalAll() (map[string]any, error) {
	valuesByKey := make(map[string]any, len(env.keys))
	for _, key := range env.keys {
		value := env.valuesByKey[key]
		var b strings.Builder
		for _, part := range value.parts {
			if part.ref != "" {
				b.WriteString("${" + part.ref + "}")
			} else {
				b.WriteString(part.text)
			}
		}
		if b.Len() > maxValueSize {
			return nil, env.lineError(value.line, "value larger than 64 KiB")
		}
		valuesByKey[key] = b.String()
	}
	return valuesByKey, nil
}

func (env *dotEnv) expandAll() (map[string]any, error) {
	ex := &expansion{env: env, doneByKey: map[string]string{}, isVisitingByKey: map[string]bool{}}
	valuesByKey := make(map[string]any, len(env.keys))
	for _, key := range env.keys {
		value, err := ex.expand(key)
		if err != nil {
			return nil, err
		}
		valuesByKey[key] = value
	}
	return valuesByKey, nil
}

type expansion struct {
	env             *dotEnv
	doneByKey       map[string]string
	isVisitingByKey map[string]bool
	// total counts every value once, when it is expanded: a key expands the keys it refers to
	// before their own turn, so counting in file order would let a chain grow unchecked.
	total int
}

func (ex *expansion) expand(key string) (string, error) {
	if value, ok := ex.doneByKey[key]; ok {
		return value, nil
	}
	value := ex.env.valuesByKey[key]
	ex.isVisitingByKey[key] = true
	var b strings.Builder
	for _, part := range value.parts {
		text, err := ex.partText(part, value.line)
		if err != nil {
			return "", err
		}
		b.WriteString(text)
		if b.Len() > maxValueSize {
			return "", ex.env.lineError(value.line, "value larger than 64 KiB")
		}
	}
	delete(ex.isVisitingByKey, key)
	if ex.total += b.Len(); ex.total > maxExpandedSize {
		return "", ex.env.lineError(value.line, "values larger than 16 MiB in total after expansion")
	}
	ex.doneByKey[key] = b.String()
	return b.String(), nil
}

func (ex *expansion) partText(part valuePart, line int) (string, error) {
	switch {
	case part.ref == "":
		return part.text, nil
	case ex.isVisitingByKey[part.ref]:
		return "", ex.env.lineError(line, "${"+part.ref+"} refers to itself")
	}
	if _, ok := ex.env.valuesByKey[part.ref]; ok {
		return ex.expand(part.ref)
	}
	if value, ok := os.LookupEnv(part.ref); ok {
		return value, nil
	}
	return "", ex.env.lineError(line, "${"+part.ref+"} is not defined")
}
