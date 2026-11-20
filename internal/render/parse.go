package render

import (
	"errors"
	"html/template"
	"io"
	"io/fs"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	textTemplate "text/template"
	"text/template/parse"
)

const (
	contentName    = "content"
	layoutFile     = "layout.html"
	indexName      = "index"
	layoutName     = "layout"
	partialsDir    = "partials"
	publicDir      = "public"
	htmlSuffix     = ".html"
	derivedMark    = "$htmltemplate"
	nonceFunc      = "nonce"
	pageAssetsFunc = "pageAssets"
)

type fileKind uint8

const (
	kindNone fileKind = iota
	kindPage
	kindLayout
	kindPartial
	kindError
	kindPageFile
)

type unit struct {
	file  string
	name  string
	trees map[string]*parse.Tree
}

type sources struct {
	pages         []*unit
	layoutByDir   map[string]*unit
	partialByName map[string]*unit
	errorPages    []*unit
	pageFiles     []string
	skipped       []string
}

type checkError struct {
	file string
	text string
}

func (e *checkError) Error() string {
	return e.text
}

type bindError struct {
	kind error
	file string
	text string
}

func (e *bindError) Error() string {
	return e.text
}

func (e *bindError) Unwrap() error {
	return e.kind
}

func classify(p string) fileKind {
	if hasHiddenSegment(p) {
		return kindNone
	}
	switch path.Ext(p) {
	case htmlSuffix:
	case ".css", ".js":
		return kindPageFile
	default:
		return kindNone
	}
	switch {
	case strings.HasPrefix(p, partialsDir+"/"):
		return kindPartial
	case path.Base(p) == layoutFile:
		return kindLayout
	case isErrorName(strings.TrimSuffix(p, htmlSuffix)):
		return kindError
	}
	return kindPage
}

func isErrorName(name string) bool {
	if len(name) != 3 || !isDigit(name[0]) {
		return false
	}
	return name[1:] == "xx" || (isDigit(name[1]) && isDigit(name[2]))
}

func isUsableErrorName(name string) bool {
	return name[0] == '4' || name[0] == '5'
}

func isDigit(b byte) bool {
	return '0' <= b && b <= '9'
}

func hasHiddenSegment(p string) bool {
	for segment := range strings.SplitSeq(p, "/") {
		if len(segment) > 0 && segment[0] == '.' {
			return true
		}
	}
	return false
}

func walkSources(fsys fs.FS, visit func(p string, d fs.DirEntry, kind fileKind) error) error {
	return fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return &FileError{File: p, Err: err}
		}
		if d.IsDir() {
			if p != "." && (d.Name()[0] == '.' || p == assetsDir || p == publicDir) {
				return fs.SkipDir
			}
			return nil
		}
		if p == assetsDir || p == publicDir {
			return nil
		}
		kind := classify(p)
		if kind == kindNone {
			return nil
		}
		return visit(p, d, kind)
	})
}

func fingerprint(fsys fs.FS) string {
	var b strings.Builder
	err := walkSources(fsys, func(p string, d fs.DirEntry, _ fileKind) error {
		info, err := d.Info()
		if err != nil {
			return err
		}
		b.WriteString(p)
		b.WriteByte(0)
		b.WriteString(strconv.FormatInt(info.Size(), 10))
		b.WriteByte(0)
		b.WriteString(strconv.FormatInt(info.ModTime().UnixNano(), 10))
		b.WriteByte(0)
		return nil
	})
	if err != nil {
		b.WriteString(err.Error())
	}
	return b.String()
}

