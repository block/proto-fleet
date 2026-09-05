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
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/block/proto-fleet/server/internal/domain/activity"
	activitymodels "github.com/block/proto-fleet/server/internal/domain/activity/models"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/inventory/models"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
)

// Event type constants for inventory activity logs.
const (
	eventPartCreated   = "inventory.part_created"
	eventPartUpdated   = "inventory.part_updated"
	eventPartDeleted   = "inventory.part_deleted"
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
	transactor  interfaces.Transactor
	activitySvc *activity.Service
}

// NewService wires inventory persistence, transaction ownership, and activity
// logging. activitySvc may be nil where audit persistence is disabled.
func NewService(
	store interfaces.InventoryStore,
	transactor interfaces.Transactor,
	activitySvc *activity.Service,
) *Service {
	return &Service{
		store:       store,
		transactor:  transactor,
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

	var part *models.InventoryPart
	if params.SiteID == nil {
		var err error
		part, err = s.store.Create(ctx, params)
		if err != nil {
			return nil, err
		}
	} else {
		if s.transactor == nil {
			return nil, fleeterror.NewInternalError("inventory transactor is not configured")
		}
		result, err := s.transactor.RunInTxWithResult(ctx, func(txCtx context.Context) (any, error) {
			if err := s.store.LockSites(txCtx, params.OrgID, []int64{*params.SiteID}); err != nil {
				return nil, err
			}
			return s.store.Create(txCtx, params)
		})
		if err != nil {
			return nil, err
		}
		var ok bool
		part, ok = result.(*models.InventoryPart)
		if !ok || part == nil {
			return nil, fleeterror.NewInternalError("create inventory transaction returned an invalid result")
		}
	}

	// Activity log fires AFTER the write succeeds.
	if s.activitySvc != nil {
		orgID := params.OrgID
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventPartCreated,
			OrganizationID: &orgID,
			SiteID:         part.SiteID,
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

// ListParts returns one filtered cursor page and the total matching count.
func (s *Service) ListParts(ctx context.Context, filter models.ListFilter) (*models.InventoryPage, error) {
	if filter.Limit <= 0 {
		filter.Limit = ListDefaultLimit
	}
	if filter.Limit > ListMaxLimit {
		filter.Limit = ListMaxLimit
	}

	totalCount, err := s.store.Count(ctx, filter)
	if err != nil {
		return nil, err
	}
	pageSize := filter.Limit
	filter.Limit++
	parts, err := s.store.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	page := &models.InventoryPage{Parts: parts, TotalCount: totalCount}
	if len(parts) > int(pageSize) {
		page.Parts = parts[:pageSize]
		cursor := page.Parts[len(page.Parts)-1].ID
		page.NextCursorID = &cursor
	}
	return page, nil
}

// UpdatePart locks, validates, and mutates a part in one transaction.
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
	if params.OnHand == nil && params.ReorderPoint == nil && params.BinLocation == nil && params.SiteID == nil {
		return nil, fleeterror.NewInvalidArgumentError("at least one inventory field must be updated")
	}
	if s.transactor == nil {
		return nil, fleeterror.NewInternalError("inventory transactor is not configured")
	}

	var before, after *models.InventoryPart
	err := s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		if params.SiteID != nil {
			if err := s.store.LockSites(txCtx, params.OrgID, []int64{*params.SiteID}); err != nil {
				return err
			}
		}
		var err error
		before, err = s.store.GetForUpdate(txCtx, params.OrgID, params.ID)
		if err != nil {
			return err
		}
		if params.OnHand != nil && *params.OnHand < before.Allocated {
			return fleeterror.NewFailedPreconditionError("on_hand cannot be less than allocated stock")
		}
		if params.SiteID != nil && !sameOptionalID(before.SiteID, params.SiteID) && before.Allocated > 0 {
			return fleeterror.NewFailedPreconditionError("site cannot be changed while stock is allocated")
		}
		after, err = s.store.Update(txCtx, params)
		return err
	})
	if err != nil {
		return nil, err
	}

	if s.activitySvc != nil {
		orgID := params.OrgID
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventPartUpdated,
			OrganizationID: &orgID,
			Description:    fmt.Sprintf("Updated inventory part %q (id=%d)", after.Name, after.ID),
			Metadata: map[string]any{
				"part_id":           after.ID,
				"part_name":         after.Name,
				"adjustment_reason": int16(params.Reason),
				"old_on_hand":       before.OnHand,
				"new_on_hand":       after.OnHand,
				"old_allocated":     before.Allocated,
				"new_allocated":     after.Allocated,
				"old_reorder_point": before.ReorderPoint,
				"new_reorder_point": after.ReorderPoint,
				"old_site_id":       before.SiteID,
				"new_site_id":       after.SiteID,
			},
		}
		event.ApplySiteScope(activitymodels.ResolveSiteScope([]*int64{before.SiteID, after.SiteID}))
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return after, nil
}

