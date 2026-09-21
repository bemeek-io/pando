-- The score counting only findings that can be fixed (R-313, design 09 §3).
--
-- Two numbers rather than one, because which of them an installation means is
-- host policy's to decide and policy changes without rescanning: a scan is a
-- fact about a revision, and re-deriving one from stored findings on every read
-- of every row in a list is the work this column exists to avoid.
--
-- Nullable, and null on every row written before this: a score that was never
-- computed is not a score of zero (R-318), and the reader falls back to the
-- score beside it rather than inventing one.
ALTER TABLE app_scans ADD COLUMN score_fixable int
    CHECK (score_fixable IS NULL OR (score_fixable BETWEEN 0 AND 100));
