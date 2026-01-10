-- Down migration is intentionally a no-op.
-- We cannot reliably reverse this migration because:
-- 1. We don't track which devices originally had type='asic' vs were correctly typed
-- 2. Reverting would reintroduce the filtering bug where devices don't show up
--    when filtering by Proto Rig or Antminer type
-- 3. This is a data fix, not a schema change

SELECT 1; -- No-op to satisfy migration framework
