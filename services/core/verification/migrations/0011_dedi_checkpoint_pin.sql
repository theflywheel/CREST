-- The pinned DeDi checkpoint (#241): the newest signed tree head this
-- deployment has authenticated AND proven a forward-only extension of the one
-- before it.
--
-- Why persist it at all: an inclusion proof says "this record is in the log
-- rooted at R", and a malicious or compromised operator can rewrite history and
-- mint a fresh R that every inclusion proof still checks out against. What that
-- operator cannot do is produce a consistency proof from a checkpoint CREST
-- already pinned. So CREST pins the newest checkpoint and, thereafter, refuses
-- any checkpoint that is not an append-only extension of it. Persisting the pin
-- is what makes the guarantee survive a restart: a rewrite that happens while
-- CREST is down is still caught on the next boot, because the pin from before
-- the downtime is the baseline the post-downtime checkpoint is measured against.
--
-- One row, ever. The pin is a single fact about a single log, so the table is a
-- singleton enforced by a fixed primary key.
CREATE TABLE dedi_checkpoint_pin (
    id         boolean PRIMARY KEY DEFAULT true CHECK (id),
    origin     text        NOT NULL,
    tree_size  bigint      NOT NULL,
    root_hex   text        NOT NULL,
    -- The exact signed note bytes, verbatim. The signature covers these bytes
    -- and a re-serialisation is no longer signed, so the note is stored as it
    -- was served and re-parsed on load rather than reconstructed from columns.
    note       bytea       NOT NULL,
    -- Whether the note's signature was authenticated against the configured
    -- node key when it was pinned. False means the pin was adopted without a
    -- key configured — a weaker baseline, recorded honestly.
    verified   boolean     NOT NULL,
    pinned_at  timestamptz NOT NULL DEFAULT now()
);
