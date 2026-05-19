package sqlstores

import (
	"fmt"
	"strings"

	"github.com/lib/pq"

	stores "github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// appendZoneKeyPredicate emits the scoped + wildcard OR-branch shape
// used by both the miner-list filter (device_filters.go, via membership
// JOIN with dsr.* alias) and the rack-list filter (collection_sort.go,
// with dcr.* alias). The shape is:
//
//	(<alias>.building_id, <alias>.zone) IN (SELECT b, z FROM UNNEST(...))
//	OR <alias>.zone = ANY(...)
//
// Either branch is omitted when its slice is empty. The caller is
// responsible for surrounding parentheses and whatever wrapping EXISTS
// or AND clause the predicate sits inside.
//
// columnAlias is the SQL alias prefix (e.g. "dsr" or "dcr") — without
// trailing dot. Caller passes the full prefix once; the helper emits
// `<alias>.building_id` and `<alias>.zone` against it.
func appendZoneKeyPredicate(
	sb *strings.Builder,
	args []any,
	argNum int,
	columnAlias string,
	keys []stores.ZoneKey,
) ([]any, int) {
	if len(keys) == 0 {
		return args, argNum
	}

	var scopedBuildingIDs []int64
	var scopedZones []string
	var wildcardZones []string
	for _, zk := range keys {
		if zk.BuildingID == 0 {
			wildcardZones = append(wildcardZones, zk.Zone)
		} else {
			scopedBuildingIDs = append(scopedBuildingIDs, zk.BuildingID)
			scopedZones = append(scopedZones, zk.Zone)
		}
	}

	first := true
	if len(scopedBuildingIDs) > 0 {
		fmt.Fprintf(sb,
			"(%s.building_id, %s.zone) IN ("+
				"SELECT b, z FROM UNNEST($%d::bigint[], $%d::text[]) AS t(b, z))",
			columnAlias, columnAlias, argNum, argNum+1)
		args = append(args, pq.Array(scopedBuildingIDs), pq.Array(scopedZones))
		argNum += 2
		first = false
	}
	if len(wildcardZones) > 0 {
		if !first {
			sb.WriteString(" OR ")
		}
		fmt.Fprintf(sb, "%s.zone = ANY($%d::text[])", columnAlias, argNum)
		args = append(args, pq.Array(wildcardZones))
		argNum++
	}
	return args, argNum
}
