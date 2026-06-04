// Package filetype is the data-driven comment-syntax table. It maps a file path to
// the FileType describing how to wrap a license header for that language and which
// leading constructs must be preserved before the header is placed.
//
// WHY data-driven: header rendering must not break compilation or parsing across
// dozens of languages. Encoding each language's comment delimiters and preserve-first
// ordering (shebang, xml-decl, php-open, BOM, coding pragma, package decl) as data
// keeps the render/apply logic generic and lets users extend coverage via config
// without code changes.
//
// Uncommentable formats (JSON and friends) are present in the table with Skip set:
// they are detected and reported, never edited.
package filetype

import (
	"path/filepath"
	"strings"

	"github.com/KofTwentyTwo/license-tool/internal/model"
)

// block builds a block-comment style.
func block(open, close string) model.CommentStyle {
	return model.CommentStyle{Block: true, Open: open, Close: close}
}

// line builds a line-comment style.
func line(prefix string) model.CommentStyle {
	return model.CommentStyle{Block: false, LinePrefix: prefix}
}

// after builds a preserve-first rule the header is placed AFTER (shebang, xml-decl, etc.).
func after(k model.PreserveKind) model.PreserveRule {
	return model.PreserveRule{Kind: k, Before: false}
}

// before builds a preserve-first rule the header is placed BEFORE (package decl).
func before(k model.PreserveKind) model.PreserveRule {
	return model.PreserveRule{Kind: k, Before: true}
}

