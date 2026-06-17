package notifications

import (
	"context"
	"errors"
	"math"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	notificationsv1 "github.com/block/proto-fleet/server/generated/grpc/notifications/v1"
	"github.com/block/proto-fleet/server/generated/grpc/notifications/v1/notificationsv1connect"
	"github.com/block/proto-fleet/server/internal/domain/authz"
	"github.com/block/proto-fleet/server/internal/domain/fleeterror"
	"github.com/block/proto-fleet/server/internal/domain/notificationhistory"
	notifications "github.com/block/proto-fleet/server/internal/domain/notifications"
	"github.com/block/proto-fleet/server/internal/handlers/middleware"
)

type Handler struct {
	svc     *notifications.Service
	history notificationhistory.Lister
}

func NewHandler(svc *notifications.Service, history notificationhistory.Lister) *Handler {
	return &Handler{svc: svc, history: history}
}

var _ notificationsv1connect.ChannelServiceHandler = (*Handler)(nil)

func (h *Handler) authorize(ctx context.Context, permission string) (int64, error) {
	info, err := middleware.RequirePermission(ctx, permission, authz.ResourceContext{})
	if err != nil {
		return 0, err
	}
	if info.OrganizationID == 0 {
		return 0, fleeterror.NewUnauthenticatedError("organization id missing on session")
	}
	return info.OrganizationID, nil
}

func mapErr(err error) error {
	if errors.Is(err, notifications.ErrNotFound) {
		return fleeterror.NewNotFoundError(err.Error())
	}
	return err
}

func (h *Handler) ListChannels(ctx context.Context, _ *connect.Request[notificationsv1.ListChannelsRequest]) (*connect.Response[notificationsv1.ListChannelsResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermNotificationRead)
	if err != nil {
		return nil, err
	}
	channels, err := h.svc.ListChannels(ctx, orgID)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]*notificationsv1.Channel, 0, len(channels))
	for _, c := range channels {
		out = append(out, channelToProto(c))
	}
	return connect.NewResponse(&notificationsv1.ListChannelsResponse{Channels: out}), nil
}

func (h *Handler) CreateChannel(ctx context.Context, req *connect.Request[notificationsv1.CreateChannelRequest]) (*connect.Response[notificationsv1.CreateChannelResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermNotificationManage)
	if err != nil {
		return nil, err
	}
	dom, err := protoToChannel("", req.Msg.GetName(), req.Msg.GetKind(), req.Msg.GetWebhook(), req.Msg.GetSmtp(), req.Msg.GetSlack())
	if err != nil {
		return nil, err
	}
	created, err := h.svc.CreateChannel(ctx, orgID, dom)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&notificationsv1.CreateChannelResponse{Channel: channelToProto(*created)}), nil
}

func (h *Handler) UpdateChannel(ctx context.Context, req *connect.Request[notificationsv1.UpdateChannelRequest]) (*connect.Response[notificationsv1.UpdateChannelResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermNotificationManage)
	if err != nil {
		return nil, err
	}
	dom, err := protoToChannel(req.Msg.GetId(), req.Msg.GetName(), req.Msg.GetKind(), req.Msg.GetWebhook(), req.Msg.GetSmtp(), req.Msg.GetSlack())
	if err != nil {
		return nil, err
	}
	updated, err := h.svc.UpdateChannel(ctx, orgID, dom)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&notificationsv1.UpdateChannelResponse{Channel: channelToProto(*updated)}), nil
}

func (h *Handler) DeleteChannel(ctx context.Context, req *connect.Request[notificationsv1.DeleteChannelRequest]) (*connect.Response[notificationsv1.DeleteChannelResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermNotificationManage)
	if err != nil {
		return nil, err
	}
	if err := h.svc.DeleteChannel(ctx, orgID, req.Msg.GetId()); err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&notificationsv1.DeleteChannelResponse{}), nil
}

func (h *Handler) TestChannel(ctx context.Context, req *connect.Request[notificationsv1.TestChannelRequest]) (*connect.Response[notificationsv1.TestChannelResponse], error) {
	orgID, err := h.authorize(ctx, authz.PermNotificationManage)
	if err != nil {
		return nil, err
	}
	// A saved-channel test needs only the id; TestChannel loads the stored contact point and ignores kind/config.
	dom := notifications.Channel{ID: req.Msg.GetId()}
	if dom.ID == "" {
		dom, err = protoToChannel("", "", req.Msg.GetKind(), req.Msg.GetWebhook(), req.Msg.GetSmtp(), req.Msg.GetSlack())
		if err != nil {
			return nil, err
		}
	}
	ok, code, errMsg, err := h.svc.TestChannel(ctx, orgID, dom)
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&notificationsv1.TestChannelResponse{
		Ok:           ok,
		Error:        errMsg,
		ResponseCode: httpStatusToInt32(code),
	}), nil
}

