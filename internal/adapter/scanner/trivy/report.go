package trivy

import (
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/errs"
)

// Reading Trivy's report.
//
// The mapping into Pando's vocabulary lives here and nowhere else (R-251): core
// never learns what a `Misconfiguration` or a `PkgName` is, and this file never
// decides what a finding costs.

type report struct {
	Results []struct {
		Target          string `json:"Target"`
		Class           string `json:"Class"`
		Vulnerabilities []struct {
			VulnerabilityID  string `json:"VulnerabilityID"`
			PkgName          string `json:"PkgName"`
			InstalledVersion string `json:"InstalledVersion"`
			FixedVersion     string `json:"FixedVersion"`
			Severity         string `json:"Severity"`
			Title            string `json:"Title"`
		} `json:"Vulnerabilities"`
		Secrets []struct {
			RuleID    string `json:"RuleID"`
			Severity  string `json:"Severity"`
			Title     string `json:"Title"`
			StartLine int    `json:"StartLine"`
		} `json:"Secrets"`
		Misconfigurations []struct {
			ID       string `json:"ID"`
			Severity string `json:"Severity"`
			Title    string `json:"Title"`
			Message  string `json:"Message"`
		} `json:"Misconfigurations"`
	} `json:"Results"`
}

// parse turns a report into findings.
func parse(body []byte) ([]api.Finding, error) {
	if len(body) == 0 {
		// A scanner that printed nothing found nothing to say, which is not the
		// same as finding nothing wrong — but it is not an error either: Trivy
		// prints an empty report for an artifact it has no analyzer for.
		return nil, nil
	}

	var r report
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, errs.Wrap(errs.AdapterFailed, "The scanner's report could not be read.", err)
	}

	var out []api.Finding
	for _, result := range r.Results {
		for _, v := range result.Vulnerabilities {
			title := v.Title
			if title == "" {
				title = v.PkgName + " " + v.InstalledVersion
			}
			out = append(out, api.Finding{
				ID:       v.VulnerabilityID,
				Severity: severity(v.Severity),
				Title:    title,
				Target:   strings.TrimSpace(v.PkgName + " " + v.InstalledVersion + " · " + result.Target),
				Fix:      v.FixedVersion,
			})
		}
		for _, s := range result.Secrets {
			out = append(out, api.Finding{
				ID:       s.RuleID,
				Severity: severity(s.Severity),
				Title:    s.Title,
				Target:   result.Target,
			})
		}
		for _, m := range result.Misconfigurations {
			title := m.Title
			if m.Message != "" {
				title = m.Title + ": " + m.Message
			}
			out = append(out, api.Finding{
				ID:       m.ID,
				Severity: severity(m.Severity),
				Title:    title,
				Target:   result.Target,
			})
		}
	}
	return out, nil
}

// severity maps Trivy's scale onto Pando's.
//
// Anything unrecognized is `unknown`, which costs something rather than
// nothing: a scanner that grows a severity level must not be able to make an
// app look clean by using it.
func severity(s string) api.Severity {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "CRITICAL":
		return api.SeverityCritical
	case "HIGH":
		return api.SeverityHigh
	case "MEDIUM":
		return api.SeverityMedium
	case "LOW":
		return api.SeverityLow
	default:
		return api.SeverityUnknown
	}
}

// walk visits a source tree, skipping what a scanner should not read.
func walk(dir string, visit func(rel string, info os.FileInfo, body io.Reader) error) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		// `.git` is the repository's history, which is large, and which the
		// scanner has no use for: the checkout beside it is the same content at
		// the commit that matters.
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		// Regular files and directories only. A symlink into the rest of the
		// filesystem is exactly what should not be copied into a scanner.
		if !info.Mode().IsRegular() && !info.IsDir() {
			return nil
		}
		if info.IsDir() {
			return visit(rel+"/", info, nil)
		}

		f, err := os.Open(path) //nolint:gosec // G304: a path from the checkout this adapter was asked to scan.
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		return visit(rel, info, f)
	})
}