func (e *Engine) parse(isLoad bool) (*site, error) {
	src, err := e.readSources()
	if err != nil {
		return nil, err
	}
	filesByOwner, orphans := src.ownPageFiles()
	if isLoad && len(orphans) > 0 {
		return nil, &checkError{file: orphans[0], text: orphans[0] + " belongs to no template: name it after a page or layout of its directory (X.css, X.<name>.css), or move it to assets/"}
	}
	if isLoad {
		e.skipped = append(e.skipped, src.skipped...)
	}
	s := &site{
		setByFile:   make(map[string]*pageSet, len(src.pages)),
		setByRoute:  make(map[string]*pageSet, len(e.routes)),
		errorByCode: map[int]*pageSet{},
		pageFiles:   map[string]string{},
		ownerByFile: map[string]string{},
	}
	pageNameByFile := make(map[string]string, len(src.pages))
	for _, page := range src.pages {
		pageNameByFile[page.file] = page.name
	}
	for owner, files := range filesByOwner {
		for _, p := range files {
			s.ownerByFile[p] = pageNameByFile[owner]
			hash := ""
			if !e.isReload {
				hash, err = hashFile(e.fsys, p)
				if err != nil {
					return nil, &FileError{File: p, Err: err}
				}
			}
			s.pageFiles[p] = hash
		}
	}

	for _, page := range src.pages {
		set, err := e.buildPageSet(src, s, filesByOwner, page, src.nearestLayout(page.file))
		if err != nil {
			return nil, err
		}
		s.setByFile[page.file] = set
		s.pageOrder = append(s.pageOrder, page.file)
	}
	for _, page := range src.errorPages {
		set, err := e.buildPageSet(src, s, filesByOwner, page, src.layoutByDir["."])
		if err != nil {
			return nil, err
		}
		s.errorFiles = append(s.errorFiles, page.file)
		if page.name[1:] == "xx" {
			s.errorByClass[page.name[0]-'0'] = set
			continue
		}
		code, _ := strconv.Atoi(page.name)
		s.errorByCode[code] = set
	}
	s.partials = make([]string, 0, len(src.partialByName))
	for name := range src.partialByName {
		s.partials = append(s.partials, name)
	}
	slices.Sort(s.partials)

	for _, key := range e.routes {
		set, err := s.resolve(key)
		if err != nil {
			return nil, err
		}
		s.setByRoute[key] = set
	}
	return s, nil
}

