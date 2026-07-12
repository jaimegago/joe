// Package skills implements Joe's Agent Skills consumer. Skills are
// judgment-elicitation documents loaded from ~/.joe/skills/ and surfaced into
// LLM context at decision time by the router.
//
// As-built scope: loading and deterministic keyword routing; the `joe skills`
// CLI (install/list/remove/update/approve/reject); fsnotify hot reload via the
// watcher; and the quarantine approve/reject flow that holds non-auto-approved
// installs until a human clears them. See docs/reference/joe-skills-design.md.
package skills

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// MaxSkillSize is the largest SKILL.md file (in bytes) Joe will load. Anything
// larger is rejected at parse time to prevent context-window exhaustion.
// See docs/reference/joe-skills-design.md "Skill content size limits".
const MaxSkillSize = 50 * 1024

// Skill is a parsed Agent Skills document.
type Skill struct {
	// Name is the skill's stable identifier, from frontmatter.
	Name string
	// Description is the one-line summary used for router matching.
	Description string
	// Body is the markdown content below the frontmatter — the actual
	// judgment frame that gets loaded into the LLM system prompt.
	Body string
	// Source is the absolute path to the SKILL.md file on disk.
	Source string
	// Hash is the sha256 of the raw file contents, hex-encoded.
	Hash string
}

type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// ParseSkill reads a SKILL.md byte buffer and returns the parsed Skill. The
// source argument is recorded on the Skill for diagnostics; it is not read.
func ParseSkill(source string, data []byte) (*Skill, error) {
	if len(data) > MaxSkillSize {
		return nil, fmt.Errorf("skill exceeds %d byte limit (%d bytes)", MaxSkillSize, len(data))
	}

	fm, body, err := splitFrontmatter(data)
	if err != nil {
		return nil, err
	}

	var meta frontmatter
	if err := yaml.Unmarshal(fm, &meta); err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}

	meta.Name = strings.TrimSpace(meta.Name)
	meta.Description = strings.TrimSpace(meta.Description)
	if meta.Name == "" {
		return nil, errors.New("frontmatter missing required field: name")
	}
	if meta.Description == "" {
		return nil, errors.New("frontmatter missing required field: description")
	}

	sum := sha256.Sum256(data)
	return &Skill{
		Name:        meta.Name,
		Description: meta.Description,
		Body:        strings.TrimSpace(body),
		Source:      source,
		Hash:        hex.EncodeToString(sum[:]),
	}, nil
}

// splitFrontmatter separates the YAML frontmatter block (between leading
// `---` delimiters) from the body. Returns an error if no frontmatter is
// present — Agent Skills require it.
func splitFrontmatter(data []byte) (frontmatterBytes []byte, body string, err error) {
	// Tolerate a UTF-8 BOM at the start of the file.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	lines := strings.Split(string(data), "\n")
	i := 0
	// Skip optional leading blank lines; require the first non-blank line to
	// be the opening `---` delimiter.
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) || strings.TrimRight(lines[i], " \t\r") != "---" {
		return nil, "", errors.New("missing YAML frontmatter delimiter '---'")
	}
	i++

	fmStart := i
	for i < len(lines) {
		if strings.TrimRight(lines[i], " \t\r") == "---" {
			fmText := strings.Join(lines[fmStart:i], "\n")
			bodyText := strings.Join(lines[i+1:], "\n")
			return []byte(fmText), bodyText, nil
		}
		i++
	}
	return nil, "", errors.New("missing closing YAML frontmatter delimiter '---'")
}
