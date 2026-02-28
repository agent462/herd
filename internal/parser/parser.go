package parser

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/agent462/herd/internal/config"
	"github.com/agent462/herd/internal/executor"
)

// FieldValue holds a single extracted field name and its value.
type FieldValue struct {
	Field string
	Value string
}

// HostParsed holds the parsed extraction results for a single host.
type HostParsed struct {
	Host   string
	Fields []FieldValue
	Err    error
}

// rule is a compiled extract rule.
type rule struct {
	field  string
	re     *regexp.Regexp // nil if using column mode
	column int            // 0 if using regex mode (1-based when set)
}

// OutputParser extracts structured fields from command output.
type OutputParser struct {
	rules []rule
}

// New creates an OutputParser from config extract rules.
// It compiles regex patterns and validates rules.
func New(rules []config.ExtractRule) (*OutputParser, error) {
	compiled := make([]rule, 0, len(rules))
	for _, r := range rules {
		cr := rule{field: r.Field}
		if r.Pattern != "" {
			re, err := regexp.Compile(r.Pattern)
			if err != nil {
				return nil, fmt.Errorf("invalid regex for field %q: %w", r.Field, err)
			}
			cr.re = re
		} else if r.Column > 0 {
			cr.column = r.Column
		} else {
			return nil, fmt.Errorf("rule for field %q must have pattern or column", r.Field)
		}
		compiled = append(compiled, cr)
	}
	return &OutputParser{rules: compiled}, nil
}

// Parse extracts fields from a single host's stdout.
func (p *OutputParser) Parse(host string, stdout []byte) *HostParsed {
	hp := &HostParsed{
		Host:   host,
		Fields: make([]FieldValue, 0, len(p.rules)),
	}

	text := string(stdout)

	for _, r := range p.rules {
		value := "-"
		if r.re != nil {
			matches := r.re.FindStringSubmatch(text)
			if len(matches) >= 2 {
				value = matches[1]
			}
		} else if r.column > 0 {
			value = extractColumn(text, r.column)
		}
		hp.Fields = append(hp.Fields, FieldValue{Field: r.field, Value: value})
	}

	return hp
}

// extractColumn splits text into lines, finds the first non-empty data line
// (skipping the first line as a header), splits by whitespace, and returns
// the column at the given 1-based index.
func extractColumn(text string, col int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	// Skip the first line (header) and find the first non-empty data line.
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if col <= len(fields) {
			return fields[col-1]
		}
		return "-"
	}
	return "-"
}

// ParseAll applies Parse to all host results.
func (p *OutputParser) ParseAll(results []*executor.HostResult) []*HostParsed {
	parsed := make([]*HostParsed, 0, len(results))
	for _, r := range results {
		hp := p.Parse(r.Host, r.Stdout)
		if r.Err != nil {
			hp.Err = r.Err
		}
		parsed = append(parsed, hp)
	}
	return parsed
}

// FormatTable renders parsed results as a formatted ASCII table with column alignment.
// If color is true, use ANSI codes for the header.
func FormatTable(parsed []*HostParsed, color bool) string {
	if len(parsed) == 0 {
		return ""
	}

	// Build column headers: HOST + each field name uppercased.
	headers := []string{"HOST"}
	for _, fv := range parsed[0].Fields {
		headers = append(headers, strings.ToUpper(fv.Field))
	}

	// Calculate max widths.
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, hp := range parsed {
		if len(hp.Host) > widths[0] {
			widths[0] = len(hp.Host)
		}
		for i, fv := range hp.Fields {
			if len(fv.Value) > widths[i+1] {
				widths[i+1] = len(fv.Value)
			}
		}
	}

	var sb strings.Builder

	// Build format string for each row.
	formatRow := func(values []string) string {
		parts := make([]string, len(values))
		for i, v := range values {
			parts[i] = fmt.Sprintf("%-*s", widths[i], v)
		}
		return strings.Join(parts, "  ")
	}

	// Write header.
	headerLine := formatRow(headers)
	if color {
		sb.WriteString("\033[1;36m")
		sb.WriteString(headerLine)
		sb.WriteString("\033[0m")
	} else {
		sb.WriteString(headerLine)
	}
	sb.WriteString("\n")

	// Write separator.
	dashes := make([]string, len(widths))
	for i, w := range widths {
		dashes[i] = strings.Repeat("-", w)
	}
	sb.WriteString(strings.Join(dashes, "  "))
	sb.WriteString("\n")

	// Write data rows.
	for _, hp := range parsed {
		values := []string{hp.Host}
		for _, fv := range hp.Fields {
			values = append(values, fv.Value)
		}
		sb.WriteString(formatRow(values))
		sb.WriteString("\n")
	}

	return sb.String()
}
