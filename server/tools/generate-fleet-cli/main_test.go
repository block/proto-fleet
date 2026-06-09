package main

import (
	"testing"

	"google.golang.org/protobuf/reflect/protodesc"
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

func stringPtr(value string) *string {
	return &value
}
