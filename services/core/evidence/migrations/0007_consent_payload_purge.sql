-- Consent-withdrawn rows are retained for receipt and audit metadata, but their
-- parsed evidence payload is private data the worker asked CREST not to keep.
-- Units and claims are intentionally untouched: this only scrubs the unclear
-- queue payload for the rows that could not lawfully become work records.
UPDATE unclear_rows
   SET record = NULL
 WHERE kind = 'consent-withdrawn'
   AND record IS NOT NULL;
