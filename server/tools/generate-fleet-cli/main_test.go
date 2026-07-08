package main

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
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

func stringPtr(value string) *string {
	return &value
}
