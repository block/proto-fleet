package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	collectionv1 "github.com/block/proto-fleet/server/generated/grpc/collection/v1"
	commonv1 "github.com/block/proto-fleet/server/generated/grpc/common/v1"
	minercommandv1 "github.com/block/proto-fleet/server/generated/grpc/minercommand/v1"
	"github.com/urfave/cli/v3"
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
	case generatedAuthSessionOnly:
		err = client.CallSessionOnly(ctx, method, req, resp)
	default:
		err = client.CallAuthenticated(ctx, method, req, resp)
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

	if len(groupLabels) > 0 {
		labelIDs, err := generatedResolveCollectionIDsByLabel(ctx, client, collectionv1.CollectionType_COLLECTION_TYPE_GROUP, groupLabels)
		if err != nil {
			return nil, fmt.Errorf("resolve group labels: %w", err)
		}
		groupIDs = append(groupIDs, labelIDs...)
	}
	groupIDs = dedupeInt64s(groupIDs)
	if len(groupIDs) > 0 {
		if err := generatedRequireCollectionTypes(ctx, client, groupIDs, collectionv1.CollectionType_COLLECTION_TYPE_GROUP); err != nil {
			return nil, fmt.Errorf("verify group ids: %w", err)
		}
		memberIDs, err := generatedCollectionMemberDeviceIDs(ctx, client, groupIDs)
		if err != nil {
			return nil, fmt.Errorf("resolve group members: %w", err)
		}
		deviceIDs = append(deviceIDs, memberIDs...)
	}
	if len(rackLabels) > 0 {
		labelIDs, err := generatedResolveCollectionIDsByLabel(ctx, client, collectionv1.CollectionType_COLLECTION_TYPE_RACK, rackLabels)
		if err != nil {
			return nil, fmt.Errorf("resolve rack labels: %w", err)
		}
		rackIDs = append(rackIDs, labelIDs...)
	}
	rackIDs = dedupeInt64s(rackIDs)
	if len(rackIDs) > 0 {
		if err := generatedRequireCollectionTypes(ctx, client, rackIDs, collectionv1.CollectionType_COLLECTION_TYPE_RACK); err != nil {
			return nil, fmt.Errorf("verify rack ids: %w", err)
		}
		memberIDs, err := generatedCollectionMemberDeviceIDs(ctx, client, rackIDs)
		if err != nil {
			return nil, fmt.Errorf("resolve rack members: %w", err)
		}
		deviceIDs = append(deviceIDs, memberIDs...)
	}

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

func generatedCollectionMemberDeviceIDs(ctx context.Context, client *Client, collectionIDs []int64) ([]string, error) {
	var deviceIDs []string
	for _, collectionID := range collectionIDs {
		pageToken := ""
		for {
			req := &collectionv1.ListCollectionMembersRequest{
				CollectionId: collectionID,
				PageSize:     500,
				PageToken:    pageToken,
			}
			resp := &collectionv1.ListCollectionMembersResponse{}
			if err := client.CallAuthenticated(ctx, "/collection.v1.DeviceCollectionService/ListCollectionMembers", req, resp); err != nil {
				return nil, fmt.Errorf("collection %d: %w", collectionID, err)
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

func generatedResolveCollectionIDsByLabel(
	ctx context.Context,
	client *Client,
	collectionType collectionv1.CollectionType,
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
		req := &collectionv1.ListCollectionsRequest{
			Type:      collectionType,
			PageSize:  500,
			PageToken: pageToken,
		}
		resp := &collectionv1.ListCollectionsResponse{}
		if err := client.CallAuthenticated(ctx, "/collection.v1.DeviceCollectionService/ListCollections", req, resp); err != nil {
			return nil, err
		}
		for _, collection := range resp.GetCollections() {
			for _, label := range normalizedLabels {
				if collection.GetLabel() == label {
					matches[label] = append(matches[label], collection.GetId())
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
			return nil, fmt.Errorf("no %s found with label %q", generatedCollectionTypeName(collectionType), label)
		case 1:
			result = append(result, ids[0])
		default:
			return nil, fmt.Errorf("multiple %s entries found with label %q; use the --%s-id flag instead", generatedCollectionTypeName(collectionType), label, generatedCollectionTypeName(collectionType))
		}
	}
	return dedupeInt64s(result), nil
}

func generatedRequireCollectionType(
	ctx context.Context,
	client *Client,
	collectionID int64,
	want collectionv1.CollectionType,
) error {
	if collectionID == 0 {
		return fmt.Errorf("collection-id is required")
	}
	req := &collectionv1.GetCollectionRequest{CollectionId: collectionID}
	resp := &collectionv1.GetCollectionResponse{}
	if err := client.CallAuthenticated(ctx, "/collection.v1.DeviceCollectionService/GetCollection", req, resp); err != nil {
		return fmt.Errorf("verify %s %d: %w", generatedCollectionTypeName(want), collectionID, err)
	}
	collection := resp.GetCollection()
	if collection == nil {
		return fmt.Errorf("verify %s %d: response did not include collection", generatedCollectionTypeName(want), collectionID)
	}
	got := collection.GetType()
	if got != want {
		return fmt.Errorf("collection %d is a %s, not a %s", collectionID, generatedCollectionTypeName(got), generatedCollectionTypeName(want))
	}
	return nil
}

func generatedRequireCollectionTypes(
	ctx context.Context,
	client *Client,
	collectionIDs []int64,
	want collectionv1.CollectionType,
) error {
	for _, collectionID := range dedupeInt64s(collectionIDs) {
		if err := generatedRequireCollectionType(ctx, client, collectionID, want); err != nil {
			return err
		}
	}
	return nil
}

func generatedCollectionTypeName(collectionType collectionv1.CollectionType) string {
	switch collectionType {
	case collectionv1.CollectionType_COLLECTION_TYPE_GROUP:
		return "group"
	case collectionv1.CollectionType_COLLECTION_TYPE_RACK:
		return "rack"
	default:
		return "collection"
	}
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
