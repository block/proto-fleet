package minerproxy

import (
	"context"
	"database/sql"
	"encoding/base64"
	"net/http"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
	"github.com/block/proto-fleet/server/internal/domain/devicewrite"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	"github.com/block/proto-fleet/server/internal/domain/fleetnode/credentialblob"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

// MinerWebSender uses only the assigned node's authenticated live connection.
type MinerWebSender interface {
	SupportsMinerWeb(fleetNodeID int64) bool
	SendMinerWeb(ctx context.Context, fleetNodeID int64, command *pb.ControlCommand) (*http.Response, error)
}

func nodeCredential(value sql.NullString) ([]byte, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	data, err := base64.StdEncoding.DecodeString(value.String)
	if err != nil || !credentialblob.IsValid(data) {
		return nil, fleeterror.NewFailedPreconditionError("invalid node miner credentials")
	}
	return data, nil
}

func (h *Handler) forwardViaNode(ctx context.Context, source *http.Request, target proxyTarget, path string, body []byte) (*http.Response, error) {
	row := target.binding
	if h.nodeSender == nil || !h.nodeSender.SupportsMinerWeb(row.FleetNodeID.Int64) {
		return nil, devicewrite.NotDispatched(fleeterror.NewUnavailableErrorf("embedded view requires a connected, upgraded Fleet Node"))
	}
	password, err := nodeCredential(row.PasswordEnc)
	if err != nil {
		return nil, devicewrite.NotDispatched(err)
	}
	username, err := nodeCredential(row.UsernameEnc)
	if err != nil {
		return nil, devicewrite.NotDispatched(err)
	}
	request := &pb.MinerWebRequest{
		Target: &pb.MinerConnectionDescriptor{
			DeviceIdentifier: row.DeviceIdentifier, DriverName: row.DriverName, DeviceClass: commonpb.DeviceClass(row.DeviceClass),
			IpAddress: row.IpAddress, Port: row.Port, UrlScheme: sourceScheme(target.baseURL),
			SerialNumber: row.SerialNumber.String, MacAddress: row.MacAddress,
			CredentialUsername: username, CredentialPassword: password,
		},
		Method: source.Method, Path: path, Query: source.URL.RawQuery, Body: body, Timeout: durationpb.New(proxyClientTimeout),
	}
	for name, values := range source.Header {
		if nodeRequestHeaderAllowed(name) {
			for _, value := range values {
				request.Headers = append(request.Headers, &pb.MinerWebHeader{Name: name, Value: value})
			}
		}
	}
	if err := ValidateNodeWebRequest(request); err != nil {
		return nil, devicewrite.NotDispatched(err)
	}
	payload, err := proto.Marshal(&pb.AgentCommand{Command: &pb.AgentCommand_MinerWeb{MinerWeb: request}})
	if err != nil {
		return nil, devicewrite.NotDispatched(err)
	}
	var response *http.Response
	err = h.dispatchTarget(ctx, target, proxyMutatesMiner(source.Method, path), func(ctx context.Context) error {
		var sendErr error
		//nolint:bodyclose // Ownership returns to forwardAndCopy, which closes after browser delivery.
		response, sendErr = h.nodeSender.SendMinerWeb(ctx, row.FleetNodeID.Int64, &pb.ControlCommand{CommandId: uuid.NewString(), Payload: payload})
		if sendErr == nil {
			return nil
		}
		safeErr := fleeterror.NewUnavailableErrorf("Fleet Node miner request did not complete")
		if control.IsCommandNotEnqueued(sendErr) || control.IsCommandRejectedBeforeDispatch(sendErr) {
			return devicewrite.NotDispatched(safeErr)
		}
		return devicewrite.UncertainDelivery(safeErr)
	})
	return response, err
}

func sourceScheme(base string) string {
	if len(base) >= 8 && base[:8] == "https://" {
		return "https"
	}
	return "http"
}
