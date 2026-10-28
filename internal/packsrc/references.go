package packsrc

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Object is the ordered-map shape used at the config boundary. packsrc accepts this
// small interface rather than depending on a config representation or decoding files.
type Object interface {
	Keys() []string
	Get(string) (any, bool)
}

// SourceDefinition describes one named tree of packs.
type SourceDefinition struct {
	Name string
	Path string // local tree root, or empty for a git tree
	Repo string // normalized git repository URL, or empty for a local tree
	Ref  string // authored git ref, or empty to pin the repository's advertised default
}

// PackReference is one pack named through the readable source vocabulary. Source is
// the physical source address; Name is the author-facing handle and never comes from
// a manifest. LocalPath is set only for local references.
type PackReference struct {
	Source          string
	Name            string
	SourceID        string
	LocalPath       string
	EmbeddedName    string
	AllowDefaultRef bool
	Origin          string
}

// ParseSourceDefinitions validates the raw pack_sources table without loading any
// config file. Local paths are retained verbatim for the caller to expand against its
// home directory; git URLs are normalized into the packsrc transport spelling.
func ParseSourceDefinitions(raw any) (map[string]SourceDefinition, error) {
	if raw == nil {
		return map[string]SourceDefinition{}, nil
	}
	m, ok := raw.(Object)
	if !ok {
		return nil, fmt.Errorf("pack_sources: expected a table")
	}
	out := make(map[string]SourceDefinition, len(m.Keys()))
	for _, name := range m.Keys() {
		if !validHandleSegment(name) || name == "local" || name == "yolo" {
			return nil, fmt.Errorf("pack_sources.%s: invalid source name", name)
		}
		value, _ := m.Get(name)
		def := SourceDefinition{Name: name}
		switch v := value.(type) {
		case string:
			if !strings.HasPrefix(v, "~/") && !filepath.IsAbs(v) {
				return nil, fmt.Errorf("pack_sources.%s: local source must be an absolute or ~/ path", name)
			}
			def.Path = v
		default:
			obj, ok := value.(Object)
			if !ok || len(obj.Keys()) == 0 {
				return nil, fmt.Errorf("pack_sources.%s: expected a local path or { git = URL, ref = REF }", name)
			}
			for _, key := range obj.Keys() {
				if key != "git" && key != "ref" {
					return nil, fmt.Errorf("pack_sources.%s.%s: unknown git source field", name, key)
				}
			}
			rawGit, has := obj.Get("git")
			git, isString := rawGit.(string)
			if !has || !isString {
				return nil, fmt.Errorf("pack_sources.%s: expected a local path or { git = URL, ref = REF }", name)
			}
			ref, err := explicitRef(obj, "pack_sources."+name)
			if err != nil {
				return nil, err
			}
			repo, err := normalizeRepository(git)
			if err != nil {
				return nil, fmt.Errorf("pack_sources.%s.git: %w", name, err)
			}
			def.Repo, def.Ref = repo, ref
		}
		out[name] = def
	}
	return out, nil
}

