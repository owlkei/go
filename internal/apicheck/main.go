package main

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const apiFile = "api/ginject.txt"

var targetPackages = []string{
	".",
	"exception",
	"log",
	"middlewares/*",
	"modules/*",
	"guards/*",
}

func main() {
	write := flag.Bool("write", false, "write "+apiFile+" instead of comparing")
	flag.Parse()
	os.Exit(run(".", nil, *write, os.Stdout))
}

func run(dir string, allowed []string, write bool, out io.Writer) (code int) {
	if allowed == nil {
		allowed = targetPackages
	}
	surface, err := loadSurface(dir)
	if err != nil {
		printLine(out, "api: "+err.Error())
		return 1
	}
	for _, path := range surface.publicPaths {
		if !isAllowed(surface.relPath(path), allowed) {
			printLine(out, "api: package "+path+" is importable: move it under internal/ or add it to the allow list")
			code = 1
		}
	}
	lines := surface.lines()
	file := filepath.Join(dir, filepath.FromSlash(apiFile))
	if write {
		if code != 0 {
			return code
		}
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			printLine(out, "api: "+err.Error())
			return 1
		}
		if err := os.WriteFile(file, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			printLine(out, "api: "+err.Error())
			return 1
		}
		return 0
	}
	data, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		printLine(out, "api: "+err.Error())
		return 1
	}
	diff := diffLines(splitLines(string(data)), lines)
	for _, line := range diff {
		printLine(out, line)
	}
	if len(diff) > 0 {
		code = 1
	}
	return code
}

func printLine(out io.Writer, line string) {
	_, _ = io.WriteString(out, line+"\n")
}

func isAllowed(rel string, allowed []string) bool {
	for _, entry := range allowed {
		if prefix, ok := strings.CutSuffix(entry, "/*"); ok {
			if strings.HasPrefix(rel, prefix+"/") {
				return true
			}
			continue
		}
		if rel == entry {
			return true
		}
	}
	return false
}

func splitLines(data string) []string {
	var lines []string
	for _, line := range strings.Split(data, "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	sort.Strings(lines)
	return lines
}

func diffLines(old, current []string) []string {
	var diff []string
	i, j := 0, 0
	for i < len(old) || j < len(current) {
		switch {
		case j == len(current) || (i < len(old) && old[i] < current[j]):
			diff = append(diff, "- "+old[i])
			i++
		case i == len(old) || current[j] < old[i]:
			diff = append(diff, "+ "+current[j])
			j++
		default:
			i++
			j++
		}
	}
	return diff
}
