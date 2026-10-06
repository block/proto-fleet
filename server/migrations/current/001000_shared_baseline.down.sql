-- Baseline adoption is forward-only. Restore a coordinated backup to return
-- to an earlier application release; never discard application or private data.
DO $$
BEGIN
    RAISE EXCEPTION 'baseline migration cannot be reverted; restore the coordinated backup';
END
$$;