// ParsePackReference resolves an authored source handle or direct-git table using
// already parsed definitions. localRoot and home are supplied by the caller so this
// function has no ambient config or credential reads.
func ParsePackReference(raw any, sources map[string]SourceDefinition, localRoot, home, origin string,
	shippedNames map[string]bool) (PackReference, error) {
	if text, ok := raw.(string); ok {
		if text == "" || strings.TrimSpace(text) != text {
			return PackReference{}, fmt.Errorf("%s: expected a non-empty pack handle", origin)
		}
		if !strings.Contains(text, "/") {
			if shippedNames[text] {
				return PackReference{Source: "embedded:" + text, Name: text, SourceID: "shipped", EmbeddedName: text, Origin: origin}, nil
			}
			return PackReference{}, fmt.Errorf("%s: only shipped packs may be named without a source", origin)
		}
		parts := strings.Split(text, "/")
		if len(parts) != 2 || !validHandleSegment(parts[0]) || !validHandleSegment(parts[1]) {
			return PackReference{}, fmt.Errorf("%s: expected source/pack, such as local/matt", origin)
		}
		if parts[0] == "yolo" {
			if !shippedNames[parts[1]] {
				return PackReference{}, fmt.Errorf("%s: yolo/%s does not name a shipped pack", origin, parts[1])
			}
			return PackReference{Source: "embedded:" + parts[1], Name: text, SourceID: "shipped",
				EmbeddedName: parts[1], Origin: origin}, nil
		}
		if parts[0] == "local" {
			if localRoot == "" {
				return PackReference{}, fmt.Errorf("%s: local pack directory is unavailable", origin)
			}
			path := filepath.Join(localRoot, parts[1])
			return PackReference{Source: "file://" + filepath.ToSlash(path), Name: text,
				SourceID: "local", LocalPath: path, Origin: origin}, nil
		}
		def, ok := sources[parts[0]]
		if !ok {
			return PackReference{}, fmt.Errorf("%s: unknown pack source %q", origin, parts[0])
		}
		if def.Path != "" {
			root := expandTilde(def.Path, home)
			path := filepath.Join(root, parts[1])
			return PackReference{Source: "file://" + filepath.ToSlash(path), Name: text,
				SourceID: def.Name, LocalPath: path, Origin: origin}, nil
		}
		return PackReference{Source: gitPackAddress(def.Repo, parts[1], def.Ref), Name: text,
			SourceID: def.Name, AllowDefaultRef: def.Ref == "", Origin: origin}, nil
	}

	obj, ok := raw.(Object)
	if !ok || len(obj.Keys()) == 0 {
		return PackReference{}, fmt.Errorf("%s: expected a source/pack handle or { git = URL }", origin)
	}
	for _, key := range obj.Keys() {
		if key != "git" && key != "ref" {
			return PackReference{}, fmt.Errorf("%s.%s: unknown direct-git field", origin, key)
		}
	}
	rawGit, has := obj.Get("git")
	git, isString := rawGit.(string)
	if !has || !isString {
		return PackReference{}, fmt.Errorf("%s: expected { git = URL }", origin)
	}
	ref, err := explicitRef(obj, origin)
	if err != nil {
		return PackReference{}, err
	}
	repo, err := normalizeRepository(git)
	if err != nil {
		return PackReference{}, fmt.Errorf("%s.git: %w", origin, err)
	}
	name, err := ownerRepositoryName(repo)
	if err != nil {
		return PackReference{}, fmt.Errorf("%s.git: %w", origin, err)
	}
	return PackReference{Source: gitPackAddress(repo, "", ref), Name: name,
		SourceID: repo, AllowDefaultRef: ref == "", Origin: origin}, nil
}

// LocalDirectoryIdentity resolves a local pack directory through symlinks when it
// exists, so two authored handles for the same physical tree compare equal.
func LocalDirectoryIdentity(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	physical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(physical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", path)
	}
	return filepath.Clean(physical), nil
}

func validHandleSegment(s string) bool {
	if s == "" || s == "." || s == ".." || strings.TrimSpace(s) != s || strings.ContainsAny(s, "\\:/?#") {
		return false
	}
	return true
}

func explicitRef(obj Object, origin string) (string, error) {
	rawRef, hasRef := obj.Get("ref")
	if !hasRef {
		return "", nil
	}
	ref, valid := rawRef.(string)
	if !valid || strings.TrimSpace(ref) != ref || ref == "" {
		return "", fmt.Errorf("%s.ref: expected a non-empty string", origin)
	}
	return ref, nil
}

func normalizeRepository(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" || s != raw {
		return "", fmt.Errorf("expected an unqualified git repository URL")
	}
	s = strings.TrimPrefix(s, "git+")
	u, err := url.Parse(s)
	if err != nil || u.Path == "" ||
		(u.Scheme != "https" && u.Scheme != "ssh" && u.Scheme != "http" && u.Scheme != "file") ||
		(u.Scheme == "file" && (u.Host != "" || !filepath.IsAbs(u.Path))) ||
		(u.Scheme != "file" && u.Host == "") ||
		hasPasswordUserinfo(u.User) {
		return "", fmt.Errorf("expected an ssh, https, http, or absolute file git repository URL without embedded credentials")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("source repository URL takes no query or fragment")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if u.Scheme != "file" {
		u.Path = strings.TrimSuffix(u.Path, ".git")
	}
	if u.Path == "" || u.Path == "/" {
		return "", fmt.Errorf("repository URL must name a repository")
	}
	return u.String(), nil
}

func hasPasswordUserinfo(user *url.Userinfo) bool {
	if user == nil {
		return false
	}
	_, password := user.Password()
	return password
}

func ownerRepositoryName(repo string) (string, error) {
	u, err := url.Parse(repo)
	if err != nil {
		return "", err
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("direct git packs must use an owner/repository URL")
	}
	owner, repository := parts[0], strings.TrimSuffix(parts[1], ".git")
	if !validHandleSegment(owner) || !validHandleSegment(repository) {
		return "", fmt.Errorf("direct git packs must use an owner/repository URL")
	}
	return owner + "/" + repository, nil
}

func gitPackAddress(repo, subdir, ref string) string {
	var b strings.Builder
	b.WriteString("git+")
	b.WriteString(repo)
	if subdir != "" {
		b.WriteString("//")
		b.WriteString(subdir)
	}
	if ref != "" {
		b.WriteString("?ref=")
		b.WriteString(url.QueryEscape(ref))
	}
	return b.String()
}

func expandTilde(path, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return path
}
