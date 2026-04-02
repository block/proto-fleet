package testutil

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
	"github.com/block/proto-fleet/server/generated/sqlc"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	db2 "github.com/block/proto-fleet/server/internal/infrastructure/db"
	id "github.com/block/proto-fleet/server/internal/infrastructure/id"
	"github.com/block/proto-fleet/server/internal/infrastructure/networking"
	"golang.org/x/crypto/bcrypt"
)

// Global counter for generating unique test IPs
// Using atomic operations ensures uniqueness even in parallel tests
var testDeviceIPCounter uint64

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
		userID, err := q.CreateUser(context.Background(), sqlc.CreateUserParams{
			UserID:       externalUserID,
			Username:     username,
			PasswordHash: string(hashedPassword),
			CreatedAt:    time.Now(),
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("error creating user: %v", err)
		}
		testUser.DatabaseID = userID

		orgID, err := q.CreateOrganization(context.Background(), sqlc.CreateOrganizationParams{
			Name:                organizationName,
			OrgID:               organizationName,
			MinerAuthPrivateKey: s.config.MinerAuthPrivateKey,
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("error creating organization: %v", err)
		}

		roleID, err := q.UpsertRole(context.Background(), sqlc.UpsertRoleParams{
			Name: "SUPER_ADMIN",
			Description: sql.NullString{
				String: "Super admin role for testing",
				Valid:  true,
			},
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("error creating role: %v", err)
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

// CreateSuperAdminUser2 creates a second test user in a different organization.
// Use this when testing cross-organization authorization.
func (s *DatabaseService) CreateSuperAdminUser2() *TestUser {
	username := "bob@example.com"
	password := "password123"
	organizationName := "Super organization 2"
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	assert.NoError(s.t, err, "could not hash pass")

	externalUserID := id.GenerateID()

	var testUser TestUser
	testUser.Username = username
	testUser.Password = password

	err = db2.WithTransactionNoResult(context.Background(), s.DB, func(q *sqlc.Queries) error {
		userID, err := q.CreateUser(context.Background(), sqlc.CreateUserParams{
			UserID:       externalUserID,
			Username:     username,
			PasswordHash: string(hashedPassword),
			CreatedAt:    time.Now(),
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("error creating user: %v", err)
		}
		testUser.DatabaseID = userID

		orgID, err := q.CreateOrganization(context.Background(), sqlc.CreateOrganizationParams{
			Name:                organizationName,
			OrgID:               organizationName,
			MinerAuthPrivateKey: s.config.MinerAuthPrivateKey,
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("error creating organization: %v", err)
		}

		roleID, err := q.UpsertRole(context.Background(), sqlc.UpsertRoleParams{
			Name: "SUPER_ADMIN",
			Description: sql.NullString{
				String: "Super admin role for testing",
				Valid:  true,
			},
		})
		if err != nil {
			return fleeterror.NewInternalErrorf("error creating role: %v", err)
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

func (s *DatabaseService) CreateDevice(organizationID int64, driverName string) DeviceIdentification {
	uuidCurrent := id.GenerateID()
	deviceIdentification, err := db2.WithTransaction(context.Background(), s.DB, func(q *sqlc.Queries) (DeviceIdentification, error) {
		// Use unique IP per device to prevent constraint violations on (org_id, ip_address, port)
		// Use atomic counter to ensure true uniqueness even in parallel tests
		ipSuffix := atomic.AddUint64(&testDeviceIPCounter, 1)
		ipAddress := fmt.Sprintf("127.0.%d.%d", (ipSuffix/256)%256, ipSuffix%256)

		port := "4028"
		if driverName == "proto" {
			port = "8080"
		}

		discoveredDeviceID, err := q.UpsertDiscoveredDevice(context.Background(), sqlc.UpsertDiscoveredDeviceParams{
			OrgID:            organizationID,
			DeviceIdentifier: uuidCurrent,
			Model:            sql.NullString{String: "TestMiner", Valid: true},
			Manufacturer:     sql.NullString{String: "TestCorp", Valid: true},
			IpAddress:        ipAddress,
			Port:             port,
			UrlScheme:        "https",
			IsActive:         true,
			DriverName:       driverName,
		})
		if err != nil {
			return DeviceIdentification{}, fleeterror.NewInternalErrorf("failed to create discovered device: %v", err)
		}

		dbID, err := q.InsertDevice(context.Background(), sqlc.InsertDeviceParams{
			OrgID:              organizationID,
			DiscoveredDeviceID: discoveredDeviceID,
			DeviceIdentifier:   uuidCurrent,
			MacAddress:         "00-1A-2B-3C-4D-5E",
		})
		if err != nil {
			return DeviceIdentification{}, fleeterror.NewInternalErrorf("failed to create device: %v", err)
		}

		return DeviceIdentification{
			DatabaseID: dbID,
			ID:         uuidCurrent,
		}, nil
	})
	assert.NoError(s.t, err)
	return deviceIdentification
}

func (s *DatabaseService) createDeviceIPAssignment(deviceID int64, ipAddress string, port string, urlScheme networking.Protocol) {
	err := db2.WithTransactionNoResult(context.Background(), s.DB, func(q *sqlc.Queries) error {
		return q.UpdateDeviceIPAssignment(context.Background(), sqlc.UpdateDeviceIPAssignmentParams{
			IpAddress: ipAddress,
			Port:      port,
			UrlScheme: urlScheme.String(),
			ID:        deviceID,
		})
	})
	assert.NoError(s.t, err)
}

func (s *DatabaseService) GetDevicePairingByDeviceIdentifier(databaseDeviceID int64) (sqlc.PairingStatusEnum, error) {
	return db2.WithTransaction(context.Background(), s.DB, func(q *sqlc.Queries) (sqlc.PairingStatusEnum, error) {
		return q.GetDevicePairingStatusByDeviceDatabaseID(context.Background(), databaseDeviceID)
	})
}

func (s *DatabaseService) GetTotalDevicePairings(orgID int64, _ int32) (int, error) {
	return db2.WithTransaction(context.Background(), s.DB, func(q *sqlc.Queries) (int, error) {
		count, err := q.GetTotalPairedDevices(context.Background(), sqlc.GetTotalPairedDevicesParams{
			OrgID: orgID,
		})
		if err != nil {
			return 0, err
		}
		return int(count), nil
	})
}

func (s *DatabaseService) CreateAndAssignDevices(count int, organizationID int64) []DeviceIdentification {
	deviceIdentifications := make([]DeviceIdentification, 0)
	for i := range count {
		deviceIdentification := s.CreateDevice(organizationID, "proto")
		s.createDeviceIPAssignment(deviceIdentification.DatabaseID, "127.0.0.1", strconv.Itoa(i), networking.ProtocolHTTPS)
		deviceIdentifications = append(deviceIdentifications, deviceIdentification)
	}
	return deviceIdentifications
}

func (s *DatabaseService) CreateTestMiners(orgID int64, count int, mockMinerURL string) []string {
	u, err := url.Parse(mockMinerURL)
	assert.NoError(s.t, err)

	protocol, err := networking.ProtocolFromString(u.Scheme)
	assert.NoError(s.t, err)

	host, portStr, err := net.SplitHostPort(u.Host)
	assert.NoError(s.t, err)

	s.t.Logf("Setting up %d test miners with host=%s, port=%s", count, host, portStr)

	driverName := "proto"
	if portStr == "4028" {
		driverName = "antminer"
	}

	deviceIDs := make([]string, count)

	// Create miners in the database
	for i := range count {
		device := s.CreateDevice(orgID, driverName)
		deviceIDs[i] = device.ID

		// Make each device have a unique IP to avoid constraint violations
		// Port remains constant for the device type (e.g., 80 for Proto, 4028 for Antminer)
		// Increment the last octet of the IP address for each device
		uniqueHost := host
		if count > 1 {
			ip := net.ParseIP(host)
			if ip != nil && ip.To4() != nil {
				ip4 := ip.To4()
				newOctet := int(ip4[3]) + i
				if newOctet > 255 {
					s.t.Fatalf("IP address overflow: starting IP %s cannot accommodate %d devices", host, count)
				}
				ip4[3] = byte(newOctet)
				uniqueHost = ip4.String()
			} else {
				// Fallback for non-IPv4 addresses
				uniqueHost = fmt.Sprintf("%s-%d", host, i)
			}
		}
		s.createDeviceIPAssignment(device.DatabaseID, uniqueHost, portStr, protocol)

		err = db2.WithTransactionNoResult(s.t.Context(), s.DB, func(q *sqlc.Queries) error {
			_, err := q.UpsertDevicePairing(s.t.Context(), sqlc.UpsertDevicePairingParams{
				DeviceID:      device.DatabaseID,
				PairingStatus: sqlc.PairingStatusEnumPAIRED,
			})
			return err
		})
		assert.NoError(s.t, err)

		s.t.Logf("Created test miner with ID: %s at %s:%s", device.ID, uniqueHost, portStr)
	}

	return deviceIDs
}
