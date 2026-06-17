// Package inventory is the domain layer for the InventoryService RPC
// surface. CRUD + insights + CSV import preview/confirm; stock
// adjustment methods (Decrement/Increment) live on the store and are
// called directly by the repair-ticket domain.
package inventory

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/activity"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/inventory/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// Event type constants for inventory activity logs.
const (
	eventPartCreated  = "inventory.part_created"
	eventPartUpdated  = "inventory.part_updated"
	eventPartDeleted  = "inventory.part_deleted"
	eventPartsImported = "inventory.parts_imported"
)

// List pagination defaults and caps.
const (
	ListDefaultLimit = int32(50)
	ListMaxLimit     = int32(200)
)

// CSV column header constants. The import parser expects these exact
// headers (case-insensitive) in the first row.
const (
	csvHeaderName         = "name"
	csvHeaderType         = "type"
	csvHeaderManufacturer = "manufacturer"
	csvHeaderPartNumber   = "part_number"
	csvHeaderSiteName     = "site_name"
	csvHeaderOnHand       = "on_hand"
	csvHeaderReorderPoint = "reorder_point"
	csvHeaderBinLocation  = "bin_location"
)

// Service is the domain entry point for inventory part CRUD.
type Service struct {
	store       interfaces.InventoryStore
	activitySvc *activity.Service
}

// NewService wires an InventoryStore and the activity Service used
// for fire-and-forget audit logs. activitySvc may be nil in tests
// or environments where activity logging is disabled.
func NewService(
	store interfaces.InventoryStore,
	activitySvc *activity.Service,
) *Service {
	return &Service{
		store:       store,
		activitySvc: activitySvc,
	}
}

// CreatePart inserts a new inventory part.
func (s *Service) CreatePart(ctx context.Context, params models.CreateParams) (*models.InventoryPart, error) {
	if params.Name == "" {
		return nil, fleeterror.NewInvalidArgumentError("name is required")
	}
	if params.Type == "" {
		return nil, fleeterror.NewInvalidArgumentError("type is required")
	}
	if params.OnHand < 0 {
		return nil, fleeterror.NewInvalidArgumentError("on_hand must be >= 0")
	}
	if params.ReorderPoint < 0 {
		return nil, fleeterror.NewInvalidArgumentError("reorder_point must be >= 0")
	}

	part, err := s.store.Create(ctx, params)
	if err != nil {
		return nil, err
	}

	// Activity log fires AFTER the write succeeds.
	if s.activitySvc != nil {
		orgID := params.OrgID
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventPartCreated,
			OrganizationID: &orgID,
			Description:    fmt.Sprintf("Created inventory part %q (id=%d)", part.Name, part.ID),
			Metadata: map[string]any{
				"part_id":   part.ID,
				"part_name": part.Name,
				"part_type": part.Type,
				"site_id":   part.SiteID,
			},
		}
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return part, nil
}

// GetPart returns the live part or NotFound.
func (s *Service) GetPart(ctx context.Context, orgID, id int64) (*models.InventoryPart, error) {
	return s.store.Get(ctx, orgID, id)
}

// ListParts returns the filtered parts list, cursor-paginated.
func (s *Service) ListParts(ctx context.Context, filter models.ListFilter) ([]models.InventoryPart, error) {
	if filter.Limit <= 0 {
		filter.Limit = ListDefaultLimit
	}
	if filter.Limit > ListMaxLimit {
		filter.Limit = ListMaxLimit
	}
	return s.store.List(ctx, filter)
}

// UpdatePart mutates the part's mutable fields.
func (s *Service) UpdatePart(ctx context.Context, params models.UpdateParams) (*models.InventoryPart, error) {
	if !params.Reason.Valid() {
		return nil, fleeterror.NewInvalidArgumentError("invalid adjustment_reason")
	}
	if params.OnHand != nil && *params.OnHand < 0 {
		return nil, fleeterror.NewInvalidArgumentError("on_hand must be >= 0")
	}
	if params.ReorderPoint != nil && *params.ReorderPoint < 0 {
		return nil, fleeterror.NewInvalidArgumentError("reorder_point must be >= 0")
	}

	part, err := s.store.Update(ctx, params)
	if err != nil {
		return nil, err
	}

	if s.activitySvc != nil {
		orgID := params.OrgID
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventPartUpdated,
			OrganizationID: &orgID,
			Description:    fmt.Sprintf("Updated inventory part %q (id=%d)", part.Name, part.ID),
			Metadata: map[string]any{
				"part_id":           part.ID,
				"part_name":         part.Name,
				"adjustment_reason": int16(params.Reason),
			},
		}
		if params.Notes != nil {
			event.Metadata["notes"] = *params.Notes
		}
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return part, nil
}

