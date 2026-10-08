package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kaiehrhardt/tm-lint/internal/lint"
)

// Output format names accepted by --format.
const (
	formatText   = "text"
	formatJSON   = "json"
	formatSarif  = "sarif"
	formatGitLab = "gitlab"
)

// outputFormats are the valid values for --format.
var outputFormats = map[string]bool{formatText: true, formatJSON: true, formatSarif: true, formatGitLab: true}

// render formats findings as format. base is the working directory file
// paths are made relative to.
func render(format, base string, findings []lint.Finding) (string, error) {
	switch format {
	case formatText:
		return renderText(base, findings), nil
	case formatJSON:
		return renderJSON(base, findings)
	case formatSarif:
		return renderSarif(base, findings)
	case formatGitLab:
		return renderGitLab(base, findings)
	default:
		return "", fmt.Errorf("unknown format %q (want %s, %s, %s or %s)", format, formatText, formatJSON, formatSarif, formatGitLab)
	}
}

// relFile makes file relative to base, with forward slashes, for use in
// output. It falls back to the absolute path if it cannot be made relative.
func relFile(base, file string) string {
	rel, err := filepath.Rel(base, file)
	if err != nil {
		rel = file
	}
	return filepath.ToSlash(rel)
}

// renderText is the default, editor/CI-friendly output:
// path:line:column: [rule] message
func renderText(base string, findings []lint.Finding) string {
	var sb strings.Builder
	for _, f := range findings {
		fmt.Fprintf(&sb, "%s:%d:%d: [%s] %s\n", relFile(base, f.File), f.Range.Start.Line, f.Range.Start.Column, f.Rule, f.Msg)
	}
	return sb.String()
}

// jsonFinding is one finding in the --format json output.
type jsonFinding struct {
	Rule      string `json:"rule"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	EndLine   int    `json:"endLine"`
	EndColumn int    `json:"endColumn"`
	Message   string `json:"message"`
}

// renderJSON renders findings as a JSON array, "[]" when there are none.
func renderJSON(base string, findings []lint.Finding) (string, error) {
	out := make([]jsonFinding, 0, len(findings))
	for _, f := range findings {
		out = append(out, jsonFinding{
			Rule:      f.Rule,
			File:      relFile(base, f.File),
			Line:      f.Range.Start.Line,
			Column:    f.Range.Start.Column,
			EndLine:   f.Range.End.Line,
			EndColumn: f.Range.End.Column,
			Message:   f.Msg,
		})
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

// SARIF 2.1.0 (a subset sufficient for findings without fixes or rule
// metadata beyond a short description). See
// https://docs.oasis-open.org/sarif/sarif/v2.1.0/os/sarif-v2.1.0-os.html
type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string           `json:"id"`
	ShortDescription sarifMultiformat `json:"shortDescription"`
}

type sarifMultiformat struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID    string           `json:"ruleId"`
	Level     string           `json:"level"`
	Message   sarifMultiformat `json:"message"`
	Locations []sarifLocation  `json:"locations"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           sarifRegion           `json:"region"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn"`
	EndLine     int `json:"endLine,omitempty"`
	EndColumn   int `json:"endColumn,omitempty"`
}

// renderSarif renders findings as a SARIF 2.1.0 log with one run, so GitHub
// code scanning (and other SARIF consumers) can ingest tm-lint's findings.
// The rule set is always the full list from lint.Rules, not just the rules
// that fired, as SARIF consumers expect every referenced ruleId to be
// declared.
func renderSarif(base string, findings []lint.Finding) (string, error) {
	rules := make([]sarifRule, 0, len(lint.Rules))
	for _, r := range lint.Rules {
		rules = append(rules, sarifRule{ID: r.Name, ShortDescription: sarifMultiformat{Text: r.Doc}})
	}

	results := make([]sarifResult, 0, len(findings))
	for _, f := range findings {
		results = append(results, sarifResult{
			RuleID:  f.Rule,
			Level:   "warning",
			Message: sarifMultiformat{Text: f.Msg},
			Locations: []sarifLocation{{
				PhysicalLocation: sarifPhysicalLocation{
					ArtifactLocation: sarifArtifactLocation{URI: relFile(base, f.File)},
					Region: sarifRegion{
						StartLine:   f.Range.Start.Line,
						StartColumn: f.Range.Start.Column,
						EndLine:     f.Range.End.Line,
						EndColumn:   f.Range.End.Column,
					},
				},
			}},
		})
	}

	log := sarifLog{
		Schema:  "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           "tm-lint",
				InformationURI: "https://github.com/kaiehrhardt/tm-lint",
				Rules:          rules,
			}},
			Results: results,
		}},
	}
	b, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

// gitlabIssue is one entry in the --format gitlab output: a subset of the
// Code Climate format GitLab's Code Quality widget reads. See
// https://docs.gitlab.com/ci/testing/code_quality/#integrate-common-tools-with-code-quality
type gitlabIssue struct {
	Description string         `json:"description"`
	CheckName   string         `json:"check_name"`
	Fingerprint string         `json:"fingerprint"`
	Severity    string         `json:"severity"`
	Location    gitlabLocation `json:"location"`
}

type gitlabLocation struct {
	Path  string      `json:"path"`
	Lines gitlabLines `json:"lines"`
}

type gitlabLines struct {
	Begin int `json:"begin"`
}

// renderGitLab renders findings as a GitLab Code Quality report: a JSON
// array, "[]" when there are none. All findings get severity "major"; tm-lint
// does not distinguish severities. The fingerprint is a hash of the rule,
// file, line and message, so the same finding keeps the same fingerprint
// across runs and GitLab can track it as unchanged instead of a new issue.
func renderGitLab(base string, findings []lint.Finding) (string, error) {
	out := make([]gitlabIssue, 0, len(findings))
	for _, f := range findings {
		file := relFile(base, f.File)
		out = append(out, gitlabIssue{
			Description: f.Msg,
			CheckName:   f.Rule,
			Fingerprint: gitlabFingerprint(f.Rule, file, f.Range.Start.Line, f.Msg),
			Severity:    "major",
			Location: gitlabLocation{
				Path:  file,
				Lines: gitlabLines{Begin: f.Range.Start.Line},
			},
		})
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

func gitlabFingerprint(rule, file string, line int, msg string) string {
	h := sha256.Sum256([]byte(rule + "\x00" + file + "\x00" + strconv.Itoa(line) + "\x00" + msg))
	return hex.EncodeToString(h[:])
}
