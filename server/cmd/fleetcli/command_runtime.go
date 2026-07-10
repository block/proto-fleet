package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	commonv1 "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	devicesetv1 "github.com/block/proto-fleet/server/generated/grpc/device_set/v1"
	minercommandv1 "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
	"google.golang.org/protobuf/proto"
)

type generatedAuthPolicy string

const (
	generatedAuthUnauthenticated generatedAuthPolicy = "unauthenticated"
	generatedAuthAuthenticated   generatedAuthPolicy = "authenticated"
	generatedAuthSessionOnly     generatedAuthPolicy = "session_only"
)

func generatedRequestCommand(
	name string,
	usage string,
	method string,
	auth generatedAuthPolicy,
	flags []cli.Flag,
	buildRequest func(ctx context.Context, cmd *cli.Command, client *Client) (proto.Message, error),
	newResponse func() proto.Message,
) *cli.Command {
	return &cli.Command{
		Name:  name,
		Usage: usage,
		Flags: flags,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			client, err := openClient(ctx, cmd)
			if err != nil {
				return err
			}
			defer func() { _ = client.Close() }()

			req, err := buildRequest(ctx, cmd, client)
			if err != nil {
				return err
			}
			resp := newResponse()
			return generatedCallAndPrintWithClient(ctx, client, auth, method, req, resp)
		},
	}
}

func generatedCallAndPrintWithClient(
	ctx context.Context,
	client *Client,
	auth generatedAuthPolicy,
	method string,
	req proto.Message,
	resp proto.Message,
) error {
	var err error
	switch auth {
	case generatedAuthUnauthenticated:
		err = client.CallUnauthenticated(ctx, method, req, resp)
	case generatedAuthAuthenticated:
		err = client.CallAuthenticated(ctx, method, req, resp)
	case generatedAuthSessionOnly:
		err = client.CallSessionOnly(ctx, method, req, resp)
	}
	if err != nil {
		return err
	}
	return printProto(resp)
}

func generatedMinerSelectorFlags() []cli.Flag {
	return append([]cli.Flag{
		&cli.BoolFlag{Name: "all-devices", Usage: "Select all devices"},
	}, generatedBoundedMinerSelectorFlags()...)
}

// generatedBoundedMinerSelectorFlags returns the miner selector flags that name
// a bounded set of devices (explicit ids, groups, or racks), without the
// fleet-wide --all-devices flag. Commands that must never target every device,
// such as firmware deploy, register these flags directly.
func generatedBoundedMinerSelectorFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringSliceFlag{Name: "device", Usage: "Select one or more device identifiers"},
		&cli.StringSliceFlag{Name: "group-id", Usage: "Select devices from one or more group ids"},
		&cli.StringSliceFlag{Name: "group", Usage: "Select devices from one or more group labels"},
		&cli.StringSliceFlag{Name: "rack-id", Usage: "Select devices from one or more rack ids"},
		&cli.StringSliceFlag{Name: "rack", Usage: "Select devices from one or more rack labels"},
	}
}

func generatedCommonSelectorFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{Name: "all-devices", Usage: "Select all devices"},
		&cli.StringSliceFlag{Name: "device", Usage: "Select one or more device identifiers"},
	}
}

func generatedCommonDeviceListSelectorFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringSliceFlag{Name: "device", Usage: "Select one or more device identifiers"},
	}
}

// generatedMinerSelectorProvided reports whether any miner selector flag was
// set. It mirrors generatedMinerSelectorFlags so the set of selector flags
// lives in one place, even when a command also accepts a --json request body.
func generatedMinerSelectorProvided(cmd *cli.Command) bool {
	return cmd.IsSet("all-devices") || generatedBoundedMinerSelectorProvided(cmd)
}

// generatedBoundedMinerSelectorProvided reports whether any bounded miner
// selector flag (device, group, or rack) was set.
func generatedBoundedMinerSelectorProvided(cmd *cli.Command) bool {
	return cmd.IsSet("device") || cmd.IsSet("group-id") ||
		cmd.IsSet("group") || cmd.IsSet("rack-id") || cmd.IsSet("rack")
}

