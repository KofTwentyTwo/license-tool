// Package header owns the shared rules for managed license headers.
package header

import (
	"strings"

	"github.com/KofTwentyTwo/license-tool/internal/model"
)

// Sentinel is the marker embedded in every header this tool writes.
const Sentinel = "license-tool:managed"

var bom = []byte{0xEF, 0xBB, 0xBF}

// PreserveBoundary returns the byte offset after all leading preserve-first
// prefixes the file type places before the managed header.
func PreserveBoundary(content []byte, ft model.FileType) int {
	pos := 0

	if HasRule(ft, model.PreserveBOM, false) && hasBOM(content[pos:]) {
		pos += len(bom)
	}

	allowShebang := true
	allowXML := HasRule(ft, model.PreserveXMLDecl, false)
	allowPHP := HasRule(ft, model.PreservePHPOpen, false)
	allowPragma := HasRule(ft, model.PreserveCodingPragma, false)

	for pos < len(content) {
		lineEnd := LineEndOffset(content, pos)
		lineText := string(content[pos:lineEnd])
		trimmed := strings.TrimRight(lineText, "\r\n")
		consumed := false

		switch {
		case allowShebang && strings.HasPrefix(trimmed, "#!"):
			consumed = true
		case allowXML && strings.HasPrefix(strings.TrimSpace(trimmed), "<?xml"):
			consumed = true
		case allowPHP && strings.HasPrefix(strings.TrimSpace(trimmed), "<?php"):
			consumed = true
		case allowPragma && IsCodingPragma(trimmed):
			consumed = true
		}

		if !consumed {
			break
		}
		pos = lineEnd
	}

	return pos
}

// HasRule reports whether ft lists a preserve rule of the given kind and direction.
func HasRule(ft model.FileType, kind model.PreserveKind, before bool) bool {
	for _, r := range ft.PreserveFirst {
		if r.Kind == kind && r.Before == before {
			return true
		}
	}
	return false
}

// LineEndOffset returns the offset just past the newline terminating the line at pos
// or len(content) for the final unterminated line.
func LineEndOffset(content []byte, pos int) int {
	for i := pos; i < len(content); i++ {
		if content[i] == '\n' {
			return i + 1
		}
	}
	return len(content)
}

// IsCodingPragma reports whether trimmed is a Python/Ruby encoding pragma.
func IsCodingPragma(trimmed string) bool {
	t := strings.TrimSpace(trimmed)
	if !strings.HasPrefix(t, "#") {
		return false
	}
	lower := strings.ToLower(t)
	return strings.Contains(lower, "coding:") || strings.Contains(lower, "coding=")
}

func hasBOM(content []byte) bool {
	return len(content) >= len(bom) && content[0] == bom[0] && content[1] == bom[1] && content[2] == bom[2]
}
