package core

import (
	"crypto/tls"
	"errors"
	"io/fs"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.owlky.com/owlky/internal/dotenv"
	"go.owlky.com/owlky/internal/versioning"
	"go.owlky.com/owlky/log"
)

const (
	envPrefix      = "OK_"
	envPortName    = "PORT"
	envServerPort  = "OK_SERVER__PORT"
	envFileName    = ".env"
	envEnvironment = "environment"
	envMaxHint     = 3
	envSecretMask  = "***"
	envCodeSource  = "code"
	envCodeOnlySet = "set"
)

var secretFields = map[string]bool{}

var (
	byteSizeType    = reflect.TypeFor[ByteSize]()
	durationType    = reflect.TypeFor[time.Duration]()
	proxyHeaderType = reflect.TypeFor[ProxyHeader]()
	versionTypeType = reflect.TypeFor[versioning.Type]()
	tlsConfigType   = reflect.TypeFor[tls.Config]()
)

var (
	proxyHeaderNames = []string{"x-forwarded", "forwarded"}
	versionTypeNames = map[string]versioning.Type{
		"query":      versioning.TypeQuery,
		"header":     versioning.TypeHeader,
		"media-type": versioning.TypeMediaType,
		"custom":     versioning.TypeCustom,
	}
)

type envRecord struct {
	errors []string
	fields []envField
}

type envField struct {
	path     string
	variable string
	source   string
	value    any
}

type envLeaf struct {
	path       string
	name       string
	index      []int
	typ        reflect.Type
	isCodeOnly bool
}

type envValue struct {
	text   string
	source string
}

func OptionsFromEnv() *AppOptions {
	opts := &AppOptions{
		Server:        &ServerOptions{TLS: &TLSOptions{}},
		HTTP:          &HTTPOptions{},
		Render:        &RenderOptions{},
		WS:            &WSOptions{},
		Versioning:    &versioning.Options{},
		Proxy:         &ProxyOptions{},
		LoadShed:      &LoadShedOptions{},
		HTTPSRedirect: &HTTPSRedirectOptions{},
		Headers:       &HeadersOptions{},
		CORS:          &CORSOptions{},
		CSRF:          &CSRFOptions{},
		Compress:      &CompressOptions{},
	}
	record := &envRecord{}
	opts.env = record

	values := map[string]envValue{}
	record.readDotEnv(values)
	for _, kv := range os.Environ() {
		name, text, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, envPrefix) || name == envPortName {
			values[name] = envValue{text: text, source: envEnvironment}
		}
	}

	leaves := envLeaves()
	leafByName := make(map[string]envLeaf, len(leaves))
	for _, leaf := range leaves {
		leafByName[leaf.name] = leaf
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	slices.Sort(names)

	root := reflect.ValueOf(opts).Elem()
	for _, name := range names {
		value := values[name]
		variable := name
		if name == envPortName {
			if _, ok := values[envServerPort]; ok {
				continue
			}
			name = envServerPort
		}
		leaf, ok := leafByName[name]
		switch {
		case !ok:
			record.fail(variable, value.source, "unknown variable"+envHint(name, leaves))
			continue
		case leaf.isCodeOnly:
			record.fail(variable, value.source, leaf.path+" is set in code only")
			continue
		case value.text == "":
			record.fail(variable, value.source, "empty value; remove the variable to unset it")
			continue
		}
		parsed, err := parseEnvValue(leaf.typ, value.text)
		if err != nil {
			record.fail(variable, value.source, maskSecret(leaf.path, err.Error()))
			continue
		}
		field := root.FieldByIndex(leaf.index)
		field.Set(parsed)
		record.fields = append(record.fields, envField{path: leaf.path, variable: variable, source: value.source, value: field.Interface()})
	}

	return opts
}

