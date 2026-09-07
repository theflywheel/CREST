package parties

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/theflywheel/crest/pkg/clock"
	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

type recoveryBlobs struct {
	deleted []string
	fail    bool
}

func (b *recoveryBlobs) Delete(_ context.Context, key string) error {
	if b.fail {
		return errors.New("unavailable")
	}
	b.deleted = append(b.deleted, key)
	return nil
}
func (*recoveryBlobs) Put(context.Context, string, io.Reader, string) (store.Blob, error) {
	return store.Blob{}, errors.New("unused")
}
func (*recoveryBlobs) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("unused")
}
func (*recoveryBlobs) Exists(context.Context, string) (bool, error) {
	return false, errors.New("unused")
}

func TestConsentUploadRecoveryPreservesCommittedAndRetriesOrphans(t *testing.T) {
	dsn := os.Getenv("CREST_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires isolated CREST_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	name := fmt.Sprintf("upload_contract_%d", time.Now().UnixNano())
	db, err := store.Open(ctx, dsn, name, clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer func() { _, _ = db.Q().Exec(ctx, "DROP SCHEMA "+name+" CASCADE") }()
	fs := fstest.MapFS{"migrations/0001.sql": {Data: []byte(`CREATE TABLE consent_upload_intents(operation_hash text PRIMARY KEY,object_key text UNIQUE NOT NULL,updated_at timestamptz NOT NULL DEFAULT now()); CREATE TABLE consents(id text,artefact_key text,artefact_digest text,artefact_type text,revoked_at timestamptz);`)}}
	if err = db.Migrate(ctx, fs, "migrations"); err != nil {
		t.Fatal(err)
	}
	blobs := &recoveryBlobs{}
	d := service.Deps{DB: db, Blobs: blobs}
	key, err := prepareConsentUpload(ctx, d, "committed")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := prepareConsentUpload(ctx, d, "committed")
	if err != nil || replay != key {
		t.Fatalf("unstable key: %q %q %v", key, replay, err)
	}
	orphan, err := prepareConsentUpload(ctx, d, "orphan")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Q().Exec(ctx, "INSERT INTO consents(artefact_key) VALUES($1)", key); err != nil {
		t.Fatal(err)
	}
	if err = recoverConsentUploads(ctx, d); err != nil {
		t.Fatal(err)
	}
	if len(blobs.deleted) != 0 {
		t.Fatal("active upload deleted")
	}
	if _, err = db.Q().Exec(ctx, "UPDATE consent_upload_intents SET updated_at=now()-interval '2 hours'"); err != nil {
		t.Fatal(err)
	}
	blobs.fail = true
	if err = recoverConsentUploads(ctx, d); err == nil {
		t.Fatal("delete failure swallowed")
	}
	var count int
	if err = db.Q().QueryRow(ctx, "SELECT count(*) FROM consent_upload_intents").Scan(&count); err != nil || count != 2 {
		t.Fatalf("journal lost on failure: %d %v", count, err)
	}
	blobs.fail = false
	if err = recoverConsentUploads(ctx, d); err != nil {
		t.Fatal(err)
	}
	if len(blobs.deleted) != 1 || blobs.deleted[0] != orphan {
		t.Fatalf("deleted committed blob or wrong key: %v", blobs.deleted)
	}
	if err = db.Q().QueryRow(ctx, "SELECT count(*) FROM consent_upload_intents").Scan(&count); err != nil || count != 0 {
		t.Fatalf("journal not resolved: %d %v", count, err)
	}
	if err = recoverConsentUploads(ctx, d); err != nil {
		t.Fatal(err)
	}
	if len(blobs.deleted) != 1 {
		t.Fatal("completed recovery replayed")
	}
	if _, err = db.Q().Exec(ctx, "INSERT INTO consents(id,artefact_key,artefact_digest,artefact_type,revoked_at) VALUES('withdrawn','consent/withdrawn','digest','audio/ogg',now())"); err != nil {
		t.Fatal(err)
	}
	blobs.fail = true
	if err = recoverWithdrawnArtefacts(ctx, d); err == nil {
		t.Fatal("withdrawal delete failure swallowed")
	}
	blobs.fail = false
	if err = recoverWithdrawnArtefacts(ctx, d); err != nil {
		t.Fatal(err)
	}
	if len(blobs.deleted) != 2 || blobs.deleted[1] != "consent/withdrawn" {
		t.Fatal("withdrawn artefact not retried")
	}
	if err = recoverWithdrawnArtefacts(ctx, d); err != nil {
		t.Fatal(err)
	}
	if len(blobs.deleted) != 2 {
		t.Fatal("withdrawn deletion not finalized")
	}

}
