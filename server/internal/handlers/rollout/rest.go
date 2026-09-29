package rollout

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	pb "github.com/block/proto-fleet/server/generated/grpc/rollout/v1"
)

const (
	rolloutRPCPrefix = "/rollout.v1.RolloutService/"
	maxRESTBodyBytes = 16 << 20
)

type restRoute struct {
	pattern string
	method  string
	idField string
}

var restRoutes = []restRoute{
	{"GET /api/v1/release-channels", "ListReleaseChannels", ""},
	{"POST /api/v1/release-channels", "CreateReleaseChannel", ""},
	{"POST /api/v1/release-channels/preview-scope", "PreviewReleaseChannelScope", ""},
	{"GET /api/v1/release-channels/membership-conflicts", "ListReleaseChannelMembershipConflicts", ""},
	{"GET /api/v1/release-channels/{channelId}", "GetReleaseChannel", "channel_id"},
	{"PUT /api/v1/release-channels/{channelId}", "UpdateReleaseChannel", "channel_id"},
	{"DELETE /api/v1/release-channels/{channelId}", "DeleteReleaseChannel", "channel_id"},
	{"GET /api/v1/release-channels/{channelId}/model-groups", "ListReleaseChannelModelGroups", "channel_id"},
	{"GET /api/v1/release-channels/{channelId}/miners", "ListReleaseChannelMiners", "channel_id"},
	{"POST /api/v1/release-channels/{channelId}/firmware/preview", "PreviewReleaseChannelFirmware", "channel_id"},
	{"POST /api/v1/release-channels/{channelId}/firmware/apply", "ApplyReleaseChannelFirmware", "channel_id"},
	{"GET /api/v1/rollouts", "ListRollouts", ""},
	{"GET /api/v1/rollouts/events", "ListRolloutEvents", ""},
	{"GET /api/v1/rollouts/{rolloutId}", "GetRollout", "rollout_id"},
	{"GET /api/v1/rollouts/{rolloutId}/devices", "ListRolloutDevices", "rollout_id"},
	{"GET /api/v1/rollouts/{rolloutId}/events", "ListRolloutEvents", "rollout_id"},
	{"POST /api/v1/rollouts/{rolloutId}/continue", "ContinueRollout", "rollout_id"},
	{"POST /api/v1/rollouts/{rolloutId}/advance", "AdvanceRollout", "rollout_id"},
	{"POST /api/v1/rollouts/{rolloutId}/skip", "SkipRolloutDevices", "rollout_id"},
	{"POST /api/v1/rollouts/{rolloutId}/complete", "CompleteRollout", "rollout_id"},
	{"POST /api/v1/rollouts/{rolloutId}/pause", "PauseRollout", "rollout_id"},
	{"POST /api/v1/rollouts/{rolloutId}/resume", "ResumeRollout", "rollout_id"},
	{"POST /api/v1/rollouts/{rolloutId}/cancel", "CancelRollout", "rollout_id"},
	{"POST /api/v1/rollouts/{rolloutId}/retry", "RetryFailedRolloutDevices", "rollout_id"},
	{"POST /api/v1/rollouts/{rolloutId}/rollback", "RollbackReleaseChannelFirmware", "rollout_id"},
}

// RegisterRESTRoutes exposes resource URLs using the same protobuf JSON contract
// and Connect handler as the UI. Authentication, authorization, validation, HA
// admission and domain error details therefore have one implementation.
func RegisterRESTRoutes(mux *http.ServeMux, rpc http.Handler) {
	methods := pb.File_rollout_v1_rollout_proto.Services().ByName("RolloutService").Methods()
	for _, route := range restRoutes {
		descriptor := methods.ByName(protoreflect.Name(route.method)).Input()
		mux.HandleFunc(route.pattern, func(w http.ResponseWriter, r *http.Request) {
			message := dynamicpb.NewMessage(descriptor)
			if err := decodeRESTRequest(w, r, message, route.idField); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"code": "invalid_argument", "message": err.Error()})
				return
			}
			body, err := protojson.Marshal(message)
			if err != nil {
				http.Error(w, "could not encode rollout request", http.StatusInternalServerError)
				return
			}
			request := r.Clone(r.Context())
			request.Method = http.MethodPost
			request.URL.Path = rolloutRPCPrefix + route.method
			request.URL.RawPath, request.URL.RawQuery = "", ""
			request.RequestURI = request.URL.RequestURI()
			request.Body = io.NopCloser(bytes.NewReader(body))
			request.ContentLength = int64(len(body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Connect-Protocol-Version", "1")
			request.Header.Del("Content-Encoding")
			request.Header.Del("Content-Length")
			rpc.ServeHTTP(w, request)
		})
	}
}

func decodeRESTRequest(w http.ResponseWriter, r *http.Request, message *dynamicpb.Message, idField string) error {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return fmt.Errorf("invalid query: %w", err)
	}
	var body []byte
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			return fmt.Errorf("GET requests must use query parameters, not a body")
		}
		values := make(map[string]any, len(query))
		fields := message.Descriptor().Fields()
		for key, value := range query {
			field := fields.ByJSONName(key)
			if field == nil {
				field = fields.ByName(protoreflect.Name(key))
			}
			if field == nil || len(value) != 1 {
				return fmt.Errorf("unknown or repeated query parameter %q", key)
			}
			name := field.JSONName()
			if _, exists := values[name]; exists {
				return fmt.Errorf("repeated query parameter %q", name)
			}
			values[name] = value[0]
		}
		body, err = json.Marshal(values)
	} else {
		if len(query) != 0 {
			return fmt.Errorf("use a JSON body for this operation")
		}
		if r.Header.Get("Content-Encoding") != "" {
			return fmt.Errorf("compressed request bodies are not supported")
		}
		// Bound decoding while allowing large scopes with escaped identifiers.
		body, err = io.ReadAll(http.MaxBytesReader(w, r.Body, maxRESTBodyBytes))
		if err != nil {
			return fmt.Errorf("read JSON body: %w", err)
		}
		if len(bytes.TrimSpace(body)) == 0 {
			body = []byte("{}")
		} else if mediaType, _, e := mime.ParseMediaType(r.Header.Get("Content-Type")); e != nil || mediaType != "application/json" {
			return fmt.Errorf("Content-Type must be application/json")
		}
	}
	if err != nil {
		return fmt.Errorf("encode query parameters: %w", err)
	}
	if err := protojson.Unmarshal(body, message); err != nil {
		return fmt.Errorf("invalid request: %w", err)
	}
	if idField != "" {
		field := message.Descriptor().Fields().ByName(protoreflect.Name(idField))
		id, err := strconv.ParseInt(r.PathValue(field.JSONName()), 10, 64)
		if err != nil || id <= 0 {
			return fmt.Errorf("%s must be a positive integer", field.JSONName())
		}
		if message.Has(field) && message.Get(field).Int() != id {
			return fmt.Errorf("%s must match the URL", field.JSONName())
		}
		message.Set(field, protoreflect.ValueOfInt64(id))
	}
	return nil
}