func (e *Engine) readSources() (*sources, error) {
	src := &sources{layoutByDir: map[string]*unit{}, partialByName: map[string]*unit{}}
	err := walkSources(e.fsys, func(p string, d fs.DirEntry, kind fileKind) error {
		if kind == kindPageFile {
			if !e.isAllowed(p, d, false) {
				src.skipped = append(src.skipped, p)
				return nil
			}
			src.pageFiles = append(src.pageFiles, p)
			return nil
		}
		if kind == kindError && !isUsableErrorName(strings.TrimSuffix(p, htmlSuffix)) {
			return &checkError{file: p, text: p + " is not an error page: use 400.html to 599.html, 4xx.html or 5xx.html"}
		}
		data, err := fs.ReadFile(e.fsys, p)
		if err != nil {
			return &FileError{File: p, Err: err}
		}
		u, err := e.parseUnit(p, string(data))
		if err != nil {
			return err
		}
		switch kind {
		case kindPage:
			src.pages = append(src.pages, u)
		case kindLayout:
			src.layoutByDir[path.Dir(p)] = u
		case kindPartial:
			src.partialByName[u.name] = u
		case kindError:
			src.errorPages = append(src.errorPages, u)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return src, nil
}

func (e *Engine) parseUnit(p, text string) (*unit, error) {
	t, err := template.New(p).Funcs(e.funcs).Parse(text)
	if err != nil {
		return nil, &FileError{File: p, Err: err}
	}
	u := &unit{file: p, name: strings.TrimSuffix(p, htmlSuffix), trees: map[string]*parse.Tree{}}
	for _, tt := range t.Templates() {
		if tt.Tree != nil {
			u.trees[tt.Name()] = tt.Tree
		}
	}
	return u, nil
}

func (src *sources) ownPageFiles() (map[string][]string, []string) {
	ownersByDir := map[string][]*unit{}
	for _, page := range src.pages {
		dir := path.Dir(page.file)
		ownersByDir[dir] = append(ownersByDir[dir], page)
	}
	for dir, layout := range src.layoutByDir {
		ownersByDir[dir] = append(ownersByDir[dir], layout)
	}
	ownersByDir["."] = append(ownersByDir["."], src.errorPages...)

	filesByOwner := map[string][]string{}
	var orphans []string
	for _, p := range src.pageFiles {
		base := path.Base(p)
		stem := strings.TrimSuffix(base, path.Ext(base))
		var owner *unit
		for _, candidate := range ownersByDir[path.Dir(p)] {
			t := path.Base(candidate.name)
			if (stem == t || strings.HasPrefix(stem, t+".")) && (owner == nil || len(t) > len(path.Base(owner.name))) {
				owner = candidate
			}
		}
		if owner == nil || strings.HasPrefix(p, partialsDir+"/") {
			orphans = append(orphans, p)
			continue
		}
		filesByOwner[owner.file] = append(filesByOwner[owner.file], p)
	}
	for _, files := range filesByOwner {
		slices.Sort(files)
	}
	return filesByOwner, orphans
}

func (src *sources) nearestLayout(file string) *unit {
	dir := path.Dir(file)
	for {
		if l, ok := src.layoutByDir[dir]; ok {
			return l
		}
		if dir == "." {
			return nil
		}
		dir = path.Dir(dir)
	}
}

func (e *Engine) buildPageSet(src *sources, s *site, filesByOwner map[string][]string, content, layout *unit) (*pageSet, error) {
	var files []string
	layoutName := ""
	if layout != nil {
		files = append(files, filesByOwner[layout.file]...)
		layoutName = layout.file
	}
	files = append(files, filesByOwner[content.file]...)
	set, err := e.buildSet(src, content, layout, template.HTML(pageTags(files, s.pageFiles)))
	if err != nil {
		return nil, err
	}
	if len(files) > 0 && !setCalls(set.Template, pageAssetsFunc) {
		target := "the page"
		if layout != nil {
			target = layout.file
		}
		return nil, &checkError{file: content.file, text: content.file + " has " + files[0] + ", but no template of the page calls {{pageAssets}}: add it to " + target}
	}
	set.file = content.file
	set.name = content.name
	set.layout = layoutName
	set.assets = files
	return set, nil
}

func pageTags(files []string, hashByFile map[string]string) string {
	var b strings.Builder
	for i, p := range files {
		if i > 0 {
			b.WriteByte('\n')
		}
		u := pagesURL + escapePath(p)
		if hash := hashByFile[p]; hash != "" {
			u += "?v=" + hash
		}
		u = template.HTMLEscapeString(u)
		if path.Ext(p) == ".css" {
			b.WriteString(`<link rel="stylesheet" href="` + u + `">`)
			continue
		}
		b.WriteString(`<script src="` + u + `" defer></script>`)
	}
	return b.String()
}

func escapePath(p string) string {
	segments := strings.Split(p, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}

func (s *site) resolve(key string) (*pageSet, error) {
	candidates, err := candidatesOf(key)
	if err != nil {
		return nil, err
	}
	var found []string
	for _, c := range candidates {
		if _, ok := s.setByFile[c]; ok {
			found = append(found, c)
		}
	}
	switch len(found) {
	case 0:
		return nil, &bindError{kind: ErrNoTemplate, file: candidates[0], text: "needs a template: " + strings.Join(candidates, " or ")}
	case 2:
		return nil, &bindError{kind: ErrTwoTemplates, file: found[0], text: "has two templates: " + found[0] + " and " + found[1]}
	}
	return s.setByFile[found[0]], nil
}

func candidatesOf(key string) ([]string, error) {
	if key == "" {
		return []string{indexName + htmlSuffix}, nil
	}
	segments := strings.Split(key, "/")
	first, last := segments[0], segments[len(segments)-1]
	if len(segments) == 1 && isErrorName(first) {
		return nil, &bindError{kind: ErrErrorPageName, text: "resolves to " + first + htmlSuffix + ", which is an error page, not a route"}
	}
	if first == partialsDir || first == assetsDir || first == publicDir {
		if len(segments) == 1 {
			return []string{first + htmlSuffix}, nil
		}
		return nil, &bindError{kind: ErrReservedDir, text: "can have no template: " + first + "/ is reserved"}
	}
	if last == indexName || last == layoutName {
		return []string{key + "/" + indexName + htmlSuffix}, nil
	}
	return []string{key + htmlSuffix, key + "/" + indexName + htmlSuffix}, nil
}

func (e *Engine) buildSet(src *sources, content, layout *unit, tags template.HTML) (*pageSet, error) {
	set, fileByName, err := e.assemble(src, content, layout, tags)
	if err != nil {
		return nil, err
	}
	page := &pageSet{Template: set, usesNonce: setCalls(set, nonceFunc)}
	execErr := set.Execute(io.Discard, nil)
	if execErr == nil {
		return page, nil
	}
	var escapeErr *template.Error
	if errors.As(execErr, &escapeErr) {
		return nil, &FileError{File: e.blame(src, escapeErr, content, layout), Err: execErr}
	}
	var runErr textTemplate.ExecError
	if errors.Is(execErr, errUnknownAsset) && errors.As(execErr, &runErr) {
		return nil, &FileError{File: fileOf(fileByName, runErr.Name, content.file), Err: execErr}
	}
	return page, nil
}

func setCalls(set *template.Template, fn string) bool {
	for _, t := range set.Templates() {
		if t.Tree != nil && calls(t.Tree.Root, fn) {
			return true
		}
	}
	return false
}

func calls(node parse.Node, fn string) bool {
	switch n := node.(type) {
	case *parse.ListNode:
		if n == nil {
			return false
		}
		for _, child := range n.Nodes {
			if calls(child, fn) {
				return true
			}
		}
		return false
	case *parse.ActionNode:
		return calls(n.Pipe, fn)
	case *parse.PipeNode:
		if n == nil {
			return false
		}
		for _, cmd := range n.Cmds {
			if calls(cmd, fn) {
				return true
			}
		}
		return false
	case *parse.CommandNode:
		for _, arg := range n.Args {
			if calls(arg, fn) {
				return true
			}
		}
		return false
	case *parse.ChainNode:
		return calls(n.Node, fn)
	case *parse.IdentifierNode:
		return n.Ident == fn
	case *parse.TemplateNode:
		return calls(n.Pipe, fn)
	case *parse.IfNode:
		return calls(n.Pipe, fn) || calls(n.List, fn) || calls(n.ElseList, fn)
	case *parse.RangeNode:
		return calls(n.Pipe, fn) || calls(n.List, fn) || calls(n.ElseList, fn)
	case *parse.WithNode:
		return calls(n.Pipe, fn) || calls(n.List, fn) || calls(n.ElseList, fn)
	}
	return false
}

func (e *Engine) assemble(src *sources, content, layout *unit, tags template.HTML) (*template.Template, map[string]string, error) {
	units := make([]*unit, 0, 4)
	if layout != nil {
		units = append(units, layout)
	}
	units = append(units, content)
	ordered := append(reachedPartials(src, units), units...)
	fileByName := map[string]string{}
	rootName := contentName
	if layout != nil {
		rootName = layout.file
	}
	for _, u := range ordered {
		for name := range u.trees {
			fileByName[setName(u, name, content)] = u.file
		}
	}
	for _, u := range ordered {
		for _, tree := range u.trees {
			for _, ref := range references(tree.Root) {
				if _, ok := fileByName[ref]; !ok {
					return nil, nil, &FileError{File: u.file, Err: errors.New("no such template " + strconv.Quote(ref))}
				}
			}
		}
	}

	set := template.New(rootName).Funcs(e.funcs).Funcs(template.FuncMap{pageAssetsFunc: func() template.HTML { return tags }})
	for _, u := range ordered {
		for name, tree := range u.trees {
			if _, err := set.AddParseTree(setName(u, name, content), tree.Copy()); err != nil {
				return nil, nil, &FileError{File: u.file, Err: err}
			}
		}
	}
	return set.Lookup(rootName), fileByName, nil
}

func setName(u *unit, name string, content *unit) string {
	if name != u.file {
		return name
	}
	if u == content {
		return contentName
	}
	if strings.HasPrefix(u.file, partialsDir+"/") {
		return u.name
	}
	return name
}

func reachedPartials(src *sources, roots []*unit) []*unit {
	var reached []*unit
	seen := map[string]bool{}
	queue := append([]*unit(nil), roots...)
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		for _, tree := range u.trees {
			for _, ref := range references(tree.Root) {
				partial, ok := src.partialByName[ref]
				if !ok || seen[ref] {
					continue
				}
				seen[ref] = true
				reached = append(reached, partial)
				queue = append(queue, partial)
			}
		}
	}
	return reached
}

func references(node parse.Node) []string {
	var names []string
	var walk func(parse.Node)
	walk = func(n parse.Node) {
		switch n := n.(type) {
		case *parse.ListNode:
			if n == nil {
				return
			}
			for _, child := range n.Nodes {
				walk(child)
			}
		case *parse.TemplateNode:
			names = append(names, n.Name)
		case *parse.IfNode:
			walk(n.List)
			walk(n.ElseList)
		case *parse.RangeNode:
			walk(n.List)
			walk(n.ElseList)
		case *parse.WithNode:
			walk(n.List)
			walk(n.ElseList)
		}
	}
	walk(node)
	return names
}

func (e *Engine) blame(src *sources, err *template.Error, content, layout *unit) string {
	if err.Node != nil {
		loc, _ := (*parse.Tree)(nil).ErrorContext(err.Node)
		if file, _, ok := strings.Cut(loc, ":"); ok && file != "" {
			return file
		}
	}
	if layout == nil || err.ErrorCode != template.ErrEndContext {
		return content.file
	}
	alone, _, err2 := e.assemble(src, content, nil, "")
	if err2 != nil {
		return content.file
	}
	var aloneErr *template.Error
	if errors.As(alone.Execute(io.Discard, nil), &aloneErr) {
		return content.file
	}
	return layout.file
}

func fileOf(fileByName map[string]string, name, fallback string) string {
	if i := strings.Index(name, derivedMark); i >= 0 {
		name = name[:i]
	}
	if file, ok := fileByName[name]; ok {
		return file
	}
	return fallback
}
