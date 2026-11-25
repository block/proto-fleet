package sqlstores

import (
	"context"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

// joinHashboardRepairObligations runs with the ticket change's transaction,
// after the ticket's targets have recorded each hashboard's client at opening.
// Each client-held hashboard the ticket names joins its open obligation, or
// opens one. A module ticket names a hashboard only after an operator selects it.
func (s *SQLMaintenanceStore) joinHashboardRepairObligations(ctx context.Context, orgID, ticketID int64) error {
	q := s.GetQueries(ctx)
	if err := q.OpenHashboardRepairObligationsForTicket(ctx, sqlc.OpenHashboardRepairObligationsForTicketParams{OrgID: orgID, TicketID: ticketID}); err != nil {
		return maintenanceWriteError("open hashboard repair obligation", err)
	}
	if err := q.LinkRepairTicketToHashboardObligations(ctx, sqlc.LinkRepairTicketToHashboardObligationsParams{OrgID: orgID, TicketID: ticketID}); err != nil {
		return maintenanceWriteError("link hashboard repair obligation", err)
	}
	return nil
}
