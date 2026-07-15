WITH ranked_active_cohorts AS (
    SELECT
        id,
        row_number() OVER (
            PARTITION BY org_id, lower(trim(label))
            ORDER BY created_at, id
        ) AS duplicate_rank
    FROM cohort
    WHERE state = 'active'
      AND is_default = FALSE
),
duplicate_active_cohorts AS (
    SELECT id
    FROM ranked_active_cohorts
    WHERE duplicate_rank > 1
)
UPDATE cohort
SET label = trim(label) || ' (cohort ' || id || ')'
WHERE id IN (SELECT id FROM duplicate_active_cohorts);

CREATE UNIQUE INDEX uq_cohort_active_label_per_org
    ON cohort (org_id, lower(trim(label)))
    WHERE state = 'active' AND is_default = FALSE;
