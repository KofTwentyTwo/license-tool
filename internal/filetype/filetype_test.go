package filetype

import (
	"testing"

	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookupTable(t *testing.T) {
	cases := []struct {
		path      string
		wantName  string
		wantOK    bool
		wantBlock bool
		wantSkip  bool
	}{
		{"Foo.java", "Java", true, true, false},
		{"src/Main.kt", "Kotlin", true, true, false},
		{"build.gradle.kts", "Kotlin", true, true, false},
		{"main.go", "Go", true, true, false},
		{"App.swift", "Swift", true, true, false},
		{"lib.c", "C/C++", true, true, false},
		{"lib.cpp", "C/C++", true, true, false},
		{"lib.h", "C/C++", true, true, false},
		{"view.mm", "C/C++", true, true, false},
		{"main.rs", "Rust", true, false, false},
		{"app.ts", "JavaScript/TypeScript", true, true, false},
		{"app.tsx", "JavaScript/TypeScript", true, true, false},
		{"app.jsx", "JavaScript/TypeScript", true, true, false},
		{"app.js", "JavaScript/TypeScript", true, true, false},
		{"style.css", "CSS", true, true, false},
		{"style.scss", "CSS", true, true, false},
		{"style.less", "CSS", true, true, false},
		{"script.py", "Python", true, false, false},
		{"run.sh", "Shell", true, false, false},
		{"run.bash", "Shell", true, false, false},
		{"flake.nix", "Nix", true, false, false},
		{"main.tf", "HCL/Terraform", true, false, false},
		{"vars.tfvars", "HCL/Terraform", true, false, false},
		{"config.yaml", "YAML", true, false, false},
		{"config.yml", "YAML", true, false, false},
		{"pom.xml", "XML/HTML", true, true, false},
		{"index.html", "XML/HTML", true, true, false},
		{"icon.svg", "XML/HTML", true, true, false},
		{"schema.sql", "SQL", true, false, false},
		{"app.rb", "Ruby", true, false, false},
		{"index.php", "PHP", true, true, false},
		{"mod.lua", "Lua", true, false, false},
		{"Dockerfile", "Dockerfile", true, false, false},
		{"Containerfile", "Dockerfile", true, false, false},
		// Uncommentable: matched but Skip.
		{"package.json", "JSON", true, false, true},
		{"tsconfig.jsonc", "JSON", true, false, true},
		// Unknown.
		{"README", "", false, false, false},
		{"data.bin", "", false, false, false},
		{"noext", "", false, false, false},
	}
	for _, c := range cases {
		ft, ok := Lookup(c.path)
		assert.Equalf(t, c.wantOK, ok, "Lookup(%q) ok", c.path)
		if !c.wantOK {
			continue
		}
		assert.Equalf(t, c.wantName, ft.Name, "Lookup(%q) name", c.path)
		assert.Equalf(t, c.wantBlock, ft.CommentStyle.Block, "Lookup(%q) block", c.path)
		assert.Equalf(t, c.wantSkip, ft.Skip, "Lookup(%q) skip", c.path)
	}
}

func TestPreserveFirstOrdering(t *testing.T) {
	// Python: BOM, then shebang, then coding pragma; header lands after all three.
	py, ok := Lookup("setup.py")
	require.True(t, ok)
	require.Len(t, py.PreserveFirst, 3)
	assert.Equal(t, model.PreserveBOM, py.PreserveFirst[0].Kind)
	assert.Equal(t, model.PreserveShebang, py.PreserveFirst[1].Kind)
	assert.Equal(t, model.PreserveCodingPragma, py.PreserveFirst[2].Kind)

	// Shell: BOM then shebang.
	sh, ok := Lookup("run.sh")
	require.True(t, ok)
	require.Len(t, sh.PreserveFirst, 2)
	assert.Equal(t, model.PreserveShebang, sh.PreserveFirst[1].Kind)

	// XML: BOM then xml-decl.
	xml, ok := Lookup("pom.xml")
	require.True(t, ok)
	require.Len(t, xml.PreserveFirst, 2)
	assert.Equal(t, model.PreserveXMLDecl, xml.PreserveFirst[1].Kind)

	// PHP: BOM, then shebang (CLI scripts), then php-open. The shebang precedes the
	// <?php tag so a "#!" line stays line 1 on an executable PHP script.
	php, ok := Lookup("index.php")
	require.True(t, ok)
	require.Len(t, php.PreserveFirst, 3)
	assert.Equal(t, model.PreserveBOM, php.PreserveFirst[0].Kind)
	assert.Equal(t, model.PreserveShebang, php.PreserveFirst[1].Kind)
	assert.Equal(t, model.PreservePHPOpen, php.PreserveFirst[2].Kind)

	// Java: header goes BEFORE the package declaration.
	java, ok := Lookup("Main.java")
	require.True(t, ok)
	var sawPackageBefore bool
	for _, r := range java.PreserveFirst {
		if r.Kind == model.PreservePackageDecl {
			sawPackageBefore = r.Before
		}
	}
	assert.True(t, sawPackageBefore, "Java header must precede the package declaration")
}

func TestMergeOverrideAddsAndReplaces(t *testing.T) {
	custom := model.FileType{
		Name:         "MyLang",
		Extensions:   []string{".myext"},
		CommentStyle: model.CommentStyle{Block: false, LinePrefix: "// "},
	}
	// Replacing an existing extension with a different style.
	override := model.FileType{
		Name:         "YAML-custom",
		Extensions:   []string{".yaml"},
		CommentStyle: model.CommentStyle{Block: false, LinePrefix: "## "},
	}
	lookup := Merge(map[string]model.FileType{
		".myext": custom,
		".yaml":  override,
	})

	ft, ok := lookup("thing.myext")
	require.True(t, ok)
	assert.Equal(t, "MyLang", ft.Name)

	ft, ok = lookup("config.yaml")
	require.True(t, ok)
	assert.Equal(t, "YAML-custom", ft.Name)
	assert.Equal(t, "## ", ft.CommentStyle.LinePrefix)

	// Built-in unaffected types still resolve.
	ft, ok = lookup("Main.java")
	require.True(t, ok)
	assert.Equal(t, "Java", ft.Name)

	// The package-global Lookup must remain unmutated by Merge.
	orig, ok := Lookup("config.yaml")
	require.True(t, ok)
	assert.Equal(t, "YAML", orig.Name)
}

// TestMergeOverrideWithFilenames covers an override that carries a Filenames
// entry: the merged name index must pick it up so an exact base-name match
// resolves to the override (exercising the filename loop and the closure's
// name-index branch).
func TestMergeOverrideWithFilenames(t *testing.T) {
	override := model.FileType{
		Name:         "MakeLang",
		Extensions:   []string{".mk"},
		Filenames:    []string{"Makefile", "GNUmakefile"},
		CommentStyle: model.CommentStyle{Block: false, LinePrefix: "# "},
	}
	lookup := Merge(map[string]model.FileType{".mk": override})

	cases := []struct {
		name     string
		path     string
		wantOK   bool
		wantName string
	}{
		{"override filename Makefile", "Makefile", true, "MakeLang"},
		{"override filename in subdir", "build/GNUmakefile", true, "MakeLang"},
		{"override extension", "rules.mk", true, "MakeLang"},
		{"builtin filename still resolves", "Dockerfile", true, "Dockerfile"},
		{"builtin extension still resolves", "Main.java", true, "Java"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ft, ok := lookup(c.path)
			require.Equalf(t, c.wantOK, ok, "lookup(%q) ok", c.path)
			assert.Equalf(t, c.wantName, ft.Name, "lookup(%q) name", c.path)
		})
	}
}

