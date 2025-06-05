package testutil

import (
	"context"
	"database/sql"
	"github.com/alecthomas/assert/v2"
	"github.com/btc-mining/proto-fleet/server/generated/sqlc"
	"github.com/btc-mining/proto-fleet/server/internal/domain/fleeterror"
	db2 "github.com/btc-mining/proto-fleet/server/internal/infrastructure/db"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"strconv"
	"testing"
	"time"
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
			Name:  organizationName,
			OrgID: organizationName,
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
