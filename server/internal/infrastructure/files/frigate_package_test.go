package files

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFrigatePackageStore_PublishOpenDelete(t *testing.T) {
	root := t.TempDir()
	store, err := NewFrigatePackageStore(root)
	require.NoError(t, err)

	id := uuid.New()
	assert.False(t, store.Exists(id, 5))
	_, _, err = store.Open(id)
	assert.ErrorIs(t, err, ErrFrigatePackageMissing)

	staged, err := store.NewStagingPath(uuid.NewString())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(staged, []byte("hello"), 0o600))
	require.NoError(t, store.Publish(id, staged))
	_, err = os.Stat(staged)
	assert.True(t, os.IsNotExist(err), "staging file is moved")

	assert.True(t, store.Exists(id, 5))
	assert.False(t, store.Exists(id, 6), "size mismatch")
	f, size, err := store.Open(id)
	require.NoError(t, err)
	defer f.Close()
	assert.Equal(t, int64(5), size)
	data, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))
	assert.Equal(t, filepath.Join(root, id.String(), "package.deb"), store.Path(id))

	require.NoError(t, store.Delete(id))
	assert.False(t, store.Exists(id, 5))
	require.NoError(t, store.Delete(id), "deleting a missing package is not an error")
}

func TestFrigatePackageStore_RejectsBadInput(t *testing.T) {
	_, err := NewFrigatePackageStore("")
	require.Error(t, err)

	store, err := NewFrigatePackageStore(t.TempDir())
	require.NoError(t, err)
	for _, name := range []string{"", ".", "..", "a/b", "../x"} {
		_, err := store.NewStagingPath(name)
		assert.Error(t, err, name)
	}
	name := uuid.NewString()
	_, err = store.NewStagingPath(name)
	require.NoError(t, err)
	_, err = store.NewStagingPath(name)
	assert.Error(t, err, "a staging name is reserved exclusively")
}

func TestFrigatePackageStore_ClearsAbandonedStaging(t *testing.T) {
	root := t.TempDir()
	store, err := NewFrigatePackageStore(root)
	require.NoError(t, err)
	staged, err := store.NewStagingPath(uuid.NewString())
	require.NoError(t, err)
	require.FileExists(t, staged)

	_, err = NewFrigatePackageStore(root)
	require.NoError(t, err)
	_, err = os.Stat(staged)
	assert.True(t, os.IsNotExist(err))
}