// generatedCommonSelectorProvided reports whether any common selector flag was
// set. It mirrors generatedCommonSelectorFlags.
func generatedCommonSelectorProvided(cmd *cli.Command) bool {
	return cmd.IsSet("all-devices") || cmd.IsSet("device")
}

func generatedCommonDeviceListSelectorProvided(cmd *cli.Command) bool {
	return cmd.IsSet("device")
}

func generatedBuildCommonSelector(cmd *cli.Command) (*commonv1.DeviceSelector, error) {
	allDevices := cmd.Bool("all-devices")
	deviceIDs := dedupeStrings(cmd.StringSlice("device"))

	if allDevices && len(deviceIDs) > 0 {
		return nil, fmt.Errorf("use either --all-devices or --device, not both")
	}
	if allDevices {
		return &commonv1.DeviceSelector{
			SelectionType: &commonv1.DeviceSelector_AllDevices{AllDevices: true},
		}, nil
	}
	if len(deviceIDs) == 0 {
		return nil, fmt.Errorf("one of --all-devices or --device is required")
	}
	return &commonv1.DeviceSelector{
		SelectionType: &commonv1.DeviceSelector_DeviceList{
			DeviceList: &commonv1.DeviceIdentifierList{DeviceIdentifiers: deviceIDs},
		},
	}, nil
}

func generatedBuildCommonDeviceListSelector(cmd *cli.Command) (*commonv1.DeviceSelector, error) {
	deviceIDs := dedupeStrings(cmd.StringSlice("device"))
	if len(deviceIDs) == 0 {
		return nil, fmt.Errorf("at least one --device is required")
	}
	return &commonv1.DeviceSelector{
		SelectionType: &commonv1.DeviceSelector_DeviceList{
			DeviceList: &commonv1.DeviceIdentifierList{DeviceIdentifiers: deviceIDs},
		},
	}, nil
}

func generatedBuildMinerSelector(ctx context.Context, cmd *cli.Command, client *Client) (*minercommandv1.DeviceSelector, error) {
	if cmd.Bool("all-devices") {
		if generatedBoundedMinerSelectorProvided(cmd) {
			return nil, fmt.Errorf("use either --all-devices or explicit device/group/rack selectors, not both")
		}
		return &minercommandv1.DeviceSelector{
			SelectionType: &minercommandv1.DeviceSelector_AllDevices{
				AllDevices: &minercommandv1.DeviceFilter{},
			},
		}, nil
	}
	return generatedBuildBoundedMinerSelector(ctx, cmd, client)
}

// resolveBoundedSelectorDeviceIDs resolves one device-set selector (labels plus
// explicit ids) into the member device identifiers it names: labels become ids,
// the combined ids are verified to be the wanted set type, and each is expanded
// to its members. noun ("group"/"rack") is used only for error messages.
func resolveBoundedSelectorDeviceIDs(
	ctx context.Context,
	client *Client,
	setType devicesetv1.DeviceSetType,
	labels []string,
	ids []int64,
) ([]string, error) {
	noun := generatedDeviceSetTypeName(setType)
	if len(labels) > 0 {
		labelIDs, err := generatedResolveDeviceSetIDsByLabel(ctx, client, setType, labels)
		if err != nil {
			return nil, fmt.Errorf("resolve %s labels: %w", noun, err)
		}
		ids = append(ids, labelIDs...)
	}
	ids = dedupeInt64s(ids)
	if len(ids) == 0 {
		return nil, nil
	}
	if err := generatedRequireDeviceSetTypes(ctx, client, ids, setType); err != nil {
		return nil, fmt.Errorf("verify %s ids: %w", noun, err)
	}
	memberIDs, err := generatedDeviceSetMemberDeviceIDs(ctx, client, ids)
	if err != nil {
		return nil, fmt.Errorf("resolve %s members: %w", noun, err)
	}
	return memberIDs, nil
}

