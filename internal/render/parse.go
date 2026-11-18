package render

import (
	"errors"
	"html/template"
	"io"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"
	textTemplate "text/template"
	"text/template/parse"
)

const (
	contentName = "content"
	layoutFile  = "layout.html"
	errorFile   = "error.html"
	partialsDir = "partials"
	htmlSuffix  = ".html"
	derivedMark = "$htmltemplate"
	nonceFunc   = "nonce"
)

type fileKind uint8

const (
	kindNone fileKind = iota
	kindPage
	kindLayout
	kindPartial
	kindError
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
	errorPage     *unit
}

func classify(p string) fileKind {
	if !strings.HasSuffix(p, htmlSuffix) || hasHiddenSegment(p) || strings.HasPrefix(p, assetsDir+"/") {
		return kindNone
	}
	switch {
	case strings.HasPrefix(p, partialsDir+"/"):
		return kindPartial
	case path.Base(p) == layoutFile:
		return kindLayout
	case p == errorFile:
		return kindError
	}
	return kindPage
}

func hasHiddenSegment(p string) bool {
	for segment := range strings.SplitSeq(p, "/") {
		if len(segment) > 0 && segment[0] == '.' {
			return true
		}
	}
	return false
}

func walkTemplates(fsys fs.FS, visit func(p string, d fs.DirEntry, kind fileKind) error) error {
	return fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return &FileError{File: p, Err: err}
		}
		if d.IsDir() {
			if p != "." && (d.Name()[0] == '.' || p == assetsDir) {
				return fs.SkipDir
			}
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
	err := walkTemplates(fsys, func(p string, d fs.DirEntry, _ fileKind) error {
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

func (e *Engine) parse() (*site, error) {
	src, err := e.readSources()
	if err != nil {
		return nil, err
	}
	s := &site{pageByName: make(map[string]*pageSet, len(src.pages)+1), pages: make([]Page, 0, len(src.pages))}
	for _, page := range src.pages {
		layout := src.nearestLayout(page.file)
		set, err := e.buildSet(src, page, layout)
		if err != nil {
			return nil, err
		}
		s.pageByName[page.name] = set
		info := Page{Name: page.name}
		if layout != nil {
			info.Layout = layout.file
		}
		s.pages = append(s.pages, info)
	}
	s.partials = make([]string, 0, len(src.partialByName))
	for name := range src.partialByName {
		s.partials = append(s.partials, name)
	}
	slices.Sort(s.partials)
	if src.errorPage != nil {
		t, err := e.buildSet(src, src.errorPage, src.layoutByDir["."])
		if err != nil {
			return nil, err
		}
		s.errorPage = t
	}
	return s, nil
}

func (e *Engine) readSources() (*sources, error) {
	src := &sources{layoutByDir: map[string]*unit{}, partialByName: map[string]*unit{}}
	err := walkTemplates(e.fsys, func(p string, _ fs.DirEntry, kind fileKind) error {
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
			src.errorPage = u
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

func (e *Engine) buildSet(src *sources, content, layout *unit) (*pageSet, error) {
	set, fileByName, err := e.assemble(src, content, layout)
	if err != nil {
		return nil, err
	}
	page := &pageSet{Template: set, usesNonce: setCallsNonce(set)}
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

func setCallsNonce(set *template.Template) bool {
	for _, t := range set.Templates() {
		if t.Tree != nil && callsNonce(t.Tree.Root) {
			return true
		}
	}
	return false
}

func callsNonce(node parse.Node) bool {
	switch n := node.(type) {
	case *parse.ListNode:
		if n == nil {
			return false
		}
		return slices.ContainsFunc(n.Nodes, callsNonce)
	case *parse.ActionNode:
		return callsNonce(n.Pipe)
	case *parse.PipeNode:
		if n == nil {
			return false
		}
		for _, cmd := range n.Cmds {
			if callsNonce(cmd) {
				return true
			}
		}
		return false
	case *parse.CommandNode:
		return slices.ContainsFunc(n.Args, callsNonce)
	case *parse.ChainNode:
		return callsNonce(n.Node)
	case *parse.IdentifierNode:
		return n.Ident == nonceFunc
	case *parse.TemplateNode:
		return callsNonce(n.Pipe)
	case *parse.IfNode:
		return callsNonce(n.Pipe) || callsNonce(n.List) || callsNonce(n.ElseList)
	case *parse.RangeNode:
		return callsNonce(n.Pipe) || callsNonce(n.List) || callsNonce(n.ElseList)
	case *parse.WithNode:
		return callsNonce(n.Pipe) || callsNonce(n.List) || callsNonce(n.ElseList)
	}
	return false
}

func (e *Engine) assemble(src *sources, content, layout *unit) (*template.Template, map[string]string, error) {
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

	set := template.New(rootName).Funcs(e.funcs)
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
	alone, _, err2 := e.assemble(src, content, nil)
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
