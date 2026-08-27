package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The default set renders exactly the sentence the service has always sent, so
// moving the wording into configuration changed nothing for a deployment that
// configured nothing.
func TestTheDefaultTemplatesRenderTheOriginalSentence(t *testing.T) {
	t.Setenv("CREST_TEMPLATES_PATH", "") // the default set, whatever the ambient environment says
	msgs, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	got, err := msgs.render("confirm-your-work", "", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	want := "We have a record of work you did. Reply YES if it is right, or NO if it is not. " +
		"If you do not reply by 2 Jan we will accept it as recorded. You will be paid either way."
	if got != want {
		t.Errorf("default render changed:\n got %q\nwant %q", got, want)
	}
}

// A deployment translates the whole set — prompt, keywords and the
// paid-either-way sentence together — and the render uses all of it.
func TestATranslatedSetRendersWithItsOwnKeywords(t *testing.T) {
	writeTemplates(t, map[string]any{
		"replyYes":      "OUI",
		"replyNo":       "NON",
		"dateFormat":    "02/01",
		"paidEitherWay": "Vous serez payé dans tous les cas.",
		"messages": map[string]string{
			"confirm-your-work": "Nous avons un enregistrement de votre travail. Répondez {{.Yes}} " +
				"ou {{.No}} avant le {{.ClosesAt}}. {{.PaidEitherWay}}",
		},
	})
	msgs, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	got, err := msgs.render("confirm-your-work", "", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	for _, must := range []string{"OUI", "NON", "02/01", "Vous serez payé dans tous les cas."} {
		if !strings.Contains(got, must) {
			t.Errorf("the translated render is missing %q: %q", must, got)
		}
	}
}

// The check #59 said must exist: a template that drops the paid-either-way
// sentence is refused at startup, because a message implying payment depends
// on replying puts a worker under duress (W4). This is not a style rule and it
// must not be skippable by translation.
func TestATemplateWithoutThePaidEitherWaySentenceIsRefused(t *testing.T) {
	writeTemplates(t, map[string]any{
		"messages": map[string]string{
			"confirm-your-work": "Reply {{.Yes}} by {{.ClosesAt}} or you will not be paid.",
		},
	})
	if _, err := loadTemplates(); err == nil ||
		!strings.Contains(err.Error(), "paid-either-way") {
		t.Fatalf("a duress-shaped template was accepted: %v", err)
	}
}

// The keywords a worker is told to send live in the same configuration as the
// prompt that prints them, so an inbound parser (#29) reads the same fields
// and a translation cannot split them.
func TestTheReplyKeywordsAreTheTemplatesOwn(t *testing.T) {
	msgs, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	if msgs.ReplyYes != "YES" || msgs.ReplyNo != "NO" {
		t.Errorf("default keywords are %q/%q", msgs.ReplyYes, msgs.ReplyNo)
	}
}

func writeTemplates(t *testing.T, doc map[string]any) {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "templates.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CREST_TEMPLATES_PATH", path)
}