// DeletePart locks and soft-deletes an unallocated inventory part.
func (s *Service) DeletePart(ctx context.Context, orgID, id int64) error {
	if s.transactor == nil {
		return fleeterror.NewInternalError("inventory transactor is not configured")
	}
	var part *models.InventoryPart
	err := s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		var err error
		part, err = s.store.GetForUpdate(txCtx, orgID, id)
		if err != nil {
			return err
		}
		if part.Allocated > 0 {
			return fleeterror.NewFailedPreconditionError("allocated inventory part cannot be deleted")
		}
		rowsAffected, err := s.store.SoftDelete(txCtx, orgID, id)
		if err != nil {
			return err
		}
		if rowsAffected == 0 {
			return fleeterror.NewNotFoundErrorf("inventory part %d not found", id)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if s.activitySvc != nil {
		event := activitymodels.Event{
			Category:       activitymodels.CategoryFleetManagement,
			Type:           eventPartDeleted,
			OrganizationID: &orgID,
			SiteID:         part.SiteID,
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

// maxCsvPreviewRows is a hard import limit. Oversized files are rejected rather
// than truncated so preview and confirmation always describe the same payload.
const maxCsvPreviewRows = 500

// ParseCsvPreview parses and validates all CSV rows, including organization-
// scoped site resolution. It never writes inventory.
func (s *Service) ParseCsvPreview(ctx context.Context, orgID int64, data []byte) ([]models.CsvPreviewRow, error) {
	rows, _, err := s.parseAndResolveCsv(ctx, orgID, data)
	return rows, err
}

// ConfirmCsvImport reparses the exact submitted bytes and commits every row in
// one transaction. A single invalid row or insert failure leaves inventory
// unchanged.
func (s *Service) ConfirmCsvImport(ctx context.Context, orgID int64, data []byte) (int32, error) {
	preview, resolved, err := s.parseAndResolveCsv(ctx, orgID, data)
	if err != nil {
		return 0, err
	}
	var invalidRows int
	for _, row := range preview {
		if row.Error != "" {
			invalidRows++
		}
	}
	if invalidRows > 0 {
		return 0, fleeterror.NewInvalidArgumentErrorf("CSV contains %d invalid row(s)", invalidRows)
	}
	if s.transactor == nil {
		return 0, fleeterror.NewInternalError("inventory transactor is not configured")
	}

	siteIDs := distinctSortedSiteIDs(resolved)
	var created int32
	err = s.transactor.RunInTx(ctx, func(txCtx context.Context) error {
		if err := s.store.LockSites(txCtx, orgID, siteIDs); err != nil {
			return err
		}
		var createErr error
		created, createErr = s.store.BulkCreate(txCtx, orgID, resolved)
		return createErr
	})
	if err != nil {
		return 0, err
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
		importSiteIDs := make([]*int64, 0, len(resolved))
		for _, row := range resolved {
			importSiteIDs = append(importSiteIDs, row.SiteID)
		}
		event.ApplySiteScope(activitymodels.ResolveSiteScope(importSiteIDs))
		activity.StampActor(ctx, &event)
		s.activitySvc.Log(ctx, event)
	}

	return created, nil
}

func distinctSortedSiteIDs(rows []models.ResolvedCsvRow) []int64 {
	unique := make(map[int64]struct{})
	for _, row := range rows {
		if row.SiteID != nil {
			unique[*row.SiteID] = struct{}{}
		}
	}
	siteIDs := make([]int64, 0, len(unique))
	for siteID := range unique {
		siteIDs = append(siteIDs, siteID)
	}
	sort.Slice(siteIDs, func(i, j int) bool { return siteIDs[i] < siteIDs[j] })
	return siteIDs
}

func (s *Service) parseAndResolveCsv(ctx context.Context, orgID int64, data []byte) ([]models.CsvPreviewRow, []models.ResolvedCsvRow, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	reader.TrimLeadingSpace = true

	headers, err := reader.Read()
	if err != nil {
		return nil, nil, fleeterror.NewInvalidArgumentError("CSV is empty or has no header row")
	}
	idx, err := buildColumnIndex(headers)
	if err != nil {
		return nil, nil, err
	}

	preview := make([]models.CsvPreviewRow, 0)
	resolved := make([]models.ResolvedCsvRow, 0)
	seen := make(map[string]struct{})
	rowNum := 1
	for {
		record, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		rowNum++
		if len(preview) == maxCsvPreviewRows {
			return nil, nil, fleeterror.NewInvalidArgumentErrorf("CSV exceeds the %d data row limit", maxCsvPreviewRows)
		}
		if readErr != nil {
			preview = append(preview, models.CsvPreviewRow{
				RowNumber: rowNum,
				Error:     fmt.Sprintf("parse error: %v", readErr),
			})
			continue
		}

		row := parseCsvRow(record, idx, rowNum)
		var siteID *int64
		if row.Error == "" && row.SiteName != "" {
			resolvedID, resolveErr := s.store.ResolveSiteByName(ctx, orgID, row.SiteName)
			if resolveErr != nil {
				if !fleeterror.IsNotFoundError(resolveErr) {
					return nil, nil, resolveErr
				}
				row.Error = fmt.Sprintf("site_name %q was not found in this organization", row.SiteName)
			} else {
				siteID = &resolvedID
			}
		}
		if row.Error == "" {
			key := fmt.Sprintf("%d\x00%s", valueOrZero(siteID), strings.ToLower(strings.TrimSpace(row.Name)))
			if _, exists := seen[key]; exists {
				row.Error = "duplicate name for site in CSV"
			} else {
				seen[key] = struct{}{}
				exists, existsErr := s.store.PartExistsBySiteAndName(ctx, orgID, siteID, row.Name)
				if existsErr != nil {
					return nil, nil, existsErr
				}
				if exists {
					row.Error = "inventory part with this name and site already exists"
				}
			}
		}
		preview = append(preview, row)
		if row.Error == "" {
			resolved = append(resolved, resolvedCsvRow(row, siteID))
		}
	}
	if len(preview) == 0 {
		return nil, nil, fleeterror.NewInvalidArgumentError("CSV contains no data rows")
	}
	return preview, resolved, nil
}

func resolvedCsvRow(row models.CsvPreviewRow, siteID *int64) models.ResolvedCsvRow {
	return models.ResolvedCsvRow{
		RowNumber:    row.RowNumber,
		Name:         strings.TrimSpace(row.Name),
		Type:         strings.TrimSpace(row.Type),
		Manufacturer: optionalCsvString(row.Manufacturer),
		PartNumber:   optionalCsvString(row.PartNumber),
		SiteID:       siteID,
		OnHand:       row.OnHand,
		ReorderPoint: row.ReorderPoint,
		BinLocation:  optionalCsvString(row.BinLocation),
	}
}

func optionalCsvString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func valueOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func sameOptionalID(left, right *int64) bool {
	return valueOrZero(left) == valueOrZero(right)
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
		case csvHeaderSiteName, "site name":
			idx.siteName = i
		case csvHeaderOnHand:
			idx.onHand = i
		case csvHeaderReorderPoint, "reorder point":
			idx.reorderPoint = i
		case csvHeaderBinLocation, "bin location":
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
	} else if utf8.RuneCountInString(row.Name) > 255 {
		row.Error = "name must be at most 255 characters"
	} else if row.Type == "" {
		row.Error = "type is required"
	} else if utf8.RuneCountInString(row.Type) > 64 {
		row.Error = "type must be at most 64 characters"
	} else if utf8.RuneCountInString(row.Manufacturer) > 255 {
		row.Error = "manufacturer must be at most 255 characters"
	} else if utf8.RuneCountInString(row.PartNumber) > 128 {
		row.Error = "part_number must be at most 128 characters"
	} else if utf8.RuneCountInString(row.BinLocation) > 64 {
		row.Error = "bin_location must be at most 64 characters"
	} else if row.OnHand < 0 {
		row.Error = "on_hand must be >= 0"
	} else if row.ReorderPoint < 0 {
		row.Error = "reorder_point must be >= 0"
	}

	return row
}
