//go:build ignore

// Command references reads and writes the private reference links of this repository, kept in
// .local/go/references.json (a clone of the private repository owlkei/references, never committed).
//
//	go run scripts/references.go list
//	go run scripts/references.go get <key>
//	go run scripts/references.go set <key> <url>
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

const (
	cloneHint = "the references are missing: git clone git@github.com:owlkei/references.git .local"
	file      = ".local/go/references.json"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "references:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if _, err := os.Stat(".local/go"); errors.Is(err, fs.ErrNotExist) {
		return errors.New(cloneHint)
	}
	refs, err := load()
	if err != nil {
		return err
	}
	switch {
	case len(args) == 1 && args[0] == "list":
		keys := make([]string, 0, len(refs))
		for k := range refs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("%s\t%s\n", k, refs[k])
		}
	case len(args) == 2 && args[0] == "get":
		url, ok := refs[args[1]]
		if !ok {
			return fmt.Errorf("no reference named %q", args[1])
		}
		fmt.Println(url)
	case len(args) == 3 && args[0] == "set":
		refs[args[1]] = args[2]
		if err := save(refs); err != nil {
			return err
		}
		fmt.Println("saved; commit and push inside .local")
	default:
		return errors.New("usage: references list | get <key> | set <key> <url>")
	}
	return nil
}

func load() (map[string]string, error) {
	refs := map[string]string{}
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return refs, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &refs); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return refs, nil
}

func save(refs map[string]string) error {
	data, err := json.MarshalIndent(refs, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return os.WriteFile(file, append(data, '\n'), 0o644)
}