func (record *envRecord) readDotEnv(values map[string]envValue) {
	if _, err := os.Stat(envFileName); errors.Is(err, fs.ErrNotExist) {
		return
	}
	data, err := dotenv.ReadFile(envFileName)
	if err != nil {
		record.errors = append(record.errors, err.Error())
		return
	}
	file, err := dotenv.Parse(envFileName, data)
	if err != nil {
		record.errors = append(record.errors, err.Error())
		return
	}
	for _, key := range file.Keys() {
		if !strings.HasPrefix(key, envPrefix) && key != envPortName {
			continue
		}
		text, line, _ := file.Value(key)
		values[key] = envValue{text: text, source: envFileName + ":" + strconv.Itoa(line)}
	}
}

func (record *envRecord) fail(variable, source, detail string) {
	record.errors = append(record.errors, variable+" ("+source+"): "+detail)
}

func (record *envRecord) suffix(opts *AppOptions, path string) string {
	if record == nil {
		return ""
	}
	root := reflect.ValueOf(opts).Elem()
	for _, f := range record.fields {
		if f.path != path && !strings.HasPrefix(f.path, path+".") {
			continue
		}
		if current, ok := fieldByPath(root, f.path); ok && reflect.DeepEqual(current.Interface(), f.value) {
			return " (from " + f.variable + ", " + f.source + ")"
		}
	}
	return ""
}

func (record *envRecord) logOptions(opts *AppOptions, logger log.Logger) {
	root := reflect.ValueOf(opts).Elem()
	for _, leaf := range envLeaves() {
		current, ok := fieldByPath(root, leaf.path)
		if !ok || current.IsZero() {
			continue
		}
		if leaf.isCodeOnly {
			logger.Info("AppOption", "option", leaf.path, "value", envCodeOnlySet, "source", envCodeSource)
			continue
		}
		source := envCodeSource
		for _, f := range record.fields {
			if f.path == leaf.path && reflect.DeepEqual(current.Interface(), f.value) {
				source = f.variable
				if f.source != envEnvironment {
					source = f.source
				}
			}
		}
		value := envDisplay(current)
		if secretFields[leaf.path] {
			value = envSecretMask
		}
		logger.Info("AppOption", "option", leaf.path, "value", value, "source", source)
	}
}

func fieldByPath(root reflect.Value, path string) (reflect.Value, bool) {
	v := root
	for name := range strings.SplitSeq(path, ".") {
		for v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return reflect.Value{}, false
			}
			v = v.Elem()
		}
		v = v.FieldByName(name)
		if !v.IsValid() {
			return reflect.Value{}, false
		}
	}
	return v, true
}

func envDisplay(v reflect.Value) any {
	switch v.Type() {
	case byteSizeType:
		return ByteSize(v.Int()).String()
	case durationType:
		return time.Duration(v.Int()).String()
	case proxyHeaderType:
		if i := int(v.Int()); i >= 0 && i < len(proxyHeaderNames) {
			return proxyHeaderNames[i]
		}
	case versionTypeType:
		for name, t := range versionTypeNames {
			if t == versioning.Type(v.Int()) {
				return name
			}
		}
	}
	if v.Kind() == reflect.Slice {
		parts := make([]string, v.Len())
		for i := range parts {
			parts[i] = toText(envDisplay(v.Index(i)))
		}
		return strings.Join(parts, ",")
	}
	return v.Interface()
}

func toText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

func maskSecret(path, text string) string {
	if secretFields[path] {
		return envSecretMask
	}
	return text
}

func envLeaves() []envLeaf {
	var leaves []envLeaf
	var walk func(t reflect.Type, index []int, path, name string)
	walk = func(t reflect.Type, index []int, path, name string) {
		for i := range t.NumField() {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			childIndex := append(slices.Clone(index), i)
			childPath := f.Name
			childName := envPrefix + upperSnake(f.Name)
			if path != "" {
				childPath = path + "." + f.Name
				childName = name + "__" + upperSnake(f.Name)
			}
			if f.Type.Kind() == reflect.Pointer && f.Type.Elem().Kind() == reflect.Struct && f.Type.Elem() != tlsConfigType {
				walk(f.Type.Elem(), childIndex, childPath, childName)
				continue
			}
			leaves = append(leaves, envLeaf{path: childPath, name: childName, index: childIndex, typ: f.Type, isCodeOnly: !isEnvType(f.Type)})
		}
	}
	walk(reflect.TypeFor[AppOptions](), nil, "", "")
	return leaves
}

