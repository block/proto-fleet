// Package authz wires the AuthzService Connect handler. Five RPCs are
// fully implemented (ListPermissions, ListRoles, CreateCustomRole,
// UpdateCustomRole, DeleteCustomRole) and back the Settings → Roles
// management UI. The assignment trio (AssignRole, UnassignRole,
// ListUserAssignments) returns Unimplemented; those land alongside the
// Team-page assignment flow in a follow-up.
//
// Role identifiers cross the wire as base-10 strings because the
// underlying role table uses int64 ids — roles have no external_id
// column the way users do. The handler parses the string back into an
// int64 on every mutation; an unparseable id surfaces as
// InvalidArgument rather than NotFound to avoid leaking which ids
// exist.
package authz

import (
	"context"
	"regexp"
	"strconv"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/block/proto-fleet/server/generated/grpc/authz/v1"
	"github.com/block/proto-fleet/server/generated/grpc/authz/v1/authzv1connect"
	authzDomain "github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

type Handler struct {
	svc *authzDomain.Service
}

var _ authzv1connect.AuthzServiceHandler = &Handler{}

func NewHandler(svc *authzDomain.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) ListPermissions(ctx context.Context, _ *connect.Request[pb.ListPermissionsRequest]) (*connect.Response[pb.ListPermissionsResponse], error) {
	if _, err := middleware.RequirePermission(ctx, authzDomain.PermRoleManage, authzDomain.ResourceContext{}); err != nil {
		return nil, err
	}
	entries := authzDomain.Catalog()
	out := make([]*pb.Permission, len(entries))
	for i, e := range entries {
		out[i] = &pb.Permission{
			Key:         e.Key,
			Description: e.Description,
			Resource:    e.Resource,
		}
	}
	return connect.NewResponse(&pb.ListPermissionsResponse{Permissions: out}), nil
}

func (h *Handler) ListRoles(ctx context.Context, _ *connect.Request[pb.ListRolesRequest]) (*connect.Response[pb.ListRolesResponse], error) {
	info, err := middleware.RequirePermission(ctx, authzDomain.PermRoleManage, authzDomain.ResourceContext{})
	if err != nil {
		return nil, err
	}
	views, err := h.svc.ListRoles(ctx, info.OrganizationID)
	if err != nil {
		return nil, err
	}
	out := make([]*pb.Role, len(views))
	for i, v := range views {
		out[i] = roleViewToProto(v)
	}
	return connect.NewResponse(&pb.ListRolesResponse{Roles: out}), nil
}

func (h *Handler) CreateCustomRole(ctx context.Context, req *connect.Request[pb.CreateCustomRoleRequest]) (*connect.Response[pb.CreateCustomRoleResponse], error) {
	info, err := middleware.RequirePermission(ctx, authzDomain.PermRoleManage, authzDomain.ResourceContext{})
	if err != nil {
		return nil, err
	}
	view, err := h.svc.CreateCustomRole(ctx, info.UserID, info.OrganizationID, req.Msg.Name, req.Msg.Description, req.Msg.PermissionKeys)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.CreateCustomRoleResponse{Role: roleViewToProto(view)}), nil
}

func (h *Handler) UpdateCustomRole(ctx context.Context, req *connect.Request[pb.UpdateCustomRoleRequest]) (*connect.Response[pb.UpdateCustomRoleResponse], error) {
	info, err := middleware.RequirePermission(ctx, authzDomain.PermRoleManage, authzDomain.ResourceContext{})
	if err != nil {
		return nil, err
	}
	roleID, err := parseRoleID(req.Msg.RoleId)
	if err != nil {
		return nil, err
	}
	view, err := h.svc.UpdateCustomRole(ctx, info.UserID, info.OrganizationID, roleID, req.Msg.Name, req.Msg.Description, req.Msg.PermissionKeys)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.UpdateCustomRoleResponse{Role: roleViewToProto(view)}), nil
}

func (h *Handler) DeleteCustomRole(ctx context.Context, req *connect.Request[pb.DeleteCustomRoleRequest]) (*connect.Response[pb.DeleteCustomRoleResponse], error) {
	info, err := middleware.RequirePermission(ctx, authzDomain.PermRoleManage, authzDomain.ResourceContext{})
	if err != nil {
		return nil, err
	}
	roleID, err := parseRoleID(req.Msg.RoleId)
	if err != nil {
		return nil, err
	}
	if err := h.svc.DeleteCustomRole(ctx, info.UserID, info.OrganizationID, roleID); err != nil {
		return nil, err
	}
	return connect.NewResponse(&pb.DeleteCustomRoleResponse{}), nil
}

func (h *Handler) AssignRole(_ context.Context, _ *connect.Request[pb.AssignRoleRequest]) (*connect.Response[pb.AssignRoleResponse], error) {
	return nil, fleeterror.NewUnimplementedError("authz.AssignRole lands with the Team-page assignment flow")
}

func (h *Handler) UnassignRole(_ context.Context, _ *connect.Request[pb.UnassignRoleRequest]) (*connect.Response[pb.UnassignRoleResponse], error) {
	return nil, fleeterror.NewUnimplementedError("authz.UnassignRole lands with the Team-page assignment flow")
}

func (h *Handler) ListUserAssignments(_ context.Context, _ *connect.Request[pb.ListUserAssignmentsRequest]) (*connect.Response[pb.ListUserAssignmentsResponse], error) {
	return nil, fleeterror.NewUnimplementedError("authz.ListUserAssignments lands with the Team-page assignment flow")
}

func roleViewToProto(v authzDomain.RoleView) *pb.Role {
	return &pb.Role{
		RoleId:         strconv.FormatInt(v.ID, 10),
		Name:           v.Name,
		Description:    v.Description,
		PermissionKeys: v.PermissionKeys,
		Builtin:        v.Builtin,
		BuiltinKey:     builtinKeyToProto(v.BuiltinKey),
		MemberCount:    v.MemberCount,
		UpdatedAt:      timestamppb.New(v.UpdatedAt),
	}
}

// builtinKeyToProto maps the seed identifier stored in role.builtin_key
// (the canonical string form, kept stable across migrations) to the
// wire enum. An unknown / empty value lands on UNSPECIFIED so older
// custom rows missing the column read as "no built-in identity" rather
// than aliasing to a real key.
func builtinKeyToProto(key string) pb.BuiltinKey {
	switch authzDomain.BuiltinKey(key) {
	case authzDomain.BuiltinKeySuperAdmin:
		return pb.BuiltinKey_BUILTIN_KEY_SUPER_ADMIN
	case authzDomain.BuiltinKeyAdmin:
		return pb.BuiltinKey_BUILTIN_KEY_ADMIN
	case authzDomain.BuiltinKeyFieldTech:
		return pb.BuiltinKey_BUILTIN_KEY_FIELD_TECH
	default:
		return pb.BuiltinKey_BUILTIN_KEY_UNSPECIFIED
	}
}

// roleIDPattern locks parseRoleID to the canonical base-10 form. Without
// it strconv.ParseInt would accept "+123" / leading whitespace / unicode
// digits, all of which round-trip to a different string than the one we
// emit in roleViewToProto.
var roleIDPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

func parseRoleID(s string) (int64, error) {
	if !roleIDPattern.MatchString(s) {
		return 0, fleeterror.NewInvalidArgumentError("invalid role_id")
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fleeterror.NewInvalidArgumentError("invalid role_id")
	}
	return id, nil
}
