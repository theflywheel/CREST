package verification

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"rsc.io/qr"

	"github.com/theflywheel/crest/pkg/credential"
	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/store"
)

// The printed card (#24, Blueprint §5).
//
// One of three ways a worker holds a credential, and the only one available to
// a worker without a phone — which makes it the holding mechanism for exactly
// the people the inclusion path exists for. Not a fallback: for them it is the
// wallet.
//
// Printed at issuance rather than at enrolment, settled 2026-08-24 on #24. At
// enrolment no work has been done and there is no credential to print; §5 used
// to say "from enrolment", which read as though a worker walked away from
// registration already holding their history. The card is produced through the
// same assisted-access channel enrolment uses.
//
// The QR carries the whole signed credential rather than a link to one. That is
// the property the offline path exists for: a bare scan verifies signature and
// schema with no network, so the record is provable to a stranger who has never
// heard of CREST and has no signal. A QR holding a URL would move the proof back
// onto a server and the worker back into needing one.
//
// What it deliberately does not carry: any trust tier. Strength is derived by
// whoever checks it, from the provenance facts inside — printing a tier would
// freeze a judgement onto a piece of paper that outlives the reasons for it.

// cardQRLevel is the error-correction level.
//
// Medium, not Low. A printed card lives in a pocket for months, gets rained on
// and folded, and the failure this guards against is a worker holding a card
// nobody can scan — which is indistinguishable, to them, from having no record
// at all. Higher levels than this would make the QR denser than a phone camera
// reliably reads at arm's length, which is the same failure from the other side.
const cardQRLevel = qr.M

func (h *handlers) credentialCard(w http.ResponseWriter, r *http.Request) {
	c, err := getCredential(r.Context(), h.d.DB.Q(), r.PathValue("id"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "credential", err, store.ErrNotFound)
		return
	}

	payload, err := credential.EncodePixelPass(c.Doc)
	if err != nil {
		httpx.Fail(w, h.d.Log, "encode the card payload", err)
		return
	}

	// The payload alone, for a caller that renders its own card — a print
	// station with its own stationery, or a test.
	if r.URL.Query().Get("format") == "payload" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(payload))
		return
	}

	code, err := qr.Encode(payload, cardQRLevel)
	if err != nil {
		// A credential too large to fit a QR is a real possibility as fields
		// are added, and it must not be a mystery when it happens.
		httpx.WriteError(w, http.StatusInsufficientStorage, "credential_does_not_fit_a_card",
			"this credential does not fit in a printable QR (%d characters of payload): %v",
			len(payload), err)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(renderCard(c, payload, code.PNG(), h.d.Clock.Now())))
}

// renderCard builds the printable page.
//
// Deliberately one self-contained document with the image inlined: the place
// this gets printed may have a printer and no internet, and a card whose QR is
// an <img src> to a server is a card that prints as a broken image icon in
// exactly the deployment that needs it most.
func renderCard(c issuedCredential, payload string, png []byte, now time.Time) string {
	var doc map[string]any
	_ = json.Unmarshal(c.Doc, &doc)
	subject, _ := doc["credentialSubject"].(map[string]any)

	field := func(k string) string {
		if subject == nil {
			return ""
		}
		v, ok := subject[k]
		if !ok {
			return ""
		}
		return fmt.Sprint(v)
	}
	outcome := ""
	if o, ok := subject["outcome"].(map[string]any); ok {
		outcome = fmt.Sprintf("%v %v", o["value"], o["unit"])
	}
	period := ""
	if p, ok := subject["period"].(map[string]any); ok {
		period = fmt.Sprint(p["start"])
		if end, ok := p["end"]; ok {
			period += " – " + fmt.Sprint(end)
		}
	}

	rows := [][2]string{
		{"work", field("activity")},
		{"amount", outcome},
		{"when", period},
		{"issued by", field("issuerOrg")},
	}
	var dl strings.Builder
	for _, row := range rows {
		if row[1] == "" {
			continue
		}
		fmt.Fprintf(&dl, "<dt>%s</dt><dd>%s</dd>", html.EscapeString(row[0]), html.EscapeString(row[1]))
	}

	return fmt.Sprintf(`<!doctype html><meta charset="utf-8">
<title>Work record</title>
<style>
 @page { margin: 10mm }
 body { font: 13px/1.5 system-ui, sans-serif; margin: 0; display: flex; justify-content: center; padding: 16px }
 .card { width: 86mm; border: 1px solid #222; border-radius: 3mm; padding: 6mm }
 h1 { font-size: 14px; margin: 0 0 1mm }
 .sub { color: #555; font-size: 11px; margin: 0 0 4mm }
 img { width: 100%%; image-rendering: pixelated; border: 1px solid #eee }
 dl { display: grid; grid-template-columns: auto 1fr; gap: 1mm 4mm; font-size: 11px; margin: 4mm 0 0 }
 dt { color: #555 } dd { margin: 0 }
 .foot { margin-top: 4mm; font-size: 9px; color: #555; border-top: 1px solid #eee; padding-top: 2mm }
 @media print { body { padding: 0 } .card { border: none } }
</style>
<div class="card">
  <h1>%s</h1>
  <p class="sub">%s</p>
  <img alt="Scan to read this record" src="data:image/png;base64,%s">
  <dl>%s</dl>
  <p class="foot">Scanning this code reads the whole signed record, with no internet needed.
  It carries no score and no identity number: how much weight it deserves is worked out
  by whoever checks it, from what is inside. Printed %s.</p>
</div>`,
		html.EscapeString(orDefault(field("activity"), "Work record")),
		html.EscapeString(field("issuerOrg")),
		base64.StdEncoding.EncodeToString(png),
		dl.String(),
		now.UTC().Format("2 January 2006"))
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