// DeletePart soft-deletes the inventory part.
func (s *Service) DeletePart(ctx context.Context, orgID, id int64) error {
	// Read the part before delete for the activity log.
	part, err := s.store.Get(ctx, orgID, id)
	if err != nil {
		return err
	}

	rowsAffected, err := s.store.SoftDelete(ctx, orgID, id)
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return fleeterror.NewNotFoundErrorf("inventory part %d not found", id)
	}

	if s.activitySvc != nil {
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventPartDeleted,
			OrganizationID: &orgID,
			Description:    fmt.Sprintf("Deleted inventory part %q (id=%d)", part.Name, part.ID),
			Metadata: map[string]any{
				"part_id":   part.ID,
				"part_name": part.Name,
			},
		}
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return nil
}

// GetInsights returns aggregate inventory stats for the org.
func (s *Service) GetInsights(ctx context.Context, orgID int64) (*models.InventoryInsights, error) {
	return s.store.GetInsights(ctx, orgID)
}

// ListPartsBySite returns in-stock parts at a given site for the
// repair ticket part picker.
func (s *Service) ListPartsBySite(ctx context.Context, orgID, siteID int64) ([]models.InventoryPart, error) {
	if siteID <= 0 {
		return nil, fleeterror.NewInvalidArgumentError("site_id must be > 0")
	}
	return s.store.ListPartsBySite(ctx, orgID, siteID)
}

// csvColumnIndex maps lowercase header names to their column index
// within the parsed CSV. Returns an error if required headers are
// missing.
type csvColumnIndex struct {
	name         int
	typ          int
	manufacturer int
	partNumber   int
	siteName     int
	onHand       int
	reorderPoint int
	binLocation  int
}

// maxCsvPreviewRows caps the number of rows returned in a preview
// response to avoid blowing up the response payload.
const maxCsvPreviewRows = 500

// ParseCsvPreview parses the raw CSV bytes and returns a preview
// table of rows with per-row validation errors. The first row is
// expected to be column headers.
func (s *Service) ParseCsvPreview(ctx context.Context, data []byte) ([]models.CsvPreviewRow, error) {
	_ = ctx // reserved for future site-name resolution

	reader := csv.NewReader(bytes.NewReader(data))
	reader.TrimLeadingSpace = true

	// Read the header row.
	headers, err := reader.Read()
	if err != nil {
		return nil, fleeterror.NewInvalidArgumentError("CSV is empty or has no header row")
	}

	idx, err := buildColumnIndex(headers)
	if err != nil {
		return nil, err
	}

	var rows []models.CsvPreviewRow
	rowNum := 1 // 1-indexed; header is row 0 conceptually
	for {
		record, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			// Surface parse errors as a preview row with an error message.
			rowNum++
			rows = append(rows, models.CsvPreviewRow{
				RowNumber: rowNum,
				Error:     fmt.Sprintf("parse error: %v", readErr),
			})
			continue
		}
		rowNum++
		row := parseCsvRow(record, idx, rowNum)
		rows = append(rows, row)
		if len(rows) >= maxCsvPreviewRows {
			break
		}
	}

	if len(rows) == 0 {
		return nil, fleeterror.NewInvalidArgumentError("CSV contains no data rows")
	}

	return rows, nil
}

