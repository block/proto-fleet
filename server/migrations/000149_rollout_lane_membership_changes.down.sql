DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM rollout_lane_membership_change
    ) THEN
        RAISE EXCEPTION
            'cannot downgrade while rollout lane membership audit records exist';
    END IF;
END;
$$;

DROP TRIGGER IF EXISTS rollout_lane_membership_change_truncate_immutable
    ON rollout_lane_membership_change;
DROP TRIGGER IF EXISTS rollout_lane_membership_change_immutable
    ON rollout_lane_membership_change;
DROP FUNCTION IF EXISTS reject_rollout_lane_membership_change_mutation();
DROP TABLE IF EXISTS rollout_lane_membership_change;