func isEnvType(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.String, reflect.Int, reflect.Int64, reflect.Bool:
		return true
	case reflect.Slice:
		return isEnvType(t.Elem())
	}
	return false
}

func upperSnake(name string) string {
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		c := name[i]
		isUpper := c >= 'A' && c <= 'Z'
		if isUpper && i > 0 {
			prev := name[i-1]
			isPrevLower := prev >= 'a' && prev <= 'z'
			isPrevUpper := prev >= 'A' && prev <= 'Z'
			isNextLower := i+1 < len(name) && name[i+1] >= 'a' && name[i+1] <= 'z'
			if isPrevLower || (isPrevUpper && isNextLower) {
				b.WriteByte('_')
			}
		}
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		b.WriteByte(c)
	}
	return b.String()
}

func parseEnvValue(t reflect.Type, text string) (reflect.Value, error) {
	if t.Kind() != reflect.Slice {
		return parseEnvScalar(t, text)
	}
	parts := strings.Split(text, ",")
	out := reflect.MakeSlice(t, len(parts), len(parts))
	for i, part := range parts {
		part = strings.Trim(part, " \t")
		position := "element " + strconv.Itoa(i+1) + " of " + strconv.Itoa(len(parts))
		if part == "" {
			return reflect.Value{}, errors.New(position + " is empty")
		}
		v, err := parseEnvScalar(t.Elem(), part)
		if err != nil {
			return reflect.Value{}, errors.New(position + ": " + err.Error())
		}
		out.Index(i).Set(v)
	}
	return out, nil
}

func parseEnvScalar(t reflect.Type, text string) (reflect.Value, error) {
	v := reflect.New(t).Elem()
	quoted := strconv.Quote(text)
	switch t {
	case byteSizeType:
		size, err := parseByteSize(text)
		if err != nil {
			return v, err
		}
		v.SetInt(int64(size))
		return v, nil
	case durationType:
		d, err := time.ParseDuration(text)
		if err != nil || !strings.ContainsFunc(text, isLetter) {
			return v, errors.New(quoted + " is not a duration with a unit (such as 30s, 5m or 1h30m)")
		}
		v.SetInt(int64(d))
		return v, nil
	case proxyHeaderType:
		i := slices.Index(proxyHeaderNames, text)
		if i < 0 {
			return v, errors.New(quoted + " is not one of " + strings.Join(proxyHeaderNames, ", "))
		}
		v.SetInt(int64(i))
		return v, nil
	case versionTypeType:
		vt, ok := versionTypeNames[text]
		if !ok {
			return v, errors.New(quoted + " is not one of query, header, media-type, custom")
		}
		v.SetInt(int64(vt))
		return v, nil
	}
	switch t.Kind() {
	case reflect.String:
		v.SetString(text)
	case reflect.Int:
		n, err := strconv.Atoi(text)
		if err != nil {
			return v, errors.New(quoted + " is not an integer")
		}
		v.SetInt(int64(n))
	case reflect.Bool:
		switch text {
		case "true":
			v.SetBool(true)
		case "false":
		default:
			return v, errors.New(quoted + " is not true or false")
		}
	}
	return v, nil
}

func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == 'µ'
}

func envHint(name string, leaves []envLeaf) string {
	best, bestDistance := "", envMaxHint+1
	for _, leaf := range leaves {
		if d := levenshtein(name, leaf.name); d < bestDistance {
			best, bestDistance = leaf.name, d
		}
	}
	if best == "" {
		return ""
	}
	return "; did you mean " + best + "?"
}

func levenshtein(a, b string) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}
