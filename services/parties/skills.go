package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/theflywheel/crest/pkg/httpx"
	"github.com/theflywheel/crest/pkg/schema"
	"github.com/theflywheel/crest/pkg/store"
)

// The skill list (#16, Blueprint §3). See the migration for why this is
// reference data rather than a twelfth primitive, and for the finding about the
// global node.

const registrySkills = "skills"

// ErrSkillImmutable is any attempt to change a published skill code.
//
// The version lives inside the code, so there is no edit that is not a lie: the
// code is already in issued credentials that cannot be rewritten, and changing
// what it means would silently restate what a worker's record says they can do.
var ErrSkillImmutable = errors.New("a published skill code is immutable; publish a new version and supersede it")

func insertSkill(ctx context.Context, tx store.Querier, s schema.Skill) error {
	affected, err := tx.Exec(ctx, `
		INSERT INTO skills (code, label, description, supersedes, published_at)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (code) DO NOTHING`,
		// The column is NOT NULL DEFAULT ''; an absent description is an empty
		// string, not a NULL. A skill with no description is ordinary, and
		// letting it fail the insert would make the optional field mandatory
		// through the back door.
		s.Code, s.Label, deref(s.Description), s.Supersedes, s.PublishedAt)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrSkillImmutable
	}
	return nil
}

// deref reads an optional string, treating absent as empty.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func getSkill(ctx context.Context, q store.Querier, code string) (schema.Skill, error) {
	var s schema.Skill
	err := q.QueryRow(ctx, `
		SELECT code, label, description, supersedes, published_at
		FROM skills WHERE code = $1`, code).
		Scan(&s.Code, &s.Label, &s.Description, &s.Supersedes, &s.PublishedAt)
	return s, err
}

func listSkills(ctx context.Context, q store.Querier) ([]schema.Skill, error) {
	rows, err := q.Query(ctx, `
		SELECT code, label, description, supersedes, published_at FROM skills ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return store.Collect(rows, func(r store.Row) (schema.Skill, error) {
		var s schema.Skill
		if err := r.Scan(&s.Code, &s.Label, &s.Description, &s.Supersedes, &s.PublishedAt); err != nil {
			return schema.Skill{}, err
		}
		return s, nil
	})
}

// skillFace is the projection published to the node. The whole record: a skill
// code is public by construction, and a verifier resolving one needs all of it.
func skillFace(s schema.Skill) map[string]any {
	out := map[string]any{
		"code":        s.Code,
		"label":       s.Label,
		"publishedAt": s.PublishedAt.UTC().Format(time.RFC3339),
	}
	if s.Description != nil && *s.Description != "" {
		out["description"] = *s.Description
	}
	if s.Supersedes != nil && *s.Supersedes != "" {
		out["supersedes"] = *s.Supersedes
	}
	return out
}

func (h *handlers) publishSkill(w http.ResponseWriter, r *http.Request) {
	var s schema.Skill
	if !httpx.ReadJSON(w, r, &s) {
		return
	}
	if s.PublishedAt.IsZero() {
		s.PublishedAt = h.d.Clock.Now()
	}
	if err := schema.Validate(schema.IDSkill, s); err != nil {
		writeValidation(w, err)
		return
	}

	if err := h.d.DB.InTx(r.Context(), func(tx store.Querier) error {
		// A superseding skill must name one that exists. Otherwise the chain a
		// worker's older credentials hang from has a gap in it, and nothing
		// would notice until somebody tried to walk it.
		if s.Supersedes != nil && *s.Supersedes != "" {
			if _, err := getSkill(r.Context(), tx, *s.Supersedes); err != nil {
				return err
			}
		}
		if err := insertSkill(r.Context(), tx, s); err != nil {
			return err
		}
		return enqueueFact(r.Context(), tx, "skill", s.Code, 1)
	}); err != nil {
		switch {
		case errors.Is(err, ErrSkillImmutable):
			httpx.WriteError(w, http.StatusConflict, "immutable", "%s", ErrSkillImmutable)
		case errors.Is(err, store.ErrNotFound):
			httpx.WriteError(w, http.StatusUnprocessableEntity, "no_such_skill",
				"supersedes names a skill code this deployment does not have")
		default:
			httpx.Fail(w, h.d.Log, "publish skill", err)
		}
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, s)
}

func (h *handlers) listSkills(w http.ResponseWriter, r *http.Request) {
	skills, err := listSkills(r.Context(), h.d.DB.Q())
	if err != nil {
		httpx.Fail(w, h.d.Log, "list skills", err)
		return
	}
	if skills == nil {
		skills = []schema.Skill{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"skills": skills, "count": len(skills)})
}

func (h *handlers) getSkill(w http.ResponseWriter, r *http.Request) {
	s, err := getSkill(r.Context(), h.d.DB.Q(), r.PathValue("code"))
	if err != nil {
		httpx.NotFoundOr(w, h.d.Log, "skill", err, store.ErrNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s)
}