// builtin is the shipped file-type table. WHY a slice not a map: a file matches by
// extension OR exact filename, and some types share fallbacks; iterating a stable
// ordered slice keeps lookup deterministic. Index maps are built once in init.
//
// The "all: bom" rule from the design is applied uniformly: every text type that
// could carry a UTF-8 BOM lists PreserveBOM first so the header never lands before
// the BOM. It is included per-entry rather than globally to keep FileType
// self-describing for config overrides.
var builtin = []model.FileType{
	// C-family block comments, package/module-bearing languages first.
	{
		Name:          "Java",
		Extensions:    []string{".java"},
		CommentStyle:  block("/*", "*/"),
		PreserveFirst: []model.PreserveRule{after(model.PreserveBOM), before(model.PreservePackageDecl)},
	},
	{
		Name:          "Kotlin",
		Extensions:    []string{".kt", ".kts"},
		CommentStyle:  block("/*", "*/"),
		PreserveFirst: []model.PreserveRule{after(model.PreserveBOM), before(model.PreservePackageDecl)},
	},
	{
		Name:          "Go",
		Extensions:    []string{".go"},
		CommentStyle:  block("/*", "*/"),
		PreserveFirst: []model.PreserveRule{after(model.PreserveBOM), before(model.PreservePackageDecl)},
	},
	{
		Name:          "Swift",
		Extensions:    []string{".swift"},
		CommentStyle:  block("/*", "*/"),
		PreserveFirst: []model.PreserveRule{after(model.PreserveBOM)},
	},
	{
		Name:          "C/C++",
		Extensions:    []string{".c", ".cc", ".cpp", ".cxx", ".h", ".hh", ".hpp", ".hxx", ".m", ".mm"},
		CommentStyle:  block("/*", "*/"),
		PreserveFirst: []model.PreserveRule{after(model.PreserveBOM)},
	},
	{
		Name:          "Rust",
		Extensions:    []string{".rs"},
		CommentStyle:  line("// "),
		PreserveFirst: []model.PreserveRule{after(model.PreserveBOM)},
	},
	{
		Name:          "JavaScript/TypeScript",
		Extensions:    []string{".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".mts", ".cts"},
		CommentStyle:  block("/*", "*/"),
		PreserveFirst: []model.PreserveRule{after(model.PreserveBOM), after(model.PreserveShebang)},
	},
	{
		Name:          "CSS",
		Extensions:    []string{".css", ".scss", ".less"},
		CommentStyle:  block("/*", "*/"),
		PreserveFirst: []model.PreserveRule{after(model.PreserveBOM)},
	},

	// Hash-comment languages.
	{
		Name:          "Python",
		Extensions:    []string{".py", ".pyi"},
		CommentStyle:  line("# "),
		PreserveFirst: []model.PreserveRule{after(model.PreserveBOM), after(model.PreserveShebang), after(model.PreserveCodingPragma)},
	},
	{
		Name:          "Shell",
		Extensions:    []string{".sh", ".bash", ".zsh", ".ksh"},
		CommentStyle:  line("# "),
		PreserveFirst: []model.PreserveRule{after(model.PreserveBOM), after(model.PreserveShebang)},
	},
	{
		Name:          "Ruby",
		Extensions:    []string{".rb"},
		CommentStyle:  line("# "),
		PreserveFirst: []model.PreserveRule{after(model.PreserveBOM), after(model.PreserveShebang), after(model.PreserveCodingPragma)},
	},
	{
		Name:          "YAML",
		Extensions:    []string{".yaml", ".yml"},
		CommentStyle:  line("# "),
		PreserveFirst: []model.PreserveRule{after(model.PreserveBOM)},
	},
	{
		Name:          "Nix",
		Extensions:    []string{".nix"},
		CommentStyle:  line("# "),
		PreserveFirst: []model.PreserveRule{after(model.PreserveBOM)},
	},
	{
		Name:          "HCL/Terraform",
		Extensions:    []string{".hcl", ".tf", ".tfvars"},
		CommentStyle:  line("# "),
		PreserveFirst: []model.PreserveRule{after(model.PreserveBOM)},
	},
	{
		Name:          "Dockerfile",
		Extensions:    []string{".dockerfile"},
		Filenames:     []string{"Dockerfile", "Containerfile"},
		CommentStyle:  line("# "),
		PreserveFirst: []model.PreserveRule{after(model.PreserveBOM)},
	},

	// Markup with a leading declaration.
	{
		Name:          "XML/HTML",
		Extensions:    []string{".xml", ".html", ".htm", ".xhtml", ".svg", ".pom"},
		Filenames:     []string{"pom.xml"},
		CommentStyle:  block("<!--", "-->"),
		PreserveFirst: []model.PreserveRule{after(model.PreserveBOM), after(model.PreserveXMLDecl)},
	},

	// SQL and double-dash languages.
	{
		Name:          "SQL",
		Extensions:    []string{".sql"},
		CommentStyle:  line("-- "),
		PreserveFirst: []model.PreserveRule{after(model.PreserveBOM)},
	},
	{
		Name:          "Lua",
		Extensions:    []string{".lua"},
		CommentStyle:  line("-- "),
		PreserveFirst: []model.PreserveRule{after(model.PreserveBOM), after(model.PreserveShebang)},
	},

	// PHP: header goes after a CLI shebang (if any) and the opening <?php tag. The
	// shebang is listed BEFORE php-open for table correctness/clarity; a "#!" line on
	// a PHP CLI script is line 1, the <?php tag line 2, then the header. (The universal
	// shebang preservation in render/detect already covers this; the explicit rule
	// documents the ordering.)
	{
		Name:          "PHP",
		Extensions:    []string{".php", ".phtml"},
		CommentStyle:  block("/*", "*/"),
		PreserveFirst: []model.PreserveRule{after(model.PreserveBOM), after(model.PreserveShebang), after(model.PreservePHPOpen)},
	},

	// Uncommentable formats: detected and reported, never written.
	{
		Name:       "JSON",
		Extensions: []string{".json", ".jsonc", ".json5"},
		Skip:       true,
	},
}

// extIndex and nameIndex are built once from builtin for O(1) lookup.
var (
	extIndex  = map[string]model.FileType{}
	nameIndex = map[string]model.FileType{}
)

func init() {
	for _, ft := range builtin {
		for _, e := range ft.Extensions {
			extIndex[strings.ToLower(e)] = ft
		}
		for _, n := range ft.Filenames {
			nameIndex[n] = ft
		}
	}
}

// Lookup resolves a file path to its FileType. It matches an exact base name first
// (so "Dockerfile" and "pom.xml" win over a generic extension), then the lowercased
// extension. The bool is false when the path matches no known type.
func Lookup(path string) (model.FileType, bool) {
	base := filepath.Base(path)
	if ft, ok := nameIndex[base]; ok {
		return ft, true
	}
	ext := strings.ToLower(filepath.Ext(base))
	if ext == "" {
		return model.FileType{}, false
	}
	if ft, ok := extIndex[ext]; ok {
		return ft, true
	}
	return model.FileType{}, false
}

// Merge returns a copy of the built-in table layered with user overrides, keyed by
// extension (e.g. ".myext"). An override that names an existing extension replaces
// that extension's mapping; a new extension adds coverage. The returned function
// has Lookup's signature so callers can swap it in transparently.
//
// WHY return a closure: the built-in indexes are package-global and must stay
// immutable for concurrent callers; Merge produces an isolated, override-aware
// lookup without mutating shared state.
func Merge(overrides map[string]model.FileType) func(path string) (model.FileType, bool) {
	mergedExt := make(map[string]model.FileType, len(extIndex)+len(overrides))
	for k, v := range extIndex {
		mergedExt[k] = v
	}
	mergedName := make(map[string]model.FileType, len(nameIndex))
	for k, v := range nameIndex {
		mergedName[k] = v
	}
	for ext, ft := range overrides {
		key := strings.ToLower(ext)
		mergedExt[key] = ft
		for _, n := range ft.Filenames {
			mergedName[n] = ft
		}
	}
	return func(path string) (model.FileType, bool) {
		base := filepath.Base(path)
		if ft, ok := mergedName[base]; ok {
			return ft, true
		}
		ext := strings.ToLower(filepath.Ext(base))
		if ext == "" {
			return model.FileType{}, false
		}
		if ft, ok := mergedExt[ext]; ok {
			return ft, true
		}
		return model.FileType{}, false
	}
}
