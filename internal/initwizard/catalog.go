// Package initwizard contains pure business logic for the interactive init flow.
package initwizard

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/KofTwentyTwo/license-tool/internal/config"
	"github.com/KofTwentyTwo/license-tool/internal/model"
	"github.com/KofTwentyTwo/license-tool/internal/render"
	"github.com/KofTwentyTwo/license-tool/internal/spdx"
)

// LanguageFamily is the stable label for a source preview sample.
type LanguageFamily string

// Language family labels supported by the source preview catalog.
const (
	LanguageTypeScriptJavaScript LanguageFamily = "TypeScript/JavaScript"
	LanguagePython               LanguageFamily = "Python"
	LanguageJava                 LanguageFamily = "Java"
	LanguageGo                   LanguageFamily = "Go"
	LanguageC                    LanguageFamily = "C"
	LanguageCPlusPlus            LanguageFamily = "C++"
	LanguageCSharp               LanguageFamily = "C#"
	LanguagePHP                  LanguageFamily = "PHP"
	LanguageRuby                 LanguageFamily = "Ruby"
	LanguageSwift                LanguageFamily = "Swift"
	LanguageKotlin               LanguageFamily = "Kotlin"
	LanguageRust                 LanguageFamily = "Rust"
	LanguageShell                LanguageFamily = "Shell"
	LanguagePowerShell           LanguageFamily = "PowerShell"
	LanguageR                    LanguageFamily = "R"
)

// Sample is one deterministic source file the init wizard can show in a preview.
type Sample struct {
	Language LanguageFamily
	Path     string
	Source   string
}

// SourcePreviewInput is the complete pure input for rendering a sample source
// preview. ResolvedYear is passed in so callers own clocks and git history.
type SourcePreviewInput struct {
	Config       model.Config
	Sample       Sample
	ResolvedYear string
}

// SourcePreview is a rendered example source file with the generated header applied.
type SourcePreview struct {
	Language LanguageFamily
	Path     string
	Content  string
}

var catalog = []Sample{
	{
		Language: LanguageTypeScriptJavaScript,
		Path:     "example.ts",
		Source: strings.TrimLeft(`
export function greet(name: string): string {
  return "Hello, " + name;
}
`, "\n"),
	},
	{
		Language: LanguagePython,
		Path:     "example.py",
		Source: strings.TrimLeft(`
def greet(name: str) -> str:
    return f"Hello, {name}"
`, "\n"),
	},
	{
		Language: LanguageJava,
		Path:     "Example.java",
		Source: strings.TrimLeft(`
package preview;

public class Example {
    public String greet(String name) {
        return "Hello, " + name;
    }
}
`, "\n"),
	},
	{
		Language: LanguageGo,
		Path:     "example.go",
		Source: strings.TrimLeft(`
package main

func greet(name string) string {
	return "Hello, " + name
}
`, "\n"),
	},
	{
		Language: LanguageC,
		Path:     "example.c",
		Source: strings.TrimLeft(`
#include <stdio.h>

void greet(const char *name) {
    printf("Hello, %s\n", name);
}
`, "\n"),
	},
	{
		Language: LanguageCPlusPlus,
		Path:     "example.cpp",
		Source: strings.TrimLeft(`
#include <iostream>

void greet(const std::string& name) {
    std::cout << "Hello, " << name << std::endl;
}
`, "\n"),
	},
	{
		Language: LanguageCSharp,
		Path:     "Example.cs",
		Source: strings.TrimLeft(`
public class Example
{
    public string Greet(string name)
    {
        return "Hello, " + name;
    }
}
`, "\n"),
	},
	{
		Language: LanguagePHP,
		Path:     "example.php",
		Source: strings.TrimLeft(`
<?php

function greet(string $name): string
{
    return "Hello, " . $name;
}
`, "\n"),
	},
	{
		Language: LanguageRuby,
		Path:     "example.rb",
		Source: strings.TrimLeft(`
def greet(name)
  "Hello, #{name}"
end
`, "\n"),
	},
	{
		Language: LanguageSwift,
		Path:     "Example.swift",
		Source: strings.TrimLeft(`
func greet(_ name: String) -> String {
    "Hello, \(name)"
}
`, "\n"),
	},
	{
		Language: LanguageKotlin,
		Path:     "Example.kt",
		Source: strings.TrimLeft(`
package preview

fun greet(name: String): String {
    return "Hello, $name"
}
`, "\n"),
	},
	{
		Language: LanguageRust,
		Path:     "example.rs",
		Source: strings.TrimLeft(`
fn greet(name: &str) -> String {
    format!("Hello, {}", name)
}
`, "\n"),
	},
	{
		Language: LanguageShell,
		Path:     "example.sh",
		Source: strings.TrimLeft(`
#!/usr/bin/env bash

greet() {
  printf 'Hello, %s\n' "$1"
}
`, "\n"),
	},
	{
		Language: LanguagePowerShell,
		Path:     "example.ps1",
		Source: strings.TrimLeft(`
function Get-Greeting {
    param([string]$Name)
    "Hello, $Name"
}
`, "\n"),
	},
	{
		Language: LanguageR,
		Path:     "example.R",
		Source: strings.TrimLeft(`
greet <- function(name) {
  paste("Hello", name)
}
`, "\n"),
	},
}

