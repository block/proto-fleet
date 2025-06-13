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
	db2 "github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type DatabaseService struct {
	DB *sql.DB
	t  *testing.T
}

func NewDatabaseService(t *testing.T) *DatabaseService {
	db := GetTestDB(t)
	return &DatabaseService{DB: db, t: t}
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
	minerAuthPrivateKey := "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"
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	assert.NoError(s.t, err, "could not hash pass")

	externalUserID := uuid.New().String()

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
			MinerAuthPrivateKey: minerAuthPrivateKey,
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

func (s *DatabaseService) CreateDevice(organizationID int64) DeviceIdentification {
	uuidCurrent := uuid.New().String()
	deviceIdentification, err := db2.WithTransaction[DeviceIdentification](context.Background(), s.DB, func(q *sqlc.Queries) (DeviceIdentification, error) {
		result, err := q.UpsertDevice(context.Background(), sqlc.UpsertDeviceParams{
			OrgID:            organizationID,
			DeviceIdentifier: uuidCurrent,
			MacAddress:       "00-1A-2B-3C-4D-5E",
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
		_, err := q.CreateDeviceIPAssignment(context.Background(), sqlc.CreateDeviceIPAssignmentParams{
			DeviceID:  deviceID,
			IpAddress: ipAddress,
			Port:      port,
		})
		return err
	})
	assert.NoError(s.t, err)
}

func (s *DatabaseService) GetDevicePairingByDeviceIdentifier(databaseDeviceID int64) (sqlc.DevicePairingPairingStatus, error) {
	return db2.WithTransaction(context.Background(), s.DB, func(q *sqlc.Queries) (sqlc.DevicePairingPairingStatus, error) {
		return q.GetDevicePairingStatusByDeviceDatabaseID(context.Background(), databaseDeviceID)
	})
}

func (s *DatabaseService) CreateAndAssignDevices(count int, organizationID int64) []DeviceIdentification {
	deviceIdentifications := make([]DeviceIdentification, 0)
	for i := range count {
		deviceIdentification := s.CreateDevice(organizationID)
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
		device := s.CreateDevice(orgID)
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
