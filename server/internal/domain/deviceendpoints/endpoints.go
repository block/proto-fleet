package deviceendpoints

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	commonpb "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

func FromSDK(endpoints []sdk.DeviceEndpoint) []*commonpb.DeviceEndpoint {
	var result []*commonpb.DeviceEndpoint
	for _, c := range endpoints {
		result = append(result, &commonpb.DeviceEndpoint{Id: c.ID, DeviceKey: c.DeviceKey, MemberIds: c.MemberIDs, Host: c.Host, Port: c.Port, UrlScheme: c.URLScheme, RpcPort: c.RPCPort, RpcTls: c.RPCTLS})
	}
	return result
}

func ToSDK(endpoints []*commonpb.DeviceEndpoint) []sdk.DeviceEndpoint {
	var result []sdk.DeviceEndpoint
	for _, c := range endpoints {
		result = append(result, sdk.DeviceEndpoint{ID: c.GetId(), DeviceKey: c.GetDeviceKey(), MemberIDs: c.GetMemberIds(), Host: c.GetHost(), Port: c.GetPort(), URLScheme: c.GetUrlScheme(), RPCPort: c.GetRpcPort(), RPCTLS: c.GetRpcTls()})
	}
	return result
}

func Key(endpoints []*commonpb.DeviceEndpoint) (string, error) {
	return sdk.DeviceEndpointKey(ToSDK(endpoints))
}

// Store by endpoint ID so rediscovery replaces one address and retains other addresses.
func Encode(endpoints []*commonpb.DeviceEndpoint) ([]byte, error) {
	sdkEndpoints := ToSDK(endpoints)
	if _, err := sdk.DeviceEndpointKey(sdkEndpoints); err != nil {
		return nil, err
	}
	result := map[string]sdk.DeviceEndpoint{}
	for _, c := range sdkEndpoints {
		result[c.ID] = c
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode device endpoints: %w", err)
	}
	return encoded, nil
}

func Decode(data []byte) ([]*commonpb.DeviceEndpoint, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var records map[string]sdk.DeviceEndpoint
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("decode device endpoints: %w", err)
	}
	var endpoints []sdk.DeviceEndpoint
	for id, c := range records {
		if id != c.ID {
			return nil, fmt.Errorf("invalid device endpoint identity")
		}
		endpoints = append(endpoints, c)
	}
	slices.SortFunc(endpoints, func(a, b sdk.DeviceEndpoint) int {
		return strings.Compare(a.ID, b.ID)
	})
	if _, err := sdk.DeviceEndpointKey(endpoints); err != nil {
		return nil, err
	}
	return FromSDK(endpoints), nil
}

func NodeIdentifier(nodeID int64, driver string, endpoints []*commonpb.DeviceEndpoint) (string, error) {
	key, err := Key(endpoints)
	if err != nil || key == "" {
		return "", err
	}
	sum := sha256.Sum256([]byte(driver + "\x00" + key))
	return "endpoint:" + strconv.FormatInt(nodeID, 10) + ":" + hex.EncodeToString(sum[:]), nil
}
