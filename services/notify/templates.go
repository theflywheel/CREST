package main

// Worker-facing message templates are deployment configuration (#59).
//
// The sentence a worker receives is the ENTIRE mechanism by which they learn a
// record exists and that they can object; a worker who cannot read it has not
// been told, and the confirmation window then runs against them. Two
// deployments will certainly disagree about language, register, date format
// and reply keywords while both being CREST — so by the layering test the
// wording is L2, and what stays in the infrastructure is: which moments
// produce a message, which facts each carries, and the record that one was
// attempted.
//
// Two rules survive translation, enforced at startup rather than trusted:
//
//   - The confirmation message must contain the deployment's own
//     paid-either-way sentence. That sentence is not style: a message implying
//     payment depends on replying puts the worker under duress, and a
//     template that dropped it should fail to boot, not ship.
//   - The reply keywords live HERE, in the same configuration as the prompt
//     that prints them. When an inbound reply channel exists (#29) it must
//     read these same fields — a deployment that translates the prompt and
//     not the parser has told workers to say a word it does not understand.
//
// Language selection is not solved here and is recorded rather than implied:
// a Party carries no locale today, so a deployment configures ONE template
// set. Per-worker language needs a schema decision first (#59 stays the
// reference).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"

	"github.com/theflywheel/crest/pkg/config"
)

type templates struct {
	// ReplyYes and ReplyNo are what a worker is told to send back. Part of the
	// template set, not constants, for the reason above.
	ReplyYes string `json:"replyYes"`
	ReplyNo  string `json:"replyNo"`
	// DateFormat is a Go reference-time layout for the window's closing date.
	DateFormat string `json:"dateFormat"`
	// PaidEitherWay is the deployment's translation of "you will be paid
	// either way". Required, and required to appear in the confirmation
	// message — see check below.
	PaidEitherWay string `json:"paidEitherWay"`
	// Messages maps a notification kind to its template. {{.Subject}},
	// {{.ClosesAt}}, {{.Yes}}, {{.No}} and {{.PaidEitherWay}} are available.
	Messages map[string]string `json:"messages"`

	parsed map[string]*template.Template
}

// defaultTemplates is the English set the service shipped with, now as the
// default value of configuration rather than as code.
func defaultTemplates() templates {
	return templates{
		ReplyYes:      "YES",
		ReplyNo:       "NO",
		DateFormat:    "2 Jan",
		PaidEitherWay: "You will be paid either way.",
		Messages: map[string]string{
			"confirm-your-work": "We have a record of work you did. Reply {{.Yes}} if it is right, " +
				"or {{.No}} if it is not. If you do not reply by {{.ClosesAt}} we will accept it " +
				"as recorded. {{.PaidEitherWay}}",
			// Addressed to an operator rather than a worker, and worded so it
			// says what to do — naming the feed, because an alert that does
			// not say which thing broke is one nobody can act on.
			"source-went-quiet": "{{.Subject}} has stopped sending us work records. Work done " +
				"since then is not being recorded. Please check that feed.",
			"default": "There is an update about your work record.",
		},
	}
}

// loadTemplates reads CREST_TEMPLATES_PATH, or serves the default set. Errors
// are fatal in the caller for the same reason the approval model's are: a
// deployment that configured templates and silently got English instead has
// workers who were never told anything.
func loadTemplates() (templates, error) {
	t := defaultTemplates()
	if path := config.Str("CREST_TEMPLATES_PATH", ""); path != "" {
		raw, err := os.ReadFile(path) //nolint:gosec // a path the deployment configured
		if err != nil {
			return templates{}, fmt.Errorf("read templates: %w", err)
		}
		var configured templates
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&configured); err != nil {
			return templates{}, fmt.Errorf("parse templates: %w", err)
		}
		// A configured file is merged over the defaults key by key: each field
		// or message it names replaces the default, and every one it is silent
		// about keeps the built-in English. A partial file falling back per
		// message is better than a worker receiving nothing.
		if configured.ReplyYes != "" {
			t.ReplyYes = configured.ReplyYes
		}
		if configured.ReplyNo != "" {
			t.ReplyNo = configured.ReplyNo
		}
		if configured.DateFormat != "" {
			t.DateFormat = configured.DateFormat
		}
		if configured.PaidEitherWay != "" {
			t.PaidEitherWay = configured.PaidEitherWay
		}
		for kind, msg := range configured.Messages {
			t.Messages[kind] = msg
		}
	}

	if strings.TrimSpace(t.PaidEitherWay) == "" {
		return templates{}, fmt.Errorf("paidEitherWay is empty: the promise that payment does not depend on replying is W4's, not a style choice")
	}

	t.parsed = map[string]*template.Template{}
	for kind, msg := range t.Messages {
		parsed, err := template.New(kind).Option("missingkey=error").Parse(msg)
		if err != nil {
			return templates{}, fmt.Errorf("template %q: %w", kind, err)
		}
		t.parsed[kind] = parsed
	}

	// The duress check (#59): the confirmation message must carry the
	// paid-either-way sentence, in whatever language the deployment wrote
	// both. Rendered and checked, so a template that references the slot but
	// a config that broke it cannot slip through.
	rendered, err := t.render("confirm-your-work", "", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return templates{}, err
	}
	if !strings.Contains(rendered, t.PaidEitherWay) {
		return templates{}, fmt.Errorf("the confirm-your-work template does not contain the paid-either-way sentence %q: a message implying payment depends on replying puts a worker under duress (W4, #59)", t.PaidEitherWay)
	}
	return t, nil
}

func (t templates) render(kind, subject string, closesAt time.Time) (string, error) {
	parsed, ok := t.parsed[kind]
	if !ok {
		parsed = t.parsed["default"]
	}
	var out bytes.Buffer
	err := parsed.Execute(&out, map[string]string{
		"Subject":       subjectOr(subject, "A system that sends us work records"),
		"ClosesAt":      closesAt.Format(t.DateFormat),
		"Yes":           t.ReplyYes,
		"No":            t.ReplyNo,
		"PaidEitherWay": t.PaidEitherWay,
	})
	if err != nil {
		return "", fmt.Errorf("render %q: %w", kind, err)
	}
	return out.String(), nil
}
