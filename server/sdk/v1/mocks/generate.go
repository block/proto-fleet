//go:generate go run go.uber.org/mock/mockgen -source=../interface.go -destination=mock_driver.go -package=mocks Driver,Device,DefaultCredentialsProvider,ModelCapabilitiesProvider

package mocks

// This file exists to trigger mock generation for SDK interfaces
// Run: go generate ./server/sdk/v1/mocks