// TestMergeLookupMisses covers the negative paths of the merged closure:
// a path with no extension and a path with an unknown extension. Both must
// report not-found, mirroring the package-global Lookup behavior.
func TestMergeLookupMisses(t *testing.T) {
	lookup := Merge(map[string]model.FileType{
		".myext": {
			Name:         "MyLang",
			Extensions:   []string{".myext"},
			CommentStyle: model.CommentStyle{Block: false, LinePrefix: "// "},
		},
	})

	cases := []struct {
		name string
		path string
	}{
		{"no extension, not a known filename", "README"},
		{"no extension bare base", "noext"},
		{"unknown extension", "data.bin"},
		{"dotfile resolves as extension and misses", ".unknownrc"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ft, ok := lookup(c.path)
			assert.Falsef(t, ok, "lookup(%q) should miss", c.path)
			assert.Equalf(t, model.FileType{}, ft, "lookup(%q) should return zero FileType", c.path)
		})
	}
}

// TestMergeEmptyOverrides ensures Merge with no overrides behaves exactly like
// the package-global Lookup across builtin extension, builtin filename, skip,
// and miss cases.
func TestMergeEmptyOverrides(t *testing.T) {
	lookup := Merge(nil)

	cases := []struct {
		name     string
		path     string
		wantOK   bool
		wantName string
	}{
		{"builtin extension", "main.go", true, "Go"},
		{"builtin filename", "pom.xml", true, "XML/HTML"},
		{"skip type", "package.json", true, "JSON"},
		{"unknown", "data.bin", false, ""},
		{"no extension", "README", false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ft, ok := lookup(c.path)
			require.Equalf(t, c.wantOK, ok, "lookup(%q) ok", c.path)
			if !c.wantOK {
				return
			}
			assert.Equalf(t, c.wantName, ft.Name, "lookup(%q) name", c.path)
		})
	}
}