func channelToProto(c notifications.Channel) *notificationsv1.Channel {
	out := &notificationsv1.Channel{
		Id:              c.ID,
		OrganizationId:  c.OrganizationID,
		Name:            c.Name,
		Kind:            channelKindToProto(c.Kind),
		CreatedAt:       timestamppb.New(c.CreatedAt),
		UpdatedAt:       timestamppb.New(c.UpdatedAt),
		ValidationState: validationStateToProto(c.ValidationState),
		ValidationError: c.ValidationError,
		HasSecret:       c.HasSecret,
	}
	if c.ValidatedAt != nil {
		out.ValidatedAt = timestamppb.New(*c.ValidatedAt)
	}
	if c.Webhook != nil {
		out.Webhook = &notificationsv1.WebhookConfig{Url: c.Webhook.URL}
	}
	if c.SMTP != nil {
		out.Smtp = &notificationsv1.SmtpConfig{
			Host:     c.SMTP.Host,
			Port:     c.SMTP.Port,
			Username: c.SMTP.Username,
			From:     c.SMTP.From,
			To:       c.SMTP.To,
		}
	}
	if c.Slack != nil {
		// webhook_url deliberately omitted: it's the secret.
		out.Slack = &notificationsv1.SlackConfig{}
	}
	return out
}

func protoToChannel(id, name string, kind notificationsv1.ChannelKind, wh *notificationsv1.WebhookConfig, smtp *notificationsv1.SmtpConfig, slack *notificationsv1.SlackConfig) (notifications.Channel, error) {
	dk, err := protoToChannelKind(kind)
	if err != nil {
		return notifications.Channel{}, err
	}
	dom := notifications.Channel{ID: id, Name: name, Kind: dk}
	if wh != nil {
		dom.Webhook = &notifications.WebhookConfig{URL: wh.GetUrl(), BearerHeader: wh.GetBearerHeader()}
	}
	if smtp != nil {
		dom.SMTP = &notifications.SMTPConfig{
			Host:     smtp.GetHost(),
			Port:     smtp.GetPort(),
			Username: smtp.GetUsername(),
			From:     smtp.GetFrom(),
			To:       smtp.GetTo(),
			Password: smtp.GetPassword(),
		}
	}
	if slack != nil {
		dom.Slack = &notifications.SlackConfig{WebhookURL: slack.GetWebhookUrl()}
	}
	return dom, nil
}

func channelKindToProto(k notifications.ChannelKind) notificationsv1.ChannelKind {
	switch k {
	case notifications.ChannelKindWebhook:
		return notificationsv1.ChannelKind_CHANNEL_KIND_WEBHOOK
	case notifications.ChannelKindSMTP:
		return notificationsv1.ChannelKind_CHANNEL_KIND_SMTP
	case notifications.ChannelKindSlack:
		return notificationsv1.ChannelKind_CHANNEL_KIND_SLACK
	}
	return notificationsv1.ChannelKind_CHANNEL_KIND_UNSPECIFIED
}

func protoToChannelKind(k notificationsv1.ChannelKind) (notifications.ChannelKind, error) {
	switch k {
	case notificationsv1.ChannelKind_CHANNEL_KIND_WEBHOOK:
		return notifications.ChannelKindWebhook, nil
	case notificationsv1.ChannelKind_CHANNEL_KIND_SMTP:
		return notifications.ChannelKindSMTP, nil
	case notificationsv1.ChannelKind_CHANNEL_KIND_SLACK:
		return notifications.ChannelKindSlack, nil
	case notificationsv1.ChannelKind_CHANNEL_KIND_UNSPECIFIED:
	}
	return "", fleeterror.NewInvalidArgumentErrorf("unknown channel kind: %s", k)
}

func httpStatusToInt32(code int) int32 {
	if code < 0 {
		return 0
	}
	if code > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(code)
}

func validationStateToProto(s notifications.ValidationState) notificationsv1.ValidationState {
	switch s {
	case notifications.ValidationPending:
		return notificationsv1.ValidationState_VALIDATION_STATE_PENDING
	case notifications.ValidationOK:
		return notificationsv1.ValidationState_VALIDATION_STATE_OK
	case notifications.ValidationFailed:
		return notificationsv1.ValidationState_VALIDATION_STATE_FAILED
	}
	return notificationsv1.ValidationState_VALIDATION_STATE_UNSPECIFIED
}