// generatedBuildBoundedMinerSelector resolves the explicit device/group/rack
// selector flags into a DeviceSelector that names a concrete set of devices.
// Group and rack selectors (by id or label) are expanded client-side to their
// member device identifiers, so the result is always an explicit include list
// and never targets all devices. It is shared by generated miner commands and
// by firmware deploy.
func generatedBuildBoundedMinerSelector(ctx context.Context, cmd *cli.Command, client *Client) (*minercommandv1.DeviceSelector, error) {
	deviceIDs := trimNonEmpty(cmd.StringSlice("device"))
	groupIDs, err := parseInt64Slice(cmd.StringSlice("group-id"))
	if err != nil {
		return nil, fmt.Errorf("invalid group-id value: %w", err)
	}
	groupLabels := dedupeStrings(cmd.StringSlice("group"))
	rackIDs, err := parseInt64Slice(cmd.StringSlice("rack-id"))
	if err != nil {
		return nil, fmt.Errorf("invalid rack-id value: %w", err)
	}
	rackLabels := dedupeStrings(cmd.StringSlice("rack"))

	groupMembers, err := resolveBoundedSelectorDeviceIDs(ctx, client, devicesetv1.DeviceSetType_DEVICE_SET_TYPE_GROUP, groupLabels, groupIDs)
	if err != nil {
		return nil, err
	}
	deviceIDs = append(deviceIDs, groupMembers...)

	rackMembers, err := resolveBoundedSelectorDeviceIDs(ctx, client, devicesetv1.DeviceSetType_DEVICE_SET_TYPE_RACK, rackLabels, rackIDs)
	if err != nil {
		return nil, err
	}
	deviceIDs = append(deviceIDs, rackMembers...)

	deviceIDs = dedupeStrings(deviceIDs)
	if len(deviceIDs) == 0 {
		return nil, fmt.Errorf("one of --device, --group-id, --group, --rack-id, or --rack is required")
	}

	return &minercommandv1.DeviceSelector{
		SelectionType: &minercommandv1.DeviceSelector_IncludeDevices{
			IncludeDevices: &commonv1.DeviceIdentifierList{DeviceIdentifiers: deviceIDs},
		},
	}, nil
}

func generatedDeviceSetMemberDeviceIDs(ctx context.Context, client *Client, deviceSetIDs []int64) ([]string, error) {
	var deviceIDs []string
	for _, deviceSetID := range deviceSetIDs {
		pageToken := ""
		for {
			req := &devicesetv1.ListDeviceSetMembersRequest{
				DeviceSetId: deviceSetID,
				PageSize:    500,
				PageToken:   pageToken,
			}
			resp := &devicesetv1.ListDeviceSetMembersResponse{}
			if err := client.CallAuthenticated(ctx, "/device_set.v1.DeviceSetService/ListDeviceSetMembers", req, resp); err != nil {
				return nil, fmt.Errorf("device set %d: %w", deviceSetID, err)
			}
			for _, member := range resp.GetMembers() {
				if member.GetDeviceIdentifier() != "" {
					deviceIDs = append(deviceIDs, member.GetDeviceIdentifier())
				}
			}
			pageToken = resp.GetNextPageToken()
			if pageToken == "" {
				break
			}
		}
	}
	return dedupeStrings(deviceIDs), nil
}