// ConfirmCsvImport takes the validated preview rows and creates parts
// in bulk. Rows with non-empty Error are skipped. Returns the number
// of parts successfully created.
func (s *Service) ConfirmCsvImport(ctx context.Context, orgID int64, rows []models.CsvPreviewRow) (int32, error) {
	var created int32
	for _, row := range rows {
		if row.Error != "" {
			continue
		}
		params := models.CreateParams{
			OrgID:        orgID,
			Name:         row.Name,
			Type:         row.Type,
			OnHand:       row.OnHand,
			ReorderPoint: row.ReorderPoint,
		}
		if row.Manufacturer != "" {
			v := row.Manufacturer
			params.Manufacturer = &v
		}
		if row.PartNumber != "" {
			v := row.PartNumber
			params.PartNumber = &v
		}
		if row.BinLocation != "" {
			v := row.BinLocation
			params.BinLocation = &v
		}
		// SiteID resolution from SiteName is deferred to the store /
		// handler layer where site lookups are available. For now, the
		// import creates parts without a site assignment when SiteName
		// is provided.

		if _, err := s.store.Create(ctx, params); err != nil {
			return created, fmt.Errorf("row %d: %w", row.RowNumber, err)
		}
		created++
	}

	if s.activitySvc != nil && created > 0 {
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventPartsImported,
			OrganizationID: &orgID,
			Description:    fmt.Sprintf("Imported %d inventory parts from CSV", created),
			Metadata: map[string]any{
				"imported_count": created,
			},
		}
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return created, nil
}

// buildColumnIndex maps header names to their positional index.
func buildColumnIndex(headers []string) (*csvColumnIndex, error) {
	idx := &csvColumnIndex{
		name:         -1,
		typ:          -1,
		manufacturer: -1,
		partNumber:   -1,
		siteName:     -1,
		onHand:       -1,
		reorderPoint: -1,
		binLocation:  -1,
	}
	for i, h := range headers {
		switch strings.ToLower(strings.TrimSpace(h)) {
		case csvHeaderName:
			idx.name = i
		case csvHeaderType:
			idx.typ = i
		case csvHeaderManufacturer:
			idx.manufacturer = i
		case csvHeaderPartNumber, "part number":
			idx.partNumber = i
		case csvHeaderSiteName, "site_name", "site name":
			idx.siteName = i
		case csvHeaderOnHand, "on_hand":
			idx.onHand = i
		case csvHeaderReorderPoint, "reorder_point", "reorder point":
			idx.reorderPoint = i
		case csvHeaderBinLocation, "bin_location", "bin location":
			idx.binLocation = i
		}
	}

	var missing []string
	if idx.name < 0 {
		missing = append(missing, csvHeaderName)
	}
	if idx.typ < 0 {
		missing = append(missing, csvHeaderType)
	}
	if len(missing) > 0 {
		return nil, fleeterror.NewInvalidArgumentErrorf("CSV missing required columns: %s", strings.Join(missing, ", "))
	}
	return idx, nil
}

// parseCsvRow extracts field values from a single CSV record using
// the resolved column index.
func parseCsvRow(record []string, idx *csvColumnIndex, rowNum int) models.CsvPreviewRow {
	row := models.CsvPreviewRow{RowNumber: rowNum}
	get := func(col int) string {
		if col < 0 || col >= len(record) {
			return ""
		}
		return strings.TrimSpace(record[col])
	}

	row.Name = get(idx.name)
	row.Type = get(idx.typ)
	row.Manufacturer = get(idx.manufacturer)
	row.PartNumber = get(idx.partNumber)
	row.SiteName = get(idx.siteName)
	row.BinLocation = get(idx.binLocation)

	// Parse numeric fields.
	if v := get(idx.onHand); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			row.Error = fmt.Sprintf("invalid on_hand: %q", v)
			return row
		}
		row.OnHand = int32(n) //nolint:gosec // bounded by ParseInt bitSize=32
	}
	if v := get(idx.reorderPoint); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			row.Error = fmt.Sprintf("invalid reorder_point: %q", v)
			return row
		}
		row.ReorderPoint = int32(n) //nolint:gosec // bounded by ParseInt bitSize=32
	}

	// Validation.
	if row.Name == "" {
		row.Error = "name is required"
	} else if row.Type == "" {
		row.Error = "type is required"
	} else if row.OnHand < 0 {
		row.Error = "on_hand must be >= 0"
	} else if row.ReorderPoint < 0 {
		row.Error = "reorder_point must be >= 0"
	}

	return row
}
