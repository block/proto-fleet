package miner_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/auth"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/miner"
	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	"github.com/block/proto-fleet/server/internal/domain/miner/models"
	"github.com/block/proto-fleet/server/internal/domain/session"
	"github.com/block/proto-fleet/server/internal/domain/stores/sqlstores"
	"github.com/block/proto-fleet/server/internal/handlers/fleetnode/gateway"
	"github.com/block/proto-fleet/server/internal/handlers/interceptors"
	"github.com/block/proto-fleet/server/internal/infrastructure/files"
	"github.com/block/proto-fleet/server/internal/testutil"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestDispatchSaturationPreservesSessionAuthAndFirmwareDownload(t *testing.T) {
	t.Chdir(t.TempDir())
	conn, crypto, fileStore := setupTestDB(t)
	// Resolve every handle before starting dispatch: the scenario isolates the
	// connections held across slow firmware commands from setup queries.
	handles := make([]interfaces.Miner, 3)
	identifiers := make([]string, 3)
	deviceIDs := make([]int64, 3)
	for i := range handles {
		identifiers[i] = fmt.Sprintf("pool-firmware-%d", i)
		deviceIDs[i] = createTestDevice(t, conn, identifiers[i])
	}
	node := createTestFleetNode(t, conn, "pool-firmware-node", bytes.Repeat([]byte{1}, 32))
	for _, id := range deviceIDs {
		_, err := conn.ExecContext(t.Context(), `INSERT INTO fleet_node_device(fleet_node_id,device_id,org_id) VALUES($1,$2,1)`, node, id)
		require.NoError(t, err)
	}
	registry := control.NewRegistry()
	stream := registry.Register(node)
	t.Cleanup(func() { registry.Disconnect(node) })
	service := miner.NewMinerService(conn, sqlstores.NewSQLUserStore(conn), crypto, fileStore, &fakePluginManager{driverName: "antminer"}).WithCommandSender(registry)
	for i := range handles {
		var err error
		handles[i], err = service.GetMinerFromDeviceIdentifier(t.Context(), models.DeviceIdentifier(identifiers[i]))
		require.NoError(t, err)
	}
	_, err := conn.ExecContext(t.Context(), `INSERT INTO "user"(id,user_id,username,password_hash) VALUES(1,'pool-user','pool-user','fixture-password-hash')`)
	require.NoError(t, err)
	userSessions := session.NewService(session.Config{IDBytes: 32, Duration: time.Hour}, sqlstores.NewSQLSessionStore(conn))
	userSession, err := userSessions.Create(t.Context(), 1, 1, "pool-test", "192.0.2.1")
	require.NoError(t, err)
	const bearer = "pool-firmware-node-session"
	authStore := sqlstores.NewSQLFleetNodeAuthStore(conn)
	require.NoError(t, authStore.UpsertSession(t.Context(), auth.SessionFingerprint(bearer), node, time.Now().Add(time.Hour)))
	authService := auth.NewService(authStore, sqlstores.NewSQLFleetNodeEnrollmentStore(conn), nil)
	payload := []byte("firmware payload downloaded while physical dispatches hold connections")
	fileID, err := fileStore.SaveFirmwareFile("update.swu", bytes.NewReader(payload), files.FirmwareMetadata{TargetManufacturer: "TestCorp", TargetModel: "TestMiner", FirmwareVersion: "1.2.3"})
	require.NoError(t, err)
	reader, info, err := fileStore.OpenFirmwareFileWithInfo(fileID)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	firmware := sdk.FirmwareFile{ID: info.ID, Filename: info.Filename, Size: info.Size, SHA256: info.SHA256}
	mux := http.NewServeMux()
	mux.Handle(fleetnodegatewayv1connect.NewFleetNodeGatewayServiceHandler(gateway.NewHandler(nil, authService, nil, registry, fileStore), connect.WithInterceptors(interceptors.NewFleetNodeAuthInterceptor(authService, []string{fleetnodegatewayv1connect.FleetNodeGatewayServiceDownloadCommandArtifactProcedure}))))
	server := testutil.NewH2CServer(t, mux)
	client := fleetnodegatewayv1connect.NewFleetNodeGatewayServiceClient(testutil.NewH2CClient(), server.URL, connect.WithGRPC())

	conn.SetMaxOpenConns(4)
	conn.SetMaxIdleConns(4)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	var dispatches sync.WaitGroup
	defer func() { cancel(); registry.Disconnect(node); dispatches.Wait() }()
	done := make(chan error, 3)
	for i := range 2 {
		dispatches.Add(1)
		go func(i int) { defer dispatches.Done(); done <- handles[i].FirmwareUpdate(ctx, firmware) }(i)
	}
	commands := make([]*pb.ControlCommand, 0, 3)
	for len(commands) < 2 {
		select {
		case command := <-stream.Outgoing:
			commands = append(commands, command)
		case <-ctx.Done():
			t.Fatal("firmware dispatches did not start")
		}
	}
	require.Equal(t, 2, conn.Stats().InUse, "two physical firmware calls hold the dispatch budget")
	dispatches.Add(1)
	go func() { defer dispatches.Done(); done <- handles[2].FirmwareUpdate(ctx, firmware) }()
	select {
	case command := <-stream.Outgoing:
		t.Fatalf("third dispatch bypassed the reserved database headroom: %s", command.GetCommandId())
	case <-time.After(200 * time.Millisecond):
	}
	// Occupy one spare connection to prove auth and download can use the other
	// while the extra physical dispatch waits outside the database pool.
	spare, err := conn.Conn(ctx)
	require.NoError(t, err)
	defer spare.Close()
	authCtx, authCancel := context.WithTimeout(ctx, 2*time.Second)
	defer authCancel()
	validated, err := userSessions.Validate(authCtx, userSession.SessionID)
	require.NoError(t, err, "user session validation must retain connection headroom")
	require.Equal(t, userSession.SessionID, validated.SessionID)
	var commandPayload pb.AgentCommand
	require.NoError(t, proto.Unmarshal(commands[0].GetPayload(), &commandPayload))
	require.NotNil(t, commandPayload.GetMinerCommand())
	// Outgoing is published after the command's artifact expectations are
	// registered. Read the target and reference from that authoritative command.
	ref := commandPayload.GetMinerCommand().GetFirmwareUpdate().GetArtifact()
	require.NotNil(t, ref)
	require.Equal(t, info.ID, ref.GetArtifactId())
	request := connect.NewRequest(&pb.DownloadCommandArtifactRequest{CommandId: commands[0].GetCommandId(), Artifact: ref, DeviceIdentifier: commandPayload.GetMinerCommand().GetTarget().GetDeviceIdentifier()})
	request.Header().Set("Authorization", "Bearer "+bearer)
	download, err := client.DownloadCommandArtifact(authCtx, request)
	require.NoError(t, err)
	var received bytes.Buffer
	for download.Receive() {
		if chunk := download.Msg().GetChunk(); chunk != nil {
			_, err = received.Write(chunk.GetData())
			require.NoError(t, err)
		}
	}
	require.NoError(t, download.Err(), "actual Fleet Node session authentication and firmware transfer must complete under dispatch saturation")
	require.NoError(t, download.Close())
	require.Equal(t, payload, received.Bytes())
	require.NoError(t, spare.Close())
	select {
	case command := <-stream.Outgoing:
		t.Fatalf("waiting dispatch ran before either active firmware call completed: %s", command.GetCommandId())
	default:
	}
	for _, command := range commands {
		stream.PublishAck(&pb.ControlAck{CommandId: command.GetCommandId(), Succeeded: true, Code: pb.AckCode_ACK_CODE_OK})
	}
	select {
	case command := <-stream.Outgoing:
		stream.PublishAck(&pb.ControlAck{CommandId: command.GetCommandId(), Succeeded: true, Code: pb.AckCode_ACK_CODE_OK})
	case <-ctx.Done():
		t.Fatal("waiting dispatch did not resume after a firmware call completed")
	}
	for range 3 {
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-ctx.Done():
			t.Fatal("firmware dispatch did not finish")
		}
	}
}