var extensionFamilies = map[string]LanguageFamily{
	".ts":    LanguageTypeScriptJavaScript,
	".tsx":   LanguageTypeScriptJavaScript,
	".mts":   LanguageTypeScriptJavaScript,
	".cts":   LanguageTypeScriptJavaScript,
	".js":    LanguageTypeScriptJavaScript,
	".jsx":   LanguageTypeScriptJavaScript,
	".mjs":   LanguageTypeScriptJavaScript,
	".cjs":   LanguageTypeScriptJavaScript,
	".py":    LanguagePython,
	".pyi":   LanguagePython,
	".java":  LanguageJava,
	".go":    LanguageGo,
	".c":     LanguageC,
	".h":     LanguageC,
	".cc":    LanguageCPlusPlus,
	".cpp":   LanguageCPlusPlus,
	".cxx":   LanguageCPlusPlus,
	".hh":    LanguageCPlusPlus,
	".hpp":   LanguageCPlusPlus,
	".hxx":   LanguageCPlusPlus,
	".cs":    LanguageCSharp,
	".php":   LanguagePHP,
	".phtml": LanguagePHP,
	".rb":    LanguageRuby,
	".swift": LanguageSwift,
	".kt":    LanguageKotlin,
	".kts":   LanguageKotlin,
	".rs":    LanguageRust,
	".sh":    LanguageShell,
	".bash":  LanguageShell,
	".zsh":   LanguageShell,
	".ksh":   LanguageShell,
	".ps1":   LanguagePowerShell,
	".psm1":  LanguagePowerShell,
	".r":     LanguageR,
}

// Catalog returns the full, stable source preview catalog.
func Catalog() []Sample {
	return append([]Sample(nil), catalog...)
}

// SelectSample returns the catalog sample that best matches repo file paths. If no
// catalog language is detected, it returns the C fallback.
func SelectSample(paths []string) Sample {
	detected := make(map[LanguageFamily]bool)
	for _, path := range paths {
		if sample, ok := SampleForPath(path); ok {
			detected[sample.Language] = true
		}
	}
	for _, sample := range catalog {
		if detected[sample.Language] {
			return sample
		}
	}
	return fallbackSample()
}

// SampleForPath maps one repository path to its preview sample when the extension
// belongs to the supported catalog.
func SampleForPath(path string) (Sample, bool) {
	ext := strings.ToLower(filepath.Ext(filepath.Base(path)))
	if ext == "" {
		return Sample{}, false
	}
	family, ok := extensionFamilies[ext]
	if !ok {
		return Sample{}, false
	}
	return sampleForFamily(family)
}

// BuildSourcePreview renders a generated header and inserts it into the selected
// sample source using the same SPDX, filetype, and render behavior as apply.
func BuildSourcePreview(in SourcePreviewInput) (SourcePreview, error) {
	sample := in.Sample
	if sample.Language == "" {
		sample = fallbackSample()
	}
	if in.ResolvedYear == "" {
		return SourcePreview{}, fmt.Errorf("initwizard: resolved year is required")
	}
	license, ok := spdx.Lookup(in.Config.License)
	if !ok {
		return SourcePreview{}, fmt.Errorf("initwizard: unknown license %q", in.Config.License)
	}
	ft, ok := config.LookupFunc(in.Config)(sample.Path)
	if !ok {
		return SourcePreview{}, fmt.Errorf("initwizard: no file type for sample %q", sample.Path)
	}
	header, err := render.Header(render.HeaderInput{
		License:  license,
		Holder:   in.Config.Holder,
		Year:     in.ResolvedYear,
		Style:    in.Config.Style,
		FileType: ft,
	})
	if err != nil {
		return SourcePreview{}, err
	}
	content, _ := render.Insert([]byte(sample.Source), ft, header)
	return SourcePreview{
		Language: sample.Language,
		Path:     sample.Path,
		Content:  string(content),
	}, nil
}

// BuildYAMLPreview renders the .license-tool.yaml preview for cfg.
func BuildYAMLPreview(cfg model.Config) ([]byte, error) {
	return config.RenderFile(cfg)
}

func fallbackSample() Sample {
	sample, _ := sampleForFamily(LanguageC)
	return sample
}

func sampleForFamily(family LanguageFamily) (Sample, bool) {
	for _, sample := range catalog {
		if sample.Language == family {
			return sample, true
		}
	}
	return Sample{}, false
}
