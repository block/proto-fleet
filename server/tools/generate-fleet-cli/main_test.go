package main

import (
	"os"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestGoPackageInfoUsesExplicitGoPackage(t *testing.T) {
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    stringPtr("ping/v1/ping.proto"),
		Package: stringPtr("ping.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: stringPtr("github.com/block/proto-fleet/server/generated/grpc/ping/v1;pingv1"),
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	importPath, alias, err := goPackageInfo(file)
	if err != nil {
		t.Fatal(err)
	}
	if importPath != "github.com/block/proto-fleet/server/generated/grpc/ping/v1" {
		t.Fatalf("importPath = %q", importPath)
	}
	if alias != "pingv1" {
		t.Fatalf("alias = %q", alias)
	}
}

func TestGoPackageInfoInfersLocalGeneratedPath(t *testing.T) {
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    stringPtr("fleetmanagement/v1/fleetmanagement.proto"),
		Package: stringPtr("fleetmanagement.v1"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	importPath, alias, err := goPackageInfo(file)
	if err != nil {
		t.Fatal(err)
	}
	if importPath != "github.com/block/proto-fleet/server/generated/grpc/fleetmanagement/v1" {
		t.Fatalf("importPath = %q", importPath)
	}
	if alias != "fleetmanagementv1" {
		t.Fatalf("alias = %q", alias)
	}
}

func TestAuthPolicyConstDefaultsToAuthenticated(t *testing.T) {
	got, err := authPolicyConst("")
	if err != nil {
		t.Fatalf("authPolicyConst(\"\") error = %v", err)
	}
	if got != "generatedAuthAuthenticated" {
		t.Fatalf("authPolicyConst(\"\") = %q, want generatedAuthAuthenticated", got)
	}
}

func TestAuthPolicyConstSupportsPolicyNames(t *testing.T) {
	tests := []struct {
		name string
		auth string
		want string
	}{
		{name: "unauthenticated", auth: "unauthenticated", want: "generatedAuthUnauthenticated"},
		{name: "authenticated", auth: "authenticated", want: "generatedAuthAuthenticated"},
		{name: "session only", auth: "session_only", want: "generatedAuthSessionOnly"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := authPolicyConst(tt.auth)
			if err != nil {
				t.Fatalf("authPolicyConst(%q) error = %v", tt.auth, err)
			}
			if got != tt.want {
				t.Fatalf("authPolicyConst(%q) = %q, want %q", tt.auth, got, tt.want)
			}
		})
	}
}

func TestAuthPolicyConstRejectsLegacyModeValues(t *testing.T) {
	for _, auth := range []string{"anonymous", "bearer", "session"} {
		t.Run(auth, func(t *testing.T) {
			got, err := authPolicyConst(auth)
			if err == nil {
				t.Fatalf("authPolicyConst(%q) = %q, want error", auth, got)
			}
			if !strings.Contains(err.Error(), "invalid auth policy") {
				t.Fatalf("authPolicyConst(%q) error = %v, want invalid auth policy", auth, err)
			}
		})
	}
}

func TestParseCommandsManifestRejectsUnknownFields(t *testing.T) {
	_, err := parseCommandsManifest([]byte(`{"services": {}}`))
	if err == nil || !strings.Contains(err.Error(), `unknown field "services"`) {
		t.Fatalf("parseCommandsManifest error = %v, want unknown services field", err)
	}
}

func TestParseCommandsManifestRejectsDuplicateCommandNames(t *testing.T) {
	_, err := parseCommandsManifest([]byte(`{
		"commands": [
			{"method": "/test.v1.TestService/Ping", "group": "test", "command": "ping"},
			{"method": "/test.v1.TestService/Pong", "group": "test", "command": "ping"}
		]
	}`))
	if err == nil || !strings.Contains(err.Error(), `duplicate generated command "test ping"`) {
		t.Fatalf("parseCommandsManifest error = %v, want duplicate command error", err)
	}
}

func TestParseCommandsManifestSupportsSubgroups(t *testing.T) {
	manifest, err := parseCommandsManifest([]byte(`{
		"commands": [
			{"method": "/test.v1.TestService/Ping", "group": "alerts", "subgroup": "channels", "command": "list"}
		]
	}`))
	if err != nil {
		t.Fatalf("parseCommandsManifest error = %v", err)
	}
	if got := manifest.Commands[0].Subgroup; got != "channels" {
		t.Fatalf("subgroup = %q, want channels", got)
	}
}

func TestParseCommandsManifestRejectsInvalidSubgroup(t *testing.T) {
	_, err := parseCommandsManifest([]byte(`{
		"commands": [
			{"method": "/test.v1.TestService/Ping", "group": "alerts", "subgroup": "Bad Group", "command": "list"}
		]
	}`))
	if err == nil || !strings.Contains(err.Error(), `invalid subgroup "Bad Group"`) {
		t.Fatalf("parseCommandsManifest error = %v, want invalid subgroup", err)
	}
}

func TestParseCommandsManifestRejectsCommandSubgroupCollision(t *testing.T) {
	_, err := parseCommandsManifest([]byte(`{
		"commands": [
			{"method": "/test.v1.TestService/Ping", "group": "alerts", "command": "channels"},
			{"method": "/test.v1.TestService/Pong", "group": "alerts", "subgroup": "channels", "command": "list"}
		]
	}`))
	if err == nil || !strings.Contains(err.Error(), `uses "channels" as both a command and subgroup`) {
		t.Fatalf("parseCommandsManifest error = %v, want command/subgroup collision", err)
	}
}

func TestParseCommandsManifestRejectsDuplicateFieldFlags(t *testing.T) {
	_, err := parseCommandsManifest([]byte(`{
		"commands": [
			{
				"method": "/test.v1.TestService/Ping",
				"group": "test",
				"command": "ping",
				"field_flags": [
					{"path": "password", "flag": "password-stdin", "kind": "secret"},
					{"path": "backup_password", "flag": "password-stdin", "kind": "secret"}
				]
			}
		]
	}`))
	if err == nil || !strings.Contains(err.Error(), `duplicate field flag "password-stdin"`) {
		t.Fatalf("parseCommandsManifest error = %v, want duplicate field flag error", err)
	}
}

func TestParseCommandsManifestRejectsUnknownFieldFlagKind(t *testing.T) {
	_, err := parseCommandsManifest([]byte(`{
		"commands": [
			{
				"method": "/test.v1.TestService/Ping",
				"group": "test",
				"command": "ping",
				"field_flags": [
					{"path": "password", "flag": "password-file", "kind": "file"}
				]
			}
		]
	}`))
	if err == nil || !strings.Contains(err.Error(), `unsupported field flag kind "file"`) {
		t.Fatalf("parseCommandsManifest error = %v, want unsupported field flag kind error", err)
	}
}

func TestParseCommandsManifestRejectsLegacyAuthPolicy(t *testing.T) {
	_, err := parseCommandsManifest([]byte(`{
		"commands": [
			{"method": "/test.v1.TestService/Ping", "group": "test", "command": "ping", "auth": "bearer"}
		]
	}`))
	if err == nil || !strings.Contains(err.Error(), `invalid auth policy "bearer"`) {
		t.Fatalf("parseCommandsManifest error = %v, want invalid bearer auth policy", err)
	}
}

func TestCommandsManifestSessionOnlyMethods(t *testing.T) {
	data, err := os.ReadFile("commands.json")
	if err != nil {
		t.Fatalf("read commands.json: %v", err)
	}
	manifest, err := parseCommandsManifest(data)
	if err != nil {
		t.Fatalf("parse commands.json: %v", err)
	}
	var got []string
	for _, command := range manifest.Commands {
		if command.Auth == "session_only" {
			got = append(got, command.Method)
		}
	}
	sort.Strings(got)
	want := []string{
		"/alerts.v1.ChannelService/CreateChannel",
		"/alerts.v1.ChannelService/DeleteChannel",
		"/alerts.v1.ChannelService/ListChannels",
		"/alerts.v1.ChannelService/TestChannel",
		"/alerts.v1.ChannelService/UpdateChannel",
		"/alerts.v1.HistoryService/ListAlerts",
		"/alerts.v1.MaintenanceWindowService/CreateMaintenanceWindow",
		"/alerts.v1.MaintenanceWindowService/DeleteMaintenanceWindow",
		"/alerts.v1.MaintenanceWindowService/ListMaintenanceWindows",
		"/alerts.v1.MaintenanceWindowService/UpdateMaintenanceWindow",
		"/alerts.v1.RuleService/ListRules",
		"/alerts.v1.RuleService/PauseRule",
		"/alerts.v1.RuleService/ResumeRule",
		"/authz.v1.AuthzService/CreateCustomRole",
		"/authz.v1.AuthzService/DeleteCustomRole",
		"/authz.v1.AuthzService/ListPermissions",
		"/authz.v1.AuthzService/ListRoles",
		"/authz.v1.AuthzService/UpdateCustomRole",
		"/curtailment.v1.CurtailmentService/AdminTerminateEvent",
		"/curtailment.v1.CurtailmentService/CreateMqttCurtailmentSource",
		"/curtailment.v1.CurtailmentService/ForceReleaseCurtailmentOwnership",
		"/curtailment.v1.CurtailmentService/TestMqttCurtailmentSourceConnection",
		"/curtailment.v1.CurtailmentService/UpdateMqttCurtailmentSource",
		"/serverlog.v1.ServerLogService/ListServerLogs",
	}
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("session-only methods:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestBuildGroupsAllowsRepeatedMethodForDifferentCommands(t *testing.T) {
	file := testServiceFile(t)
	files := []protoreflect.FileDescriptor{file}
	messages, enums, err := buildTypeIndexes(files)
	if err != nil {
		t.Fatal(err)
	}

	groups, report, err := buildGroups(files, messages, enums, commandsManifest{
		Commands: []commandSpec{
			{Method: "/test.v1.TestService/Ping", Group: "alpha", Command: "ping"},
			{Method: "/test.v1.TestService/Ping", Group: "beta", Command: "ping"},
		},
	})
	if err != nil {
		t.Fatalf("buildGroups error = %v, want success", err)
	}
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	if report.Summary["generated"] != 2 {
		t.Fatalf("generated count = %d, want 2", report.Summary["generated"])
	}
}

func TestBuildGroupsPlacesCommandsInSubgroupsAndReportsPath(t *testing.T) {
	file := testServiceFile(t)
	files := []protoreflect.FileDescriptor{file}
	messages, enums, err := buildTypeIndexes(files)
	if err != nil {
		t.Fatal(err)
	}

	groups, report, err := buildGroups(files, messages, enums, commandsManifest{
		Commands: []commandSpec{{
			Method: "/test.v1.TestService/Ping", Group: "alerts", Subgroup: "channels", Command: "list",
		}},
	})
	if err != nil {
		t.Fatalf("buildGroups error = %v", err)
	}
	if len(groups) != 1 || len(groups[0].Subgroups["channels"]) != 1 || len(groups[0].CommandExprs) != 0 {
		t.Fatalf("groups = %#v, want one nested command", groups)
	}
	if len(report.Methods) != 1 || report.Methods[0].Subgroup != "channels" {
		t.Fatalf("report = %#v, want channels subgroup", report.Methods)
	}
}

func TestBuildGroupsRejectsJSONOptionalForSimpleRequest(t *testing.T) {
	file := testServiceFile(t)
	files := []protoreflect.FileDescriptor{file}
	messages, enums, err := buildTypeIndexes(files)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = buildGroups(files, messages, enums, commandsManifest{
		Commands: []commandSpec{{
			Method: "/test.v1.TestService/Ping", Group: "test", Command: "ping", JSONOptional: true,
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "json_optional requires a JSON-only request") {
		t.Fatalf("buildGroups error = %v, want json_optional validation", err)
	}
}

func TestBuildGroupsEmitsSecretFieldStdinFlag(t *testing.T) {
	file := testSecretServiceFile(t)
	files := []protoreflect.FileDescriptor{file}
	messages, enums, err := buildTypeIndexes(files)
	if err != nil {
		t.Fatal(err)
	}

	groups, report, err := buildGroups(files, messages, enums, commandsManifest{
		Commands: []commandSpec{
			{
				Method:  "/test.v1.TestService/CreateAdmin",
				Group:   "onboarding",
				Command: "create-admin",
				FieldFlags: []fieldFlagSpec{
					{
						Path:     "password",
						Flag:     "password-stdin",
						Kind:     "secret",
						Required: true,
						Prompt:   true,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("buildGroups error = %v, want success", err)
	}
	if report.Summary["generated"] != 1 {
		t.Fatalf("generated count = %d, want 1", report.Summary["generated"])
	}
	if len(groups) != 1 || len(groups[0].CommandExprs) != 1 {
		t.Fatalf("groups = %#v, want one generated command", groups)
	}
	expr := groups[0].CommandExprs[0]
	for _, want := range []string{
		`&cli.BoolFlag{Name: "password-stdin"`,
		`generatedReadSecret(cmd, "password-stdin", "password")`,
		`req.Password = secretPassword`,
	} {
		if !strings.Contains(expr, want) {
			t.Fatalf("generated expr missing %q:\n%s", want, expr)
		}
	}
	if strings.Contains(expr, `&cli.StringFlag{Name: "password"`) {
		t.Fatalf("generated expr exposed argv password flag:\n%s", expr)
	}
}

func TestBuildGroupsEmitsNestedFieldFlags(t *testing.T) {
	file := testPoolServiceFile(t)
	files := []protoreflect.FileDescriptor{file}
	messages, enums, err := buildTypeIndexes(files)
	if err != nil {
		t.Fatal(err)
	}

	groups, report, err := buildGroups(files, messages, enums, commandsManifest{
		Commands: []commandSpec{
			{
				Method:  "/pools.v1.PoolsService/CreatePool",
				Group:   "pools",
				Command: "create",
				FieldFlags: []fieldFlagSpec{
					{Path: "pool_config.pool_name", Flag: "pool-name", Usage: "pool name"},
					{Path: "pool_config.url", Flag: "url", Usage: "url"},
					{Path: "pool_config.username", Flag: "username", Usage: "username"},
					{Path: "pool_config.password", Flag: "pool-password-stdin", Usage: "Read pool password from stdin", Kind: "secret"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("buildGroups error = %v, want success", err)
	}
	if report.Summary["generated_json_fallback"] != 1 {
		t.Fatalf("generated_json_fallback count = %d, want 1", report.Summary["generated_json_fallback"])
	}
	if len(groups) != 1 || len(groups[0].CommandExprs) != 1 {
		t.Fatalf("groups = %#v, want one generated command", groups)
	}
	expr := groups[0].CommandExprs[0]
	for _, want := range []string{
		`&cli.StringFlag{Name: "pool-name", Usage: "pool name"}`,
		`&cli.StringFlag{Name: "url", Usage: "url"}`,
		`&cli.StringFlag{Name: "username", Usage: "username"}`,
		`&cli.BoolFlag{Name: "pool-password-stdin", Usage: "Read pool password from stdin"}`,
		`req.PoolConfig = &poolsv1.PoolConfig{}`,
		`req.PoolConfig.PoolName = cmd.String("pool-name")`,
		`req.PoolConfig.Url = cmd.String("url")`,
		`req.PoolConfig.Username = cmd.String("username")`,
		`generatedReadSecret(cmd, "pool-password-stdin", "pool password")`,
		`req.PoolConfig.Password = wrapperspb.String(secretPoolConfigPassword)`,
	} {
		if !strings.Contains(expr, want) {
			t.Fatalf("generated expr missing %q:\n%s", want, expr)
		}
	}
	if strings.Contains(expr, `&cli.StringFlag{Name: "password"`) {
		t.Fatalf("generated expr exposed argv password flag:\n%s", expr)
	}
}

func TestBuildGroupsRejectsUnknownFieldFlagPath(t *testing.T) {
	file := testSecretServiceFile(t)
	files := []protoreflect.FileDescriptor{file}
	messages, enums, err := buildTypeIndexes(files)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = buildGroups(files, messages, enums, commandsManifest{
		Commands: []commandSpec{
			{
				Method:  "/test.v1.TestService/CreateAdmin",
				Group:   "onboarding",
				Command: "create-admin",
				FieldFlags: []fieldFlagSpec{
					{Path: "missing", Flag: "missing", Kind: "secret"},
				},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), `unknown root field "missing"`) {
		t.Fatalf("buildGroups error = %v, want unknown field flag path error", err)
	}
}

func TestBuildGroupsRejectsInvalidFieldFlagType(t *testing.T) {
	file := testSecretServiceFile(t)
	files := []protoreflect.FileDescriptor{file}
	messages, enums, err := buildTypeIndexes(files)
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = buildGroups(files, messages, enums, commandsManifest{
		Commands: []commandSpec{
			{
				Method:  "/test.v1.TestService/CreateAdmin",
				Group:   "onboarding",
				Command: "create-admin",
				FieldFlags: []fieldFlagSpec{
					{Path: "username", Flag: "username-stdin", Kind: "secret"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("buildGroups error = %v, want string secret field success", err)
	}

	_, _, err = buildGroups(files, messages, enums, commandsManifest{
		Commands: []commandSpec{
			{
				Method:  "/test.v1.TestService/CreateAdmin",
				Group:   "onboarding",
				Command: "create-admin",
				FieldFlags: []fieldFlagSpec{
					{Path: "username", Flag: "username", Kind: "string"},
				},
				FixedFields: map[string]string{"username": "fixed"},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), `unknown root field "username"`) {
		t.Fatalf("buildGroups error = %v, want field flag ignored by fixed field to fail loudly", err)
	}
}

func TestBuildGroupsRendersDefaultFieldsBeforeFlagOverrides(t *testing.T) {
	file := testPageSizeServiceFile(t)
	files := []protoreflect.FileDescriptor{file}
	messages, enums, err := buildTypeIndexes(files)
	if err != nil {
		t.Fatal(err)
	}

	groups, _, err := buildGroups(files, messages, enums, commandsManifest{
		Commands: []commandSpec{
			{
				Method:        "/test.v1.TestService/List",
				Group:         "test",
				Command:       "list",
				DefaultFields: map[string]string{"page_size": "100"},
			},
		},
	})
	if err != nil {
		t.Fatalf("buildGroups error = %v, want success", err)
	}
	if len(groups) != 1 || len(groups[0].CommandExprs) != 1 {
		t.Fatalf("generated groups = %#v, want one command expression", groups)
	}
	expr := groups[0].CommandExprs[0]
	defaultSnippet := "if req.PageSize == 0 {\n\t\t\treq.PageSize = int32(100)\n\t\t}"
	overrideSnippet := "if cmd.IsSet(\"page-size\") {\n\t\t\treq.PageSize = int32(cmd.Int(\"page-size\"))\n\t\t}"
	defaultIndex := strings.Index(expr, defaultSnippet)
	overrideIndex := strings.Index(expr, overrideSnippet)
	if defaultIndex < 0 {
		t.Fatalf("generated command missing default snippet:\n%s", expr)
	}
	if overrideIndex < 0 {
		t.Fatalf("generated command missing override snippet:\n%s", expr)
	}
	if defaultIndex > overrideIndex {
		t.Fatalf("default_fields assignment should appear before flag override:\n%s", expr)
	}
}

func testPoolServiceFile(t *testing.T) protoreflect.FileDescriptor {
	t.Helper()
	files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			protodesc.ToFileDescriptorProto(wrapperspb.File_google_protobuf_wrappers_proto),
			{
				Name:    stringPtr("pools/v1/pools.proto"),
				Syntax:  stringPtr("proto3"),
				Package: stringPtr("pools.v1"),
				Options: &descriptorpb.FileOptions{
					GoPackage: stringPtr("github.com/block/proto-fleet/server/generated/grpc/pools/v1;poolsv1"),
				},
				Dependency: []string{"google/protobuf/wrappers.proto"},
				MessageType: []*descriptorpb.DescriptorProto{
					{
						Name: stringPtr("PoolConfig"),
						Field: []*descriptorpb.FieldDescriptorProto{
							{
								Name:   stringPtr("url"),
								Number: int32Ptr(1),
								Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
								Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
							},
							{
								Name:   stringPtr("username"),
								Number: int32Ptr(2),
								Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
								Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
							},
							{
								Name:     stringPtr("password"),
								Number:   int32Ptr(3),
								Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
								Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
								TypeName: stringPtr(".google.protobuf.StringValue"),
							},
							{
								Name:   stringPtr("pool_name"),
								Number: int32Ptr(4),
								Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
								Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
							},
						},
					},
					{
						Name: stringPtr("CreatePoolRequest"),
						Field: []*descriptorpb.FieldDescriptorProto{
							{
								Name:     stringPtr("pool_config"),
								Number:   int32Ptr(1),
								Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
								Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
								TypeName: stringPtr(".pools.v1.PoolConfig"),
							},
						},
					},
					{Name: stringPtr("CreatePoolResponse")},
				},
				Service: []*descriptorpb.ServiceDescriptorProto{
					{
						Name: stringPtr("PoolsService"),
						Method: []*descriptorpb.MethodDescriptorProto{
							{
								Name:       stringPtr("CreatePool"),
								InputType:  stringPtr(".pools.v1.CreatePoolRequest"),
								OutputType: stringPtr(".pools.v1.CreatePoolResponse"),
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	file, err := files.FindFileByPath("pools/v1/pools.proto")
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func testServiceFile(t *testing.T) protoreflect.FileDescriptor {
	t.Helper()
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    stringPtr("test/v1/test.proto"),
		Syntax:  stringPtr("proto3"),
		Package: stringPtr("test.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: stringPtr("github.com/block/proto-fleet/server/generated/grpc/test/v1;testv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: stringPtr("PingRequest")},
			{Name: stringPtr("PingResponse")},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: stringPtr("TestService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       stringPtr("Ping"),
						InputType:  stringPtr(".test.v1.PingRequest"),
						OutputType: stringPtr(".test.v1.PingResponse"),
					},
				},
			},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func testPageSizeServiceFile(t *testing.T) protoreflect.FileDescriptor {
	t.Helper()
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    stringPtr("test/v1/test.proto"),
		Syntax:  stringPtr("proto3"),
		Package: stringPtr("test.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: stringPtr("github.com/block/proto-fleet/server/generated/grpc/test/v1;testv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: stringPtr("ListRequest"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   stringPtr("page_size"),
						Number: int32Ptr(1),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
					},
				},
			},
			{Name: stringPtr("ListResponse")},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: stringPtr("TestService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       stringPtr("List"),
						InputType:  stringPtr(".test.v1.ListRequest"),
						OutputType: stringPtr(".test.v1.ListResponse"),
					},
				},
			},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func testSecretServiceFile(t *testing.T) protoreflect.FileDescriptor {
	t.Helper()
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:    stringPtr("test/v1/test.proto"),
		Syntax:  stringPtr("proto3"),
		Package: stringPtr("test.v1"),
		Options: &descriptorpb.FileOptions{
			GoPackage: stringPtr("github.com/block/proto-fleet/server/generated/grpc/test/v1;testv1"),
		},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: stringPtr("CreateAdminRequest"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name:   stringPtr("username"),
						Number: int32Ptr(1),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
					{
						Name:   stringPtr("password"),
						Number: int32Ptr(2),
						Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						Type:   descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
					},
				},
			},
			{Name: stringPtr("CreateAdminResponse")},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: stringPtr("TestService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       stringPtr("CreateAdmin"),
						InputType:  stringPtr(".test.v1.CreateAdminRequest"),
						OutputType: stringPtr(".test.v1.CreateAdminResponse"),
					},
				},
			},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func stringPtr(value string) *string {
	return &value
}

func int32Ptr(value int32) *int32 {
	return &value
}
