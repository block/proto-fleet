package deviceresolver

import (
	"context"
	"errors"
	"testing"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/stores/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockStore struct {
	allBelong    bool
	allBelongErr error
	allBelongIDs []string // captures the IDs passed to AllDevicesBelongToOrg
	filterIDs    []string
	filterErr    error
}

func (m *mockStore) AllDevicesBelongToOrg(_ context.Context, deviceIdentifiers []string, _ int64) (bool, error) {
	m.allBelongIDs = deviceIdentifiers
	return m.allBelong, m.allBelongErr
}

func (m *mockStore) GetDeviceIdentifiersByOrgWithFilter(_ context.Context, _ int64, _ *interfaces.MinerFilter) ([]string, error) {
	return m.filterIDs, m.filterErr
}

func TestResolve_NilSelector(t *testing.T) {
	resolver := New(&mockStore{})

	// Act
	_, err := resolver.Resolve(context.Background(), nil, 1)

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestResolve_EmptySelectionType(t *testing.T) {
	resolver := New(&mockStore{})

	// Act
	_, err := resolver.Resolve(context.Background(), &commonpb.DeviceSelector{}, 1)

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestResolve_DeviceList_Success(t *testing.T) {
	store := &mockStore{allBelong: true}
	resolver := New(store)

	// Act
	ids, err := resolver.Resolve(context.Background(), &commonpb.DeviceSelector{
		SelectionType: &commonpb.DeviceSelector_DeviceList{
			DeviceList: &commonpb.DeviceIdentifierList{
				DeviceIdentifiers: []string{"dev-1", "dev-2"},
			},
		},
	}, 1)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, []string{"dev-1", "dev-2"}, ids)
}

func TestResolve_DeviceList_Deduplicates(t *testing.T) {
	store := &mockStore{allBelong: true}
	resolver := New(store)

	// Act
	ids, err := resolver.Resolve(context.Background(), &commonpb.DeviceSelector{
		SelectionType: &commonpb.DeviceSelector_DeviceList{
			DeviceList: &commonpb.DeviceIdentifierList{
				DeviceIdentifiers: []string{"dev-1", "dev-2", "dev-1", "dev-3", "dev-2"},
			},
		},
	}, 1)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, []string{"dev-1", "dev-2", "dev-3"}, ids)
	assert.Equal(t, []string{"dev-1", "dev-2", "dev-3"}, store.allBelongIDs)
}

func TestResolve_DeviceList_ForbiddenWhenNotOwned(t *testing.T) {
	store := &mockStore{allBelong: false}
	resolver := New(store)

	// Act
	_, err := resolver.Resolve(context.Background(), &commonpb.DeviceSelector{
		SelectionType: &commonpb.DeviceSelector_DeviceList{
			DeviceList: &commonpb.DeviceIdentifierList{
				DeviceIdentifiers: []string{"dev-1"},
			},
		},
	}, 1)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access denied")
}

func TestResolve_DeviceList_PropagatesStoreError(t *testing.T) {
	store := &mockStore{allBelongErr: errors.New("db failure")}
	resolver := New(store)

	// Act
	_, err := resolver.Resolve(context.Background(), &commonpb.DeviceSelector{
		SelectionType: &commonpb.DeviceSelector_DeviceList{
			DeviceList: &commonpb.DeviceIdentifierList{
				DeviceIdentifiers: []string{"dev-1"},
			},
		},
	}, 1)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db failure")
}

func TestResolve_DeviceList_EmptyList(t *testing.T) {
	resolver := New(&mockStore{})

	// Act
	_, err := resolver.Resolve(context.Background(), &commonpb.DeviceSelector{
		SelectionType: &commonpb.DeviceSelector_DeviceList{
			DeviceList: &commonpb.DeviceIdentifierList{},
		},
	}, 1)

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}

func TestResolve_AllDevices_Success(t *testing.T) {
	store := &mockStore{filterIDs: []string{"dev-a", "dev-b"}}
	resolver := New(store)

	// Act
	ids, err := resolver.Resolve(context.Background(), &commonpb.DeviceSelector{
		SelectionType: &commonpb.DeviceSelector_AllDevices{AllDevices: true},
	}, 1)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, []string{"dev-a", "dev-b"}, ids)
}

func TestResolve_AllDevices_PropagatesStoreError(t *testing.T) {
	store := &mockStore{filterErr: errors.New("db failure")}
	resolver := New(store)

	// Act
	_, err := resolver.Resolve(context.Background(), &commonpb.DeviceSelector{
		SelectionType: &commonpb.DeviceSelector_AllDevices{AllDevices: true},
	}, 1)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db failure")
}

func TestResolveExplicitDevices_NilList(t *testing.T) {
	resolver := New(&mockStore{})

	// Act
	_, err := resolver.ResolveExplicitDevices(context.Background(), nil, 1)

	// Assert
	require.Error(t, err)
	assert.True(t, fleeterror.IsInvalidArgumentError(err))
}
