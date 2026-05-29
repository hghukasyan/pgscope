package incident

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/hghukasyan/pgscope/pkg/models"
	"gopkg.in/yaml.v3"
)

// WriteInvestigation renders an investigation report.
func WriteInvestigation(w io.Writer, rep models.InvestigationReport, format string) error {
	switch strings.ToLower(format) {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	case "yaml":
		enc := yaml.NewEncoder(w)
		enc.SetIndent(2)
		defer enc.Close()
		return enc.Encode(rep)
	default:
		return writeInvestigationText(w, rep)
	}
}

func writeInvestigationText(w io.Writer, rep models.InvestigationReport) error {
	inc := rep.Incident
	fmt.Fprintf(w, "PGScope — Incident Investigation\n\n")
	fmt.Fprintf(w, "Incident: %s\n", inc.ID)
	fmt.Fprintf(w, "Status:   %s\n", inc.Status)
	fmt.Fprintf(w, "Severity: %s\n", strings.ToUpper(string(inc.Severity)))
	fmt.Fprintf(w, "Title:    %s\n\n", inc.Title)

	fmt.Fprintf(w, "Affected database:\n  %s (%s)\n\n", inc.Database, inc.InstanceKey)

	if len(rep.Regressions) > 0 {
		fmt.Fprintf(w, "Detected regressions:\n")
		for _, r := range rep.Regressions {
			fmt.Fprintf(w, "  - %s: %+.0f%% (baseline=%.4g → current=%.4g) [%s]\n",
				r.MetricName, r.RelativeChange*100, r.BaselineValue, r.CurrentValue, r.Reliability)
		}
		fmt.Fprintln(w)
	}

	if len(rep.Hypotheses) > 0 {
		primary := rep.Hypotheses[0]
		fmt.Fprintf(w, "Primary hypothesis:\n  %s (%s)\n", primary.Title, primary.Confidence)
		if primary.Description != "" {
			fmt.Fprintf(w, "  %s\n", primary.Description)
		}
		if len(primary.SupportingEvidence) > 0 {
			fmt.Fprintf(w, "  Evidence:\n")
			for _, e := range primary.SupportingEvidence {
				line := e.Description
				if e.Value != "" {
					line += " — " + e.Value
				}
				fmt.Fprintf(w, "    + %s\n", line)
			}
		}
		if len(primary.ContradictingEvidence) > 0 {
			fmt.Fprintf(w, "  Contradictions:\n")
			for _, e := range primary.ContradictingEvidence {
				fmt.Fprintf(w, "    - %s\n", e.Description)
			}
		}
		if len(primary.MissingEvidence) > 0 {
			fmt.Fprintf(w, "  Missing:\n")
			for _, m := range primary.MissingEvidence {
				fmt.Fprintf(w, "    ? %s\n", m)
			}
		}
		fmt.Fprintln(w)

		if len(rep.Hypotheses) > 1 {
			fmt.Fprintf(w, "Alternative hypotheses:\n")
			for _, h := range rep.Hypotheses[1:] {
				fmt.Fprintf(w, "  - %s: %s\n", h.Title, h.Confidence)
			}
			fmt.Fprintln(w)
		}

		if primary.Remediation != "" {
			fmt.Fprintf(w, "Recommended investigation:\n  %s\n\n", primary.Remediation)
		}
		if primary.Validation != "" {
			fmt.Fprintf(w, "Validation:\n  %s\n\n", primary.Validation)
		}
	}

	if len(rep.Timeline) > 0 {
		fmt.Fprintf(w, "Timeline:\n")
		for _, e := range rep.Timeline {
			mark := "observed"
			if !e.Observed {
				mark = "window"
			}
			fmt.Fprintf(w, "  %s  %s  [%s]\n", e.At.UTC().Format(time.RFC3339), e.Summary, mark)
		}
		fmt.Fprintln(w)
	}

	if len(rep.Limitations) > 0 {
		fmt.Fprintf(w, "Limitations:\n")
		for _, l := range rep.Limitations {
			fmt.Fprintf(w, "  - %s: %s (%s)\n", l.Area, l.Reason, l.Impact)
		}
	}
	return nil
}