func generatedResolveDeviceSetIDsByLabel(
	ctx context.Context,
	client *Client,
	deviceSetType devicesetv1.DeviceSetType,
	labels []string,
) ([]int64, error) {
	normalizedLabels := make([]string, 0, len(labels))
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		normalizedLabels = append(normalizedLabels, label)
	}
	if len(normalizedLabels) == 0 {
		return nil, nil
	}

	matches := make(map[string][]int64, len(normalizedLabels))
	pageToken := ""
	for {
		req := &devicesetv1.ListDeviceSetsRequest{
			Type:      deviceSetType,
			PageSize:  500,
			PageToken: pageToken,
		}
		resp := &devicesetv1.ListDeviceSetsResponse{}
		if err := client.CallAuthenticated(ctx, "/device_set.v1.DeviceSetService/ListDeviceSets", req, resp); err != nil {
			return nil, err
		}
		for _, deviceSet := range resp.GetDeviceSets() {
			for _, label := range normalizedLabels {
				if deviceSet.GetLabel() == label {
					matches[label] = append(matches[label], deviceSet.GetId())
				}
			}
		}
		pageToken = resp.GetNextPageToken()
		if pageToken == "" {
			break
		}
	}

	result := make([]int64, 0, len(normalizedLabels))
	for _, label := range normalizedLabels {
		ids := matches[label]
		switch len(ids) {
		case 0:
			return nil, fmt.Errorf("no %s found with label %q", generatedDeviceSetTypeName(deviceSetType), label)
		case 1:
			result = append(result, ids[0])
		default:
			return nil, fmt.Errorf("multiple %s entries found with label %q; use the --%s-id flag instead", generatedDeviceSetTypeName(deviceSetType), label, generatedDeviceSetTypeName(deviceSetType))
		}
	}
	return dedupeInt64s(result), nil
}

func generatedRequireDeviceSetType(
	ctx context.Context,
	client *Client,
	deviceSetID int64,
	want devicesetv1.DeviceSetType,
) error {
	if deviceSetID == 0 {
		return fmt.Errorf("device-set-id is required")
	}
	req := &devicesetv1.GetDeviceSetRequest{DeviceSetId: deviceSetID}
	resp := &devicesetv1.GetDeviceSetResponse{}
	if err := client.CallAuthenticated(ctx, "/device_set.v1.DeviceSetService/GetDeviceSet", req, resp); err != nil {
		return fmt.Errorf("verify %s %d: %w", generatedDeviceSetTypeName(want), deviceSetID, err)
	}
	deviceSet := resp.GetDeviceSet()
	if deviceSet == nil {
		return fmt.Errorf("verify %s %d: response did not include device set", generatedDeviceSetTypeName(want), deviceSetID)
	}
	got := deviceSet.GetType()
	if got != want {
		return fmt.Errorf("device set %d is a %s, not a %s", deviceSetID, generatedDeviceSetTypeName(got), generatedDeviceSetTypeName(want))
	}
	return nil
}

func generatedRequireDeviceSetTypes(
	ctx context.Context,
	client *Client,
	deviceSetIDs []int64,
	want devicesetv1.DeviceSetType,
) error {
	for _, deviceSetID := range dedupeInt64s(deviceSetIDs) {
		if err := generatedRequireDeviceSetType(ctx, client, deviceSetID, want); err != nil {
			return err
		}
	}
	return nil
}

func generatedDeviceSetTypeName(deviceSetType devicesetv1.DeviceSetType) string {
	switch deviceSetType {
	case devicesetv1.DeviceSetType_DEVICE_SET_TYPE_UNSPECIFIED:
		return "device set"
	case devicesetv1.DeviceSetType_DEVICE_SET_TYPE_GROUP:
		return "group"
	case devicesetv1.DeviceSetType_DEVICE_SET_TYPE_RACK:
		return "rack"
	default:
		return "device set"
	}
}

func generatedReadSecret(cmd *cli.Command, stdinFlag string, label string) (string, error) {
	if cmd.Bool(stdinFlag) {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read %s from stdin: %w", label, err)
		}
		secret := strings.TrimRight(string(data), "\r\n")
		if secret == "" {
			return "", fmt.Errorf("%s from stdin is empty", label)
		}
		return secret, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("%s is required; use --%s or run from a terminal to prompt", label, stdinFlag)
	}
	fmt.Fprintf(os.Stderr, "%s: ", strings.Title(label))
	secret, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", label, err)
	}
	if len(secret) == 0 {
		return "", fmt.Errorf("%s is required", label)
	}
	return string(secret), nil
}

// trimNonEmpty trims surrounding whitespace from each value and drops the
// blanks, preserving order.
func trimNonEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func dedupeInt64s(values []int64) []int64 {
	seen := make(map[int64]bool, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
