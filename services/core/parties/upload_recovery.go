package parties

import (
	"context"
	"fmt"
	"time"

	"github.com/theflywheel/crest/pkg/service"
	"github.com/theflywheel/crest/pkg/store"
)

func prepareConsentUpload(ctx context.Context, d service.Deps, operationHash string) (string, error) {
	key, err := store.PrepareBlobKey("consent")
	if err != nil {
		return "", err
	}
	err = d.DB.Q().QueryRow(ctx, `INSERT INTO consent_upload_intents(operation_hash,object_key) VALUES($1,$2)
 ON CONFLICT(operation_hash) DO UPDATE SET updated_at=now() RETURNING object_key`, operationHash, key).Scan(&key)
	return key, err
}

func recoverConsentUploads(ctx context.Context, d service.Deps) error {
	return d.DB.InTx(ctx, func(tx store.Querier) error {
		rows, err := tx.Query(ctx, `SELECT object_key FROM consent_upload_intents WHERE updated_at<now()-interval '1 hour' ORDER BY updated_at LIMIT 100 FOR UPDATE SKIP LOCKED`)
		if err != nil {
			return err
		}
		keys, err := store.Collect(rows, func(r store.Row) (string, error) {
			var key string
			err := r.Scan(&key)
			return key, err
		})
		if err != nil {
			return err
		}
		for _, key := range keys {
			var referenced bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM consents WHERE artefact_key=$1)", key).Scan(&referenced); err != nil {
				return err
			}
			if !referenced {
				if d.Blobs == nil {
					return fmt.Errorf("object store unavailable")
				}
				if err := d.Blobs.Delete(ctx, key); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, "DELETE FROM consent_upload_intents WHERE object_key=$1", key); err != nil {
				return err
			}
		}
		return nil
	})
}

func uploadRecoveryLoop(ctx context.Context, d service.Deps) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := recoverConsentUploads(ctx, d); err != nil {
				d.Log.Error("consent upload recovery failed", "error", err)
			}
			if err := recoverWithdrawnArtefacts(ctx, d); err != nil {
				d.Log.Error("withdrawn consent deletion retry failed", "error", err)
			}
		}
	}
}

func recoverWithdrawnArtefacts(ctx context.Context, d service.Deps) error {
	return d.DB.InTx(ctx, func(tx store.Querier) error {
		rows, err := tx.Query(ctx, `SELECT id,artefact_key FROM consents WHERE revoked_at IS NOT NULL AND artefact_key IS NOT NULL LIMIT 100 FOR UPDATE SKIP LOCKED`)
		if err != nil {
			return err
		}
		type artefact struct{ id, key string }
		pending, err := store.Collect(rows, func(r store.Row) (artefact, error) {
			var a artefact
			err := r.Scan(&a.id, &a.key)
			return a, err
		})
		if err != nil {
			return err
		}
		for _, a := range pending {
			if d.Blobs == nil {
				return fmt.Errorf("object store unavailable")
			}
			if err := d.Blobs.Delete(ctx, a.key); err != nil {
				return err
			}
			if err := clearArtefactKey(ctx, tx, a.id); err != nil {
				return err
			}
		}
		return nil
	})
}
