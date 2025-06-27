package testutil

import (
	"context"
	"database/sql"
	"net"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	"github.com/btc-mining/proto-fleet/server/internal/domain/miner/models"
	db2 "github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
	id "github.com/btc-mining/proto-fleet/server/internal/infrastructure/id"
	"golang.org/x/crypto/bcrypt"
)

type DatabaseService struct {
	DB     *sql.DB
	t      *testing.T
	config *Config
}

func NewDatabaseService(t *testing.T, config *Config) *DatabaseService {
	db := GetTestDB(t)
	return &DatabaseService{DB: db, t: t, config: config}
}

type TestUser struct {
	Username       string
	Password       string
	OrganizationID int64
	DatabaseID     int64
}

type DeviceIdentification struct {
	DatabaseID int64
	ID         string
}

func (s *DatabaseService) CreateSuperAdminUser() *TestUser {
	username := "alice@example.com"
	password := "fizzbuzz"
	organizationName := "Super organization 1"
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	assert.NoError(s.t, err, "could not hash pass")

	externalUserID := id.GenerateID()

	var testUser TestUser
	testUser.Username = username
	testUser.Password = password

	err = db2.WithTransactionNoResult(context.Background(), s.DB, func(q *sqlc.Queries) error {
		userResult, err := q.CreateUser(context.Background(), sqlc.CreateUserParams{
			UserID:       externalUserID,
			Username:     username,
			PasswordHash: string(hashedPassword),
			CreatedAt:    time.Now(),
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("error creating user: %v", err)
		}

		userID, err := userResult.LastInsertId()
		if err != nil {
			return fleeterror.NewInternalErrorf("error getting last inserted id: %v", err)
		}
		testUser.DatabaseID = userID

		orgResult, err := q.CreateOrganization(context.Background(), sqlc.CreateOrganizationParams{
			Name:                organizationName,
			OrgID:               organizationName,
			MinerAuthPrivateKey: s.config.MinerAuthPrivateKey,
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("error creating organization: %v", err)
		}

		orgID, err := orgResult.LastInsertId()
		if err != nil {
			return fleeterror.NewInternalErrorf("error getting org id: %v", err)
		}
		roleResult, err := q.UpsertRole(context.Background(), sqlc.UpsertRoleParams{
			Name: "SUPER_ADMIN",
			Description: sql.NullString{
				String: "Super admin role for testing",
				Valid:  true,
			},
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("error creating role: %v", err)
		}

		roleID, err := roleResult.LastInsertId()
		if err != nil {
			return fleeterror.NewInternalErrorf("error getting role id: %v", err)
		}

		err = q.CreateUserOrganization(context.Background(), sqlc.CreateUserOrganizationParams{
			UserID:         userID,
			RoleID:         roleID,
			OrganizationID: orgID,
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("error associating user with org: %v", err)
		}
		testUser.OrganizationID = orgID

		return nil
	})
	assert.NoError(s.t, err, "db transaction error")

	return &testUser
}

func (s *DatabaseService) CreateDevice(organizationID int64, minerType models.Type) DeviceIdentification {
	uuidCurrent := id.GenerateID()
	deviceIdentification, err := db2.WithTransaction(context.Background(), s.DB, func(q *sqlc.Queries) (DeviceIdentification, error) {
		result, err := q.UpsertDevice(context.Background(), sqlc.UpsertDeviceParams{
			OrgID:            organizationID,
			DeviceIdentifier: uuidCurrent,
			MacAddress:       "00-1A-2B-3C-4D-5E",
			Type:             minerType.String(),
		})
		if err != nil {
			return DeviceIdentification{}, fleeterror.NewInternalErrorf("failed to create device: %v", err)
		}
		dbID, err := result.LastInsertId()
		if err != nil {
			return DeviceIdentification{}, fleeterror.NewInternalErrorf("failed to query last insert ID: %v", err)
		}

		return DeviceIdentification{
			DatabaseID: dbID,
			ID:         uuidCurrent,
		}, nil
	})
	assert.NoError(s.t, err)
	return deviceIdentification
}

func (s *DatabaseService) CreateDeviceIPAssignment(deviceID int64, ipAddress string, port string) {
	err := db2.WithTransactionNoResult(context.Background(), s.DB, func(q *sqlc.Queries) error {
		err := q.CreateInactiveDeviceIPAssignment(context.Background(), sqlc.CreateInactiveDeviceIPAssignmentParams{
			DeviceID:  deviceID,
			IpAddress: ipAddress,
			Port:      port,
		})
		if err != nil {
			return err
		}

		return q.ActivateNewIPAssignment(context.Background(), sqlc.ActivateNewIPAssignmentParams{
			DeviceID:  deviceID,
			IpAddress: ipAddress,
			Port:      port,
		})
	})
	assert.NoError(s.t, err)
}

func (s *DatabaseService) GetDevicePairingByDeviceIdentifier(databaseDeviceID int64) (sqlc.DevicePairingPairingStatus, error) {
	return db2.WithTransaction(context.Background(), s.DB, func(q *sqlc.Queries) (sqlc.DevicePairingPairingStatus, error) {
		return q.GetDevicePairingStatusByDeviceDatabaseID(context.Background(), databaseDeviceID)
	})
}

func (s *DatabaseService) GetTotalDevicePairings(orgID int64, limit int32) (int, error) {
	return db2.WithTransaction(context.Background(), s.DB, func(q *sqlc.Queries) (int, error) {
		pairings, err := q.ListPairedMinersWithStatus(context.Background(), sqlc.ListPairedMinersWithStatusParams{
			OrgID: orgID,
			Limit: limit,
		})
		if err != nil {
			return 0, err
		}
		return len(pairings), nil
	})
}

func (s *DatabaseService) CreateAndAssignDevices(count int, organizationID int64) []DeviceIdentification {
	deviceIdentifications := make([]DeviceIdentification, 0)
	for i := range count {
		deviceIdentification := s.CreateDevice(organizationID, models.TypeProto)
		s.CreateDeviceIPAssignment(deviceIdentification.DatabaseID, "127.0.0.1", strconv.Itoa(i))
		deviceIdentifications = append(deviceIdentifications, deviceIdentification)
	}
	return deviceIdentifications
}

func (s *DatabaseService) CreateTestMiners(orgID int64, count int, mockMinerURL string) []string {
	u, err := url.Parse(mockMinerURL)
	assert.NoError(s.t, err)

	host, portStr, err := net.SplitHostPort(u.Host)
	assert.NoError(s.t, err)

	s.t.Logf("Setting up %d test miners with host=%s, port=%s", count, host, portStr)

	deviceIDs := make([]string, count)

	// Create miners in the database
	for i := range count {
		device := s.CreateDevice(orgID, models.TypeProto)
		deviceIDs[i] = device.ID

		s.CreateDeviceIPAssignment(device.DatabaseID, host, portStr)

		err := db2.WithTransactionNoResult(s.t.Context(), s.DB, func(q *sqlc.Queries) error {
			_, err := q.UpsertDevicePairing(s.t.Context(), sqlc.UpsertDevicePairingParams{
				DeviceID:      device.DatabaseID,
				PairingToken:  sql.NullString{String: "test-token", Valid: true},
				PairingStatus: sqlc.DevicePairingPairingStatusPAIRED,
			})
			return err
		})
		assert.NoError(s.t, err)

		s.t.Logf("Created test miner with ID: %s", device.ID)
	}

	return deviceIDs
}
