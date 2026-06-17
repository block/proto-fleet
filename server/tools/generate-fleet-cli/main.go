package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"unicode"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

const (
	modulePath           = "github.com/block/proto-fleet/server"
	generatedProtoRoot   = "generated/grpc"
	descriptorSetPath    = ".cache/fleet-cli/fleet-descriptor-set.bin"
	reportPath           = ".cache/fleet-cli/fleet-cli-report.json"
	overridesPath        = "server/tools/generate-fleet-cli/overrides.json"
	outputDir            = "server/cmd/fleetcli"
	groupTemplatePath    = "server/tools/generate-fleet-cli/templates/fleet-group.gotmpl"
	commandsTemplatePath = "server/tools/generate-fleet-cli/templates/fleet-commands.gotmpl"
)

type overridesFile struct {
	Services map[string]serviceOverride `json:"services"`
	Methods  map[string]methodOverride  `json:"methods"`
	Commands []commandOverride          `json:"commands"`
}

type serviceOverride struct {
	Group      string `json:"group,omitempty"`
	Auth       string `json:"auth,omitempty"`
	Skip       bool   `json:"skip,omitempty"`
	AllMethods bool   `json:"all_methods,omitempty"`
}

type methodOverride struct {
	Group    string `json:"group,omitempty"`
	Command  string `json:"command,omitempty"`
	Usage    string `json:"usage,omitempty"`
	Auth     string `json:"auth,omitempty"`
	Skip     bool   `json:"skip,omitempty"`
	JSONOnly bool   `json:"json_only,omitempty"`
}

type commandOverride struct {
	Method       string            `json:"method"`
	Group        string            `json:"group"`
	Command      string            `json:"command"`
	Usage        string            `json:"usage,omitempty"`
	Auth         string            `json:"auth,omitempty"`
	IgnoreFields []string          `json:"ignore_fields,omitempty"`
	FixedFields  map[string]string `json:"fixed_fields,omitempty"`
	JSONOnly     bool              `json:"json_only,omitempty"`
}

type importSpec struct {
	Alias string
	Path  string
}

type groupTemplateData struct {
	FuncName     string
	Name         string
	Usage        string
	Imports      []importSpec
	CommandExprs []string
}

type commandsTemplateData struct {
	GroupFuncNames []string
}

type groupSpec struct {
	Name         string
	FuncName     string
	Usage        string
	Imports      map[string]string
	CommandExprs []string
}

type methodRef struct {
	ServiceKey      string
	ServiceName     protoreflect.Name
	ServiceOverride serviceOverride
	Method          protoreflect.MethodDescriptor
}

type renderOptions struct {
	CommandName  string
	Usage        string
	Auth         string
	JSONOnly     bool
	IgnoreFields map[string]bool
	FixedFields  map[string]string
}

type messageInfo struct {
	GoImportPath string
	GoAlias      string
	GoIdent      string
	Descriptor   protoreflect.MessageDescriptor
}

type enumInfo struct {
	GoImportPath string
	GoAlias      string
	GoIdent      string
	Descriptor   protoreflect.EnumDescriptor
}

type generationReport struct {
	Summary map[string]int `json:"summary"`
	Methods []methodReport `json:"methods"`
}

type methodReport struct {
	Method  string `json:"method"`
	Status  string `json:"status"`
	Group   string `json:"group,omitempty"`
	Command string `json:"command,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

type renderResult struct {
	Expr    string
	Imports map[string]string
	Status  string
	Reason  string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "generate-fleet-cli: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	overrides, err := loadOverrides()
	if err != nil {
		return err
	}

	files, err := loadDescriptorFiles()
	if err != nil {
		return err
	}

	messages, enums, err := buildTypeIndexes(files)
	if err != nil {
		return err
	}

	groups, report, err := buildGroups(files, messages, enums, overrides)
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		return fmt.Errorf("no Fleet CLI groups were selected for generation")
	}

	generated, err := renderGroups(groups)
	if err != nil {
		return err
	}

	if err := renderCommandsFile(groups); err != nil {
		return err
	}
	generated["cmd_commands.go"] = true

	if err := removeStaleGeneratedFiles(generated); err != nil {
		return err
	}

	if err := renderReport(report); err != nil {
		return err
	}

	return nil
}

func loadOverrides() (overridesFile, error) {
	var overrides overridesFile
	data, err := os.ReadFile(overridesPath)
	if err != nil {
		return overrides, fmt.Errorf("read overrides: %w", err)
	}
	if err := json.Unmarshal(data, &overrides); err != nil {
		return overrides, fmt.Errorf("parse overrides: %w", err)
	}
	if overrides.Services == nil {
		overrides.Services = map[string]serviceOverride{}
	}
	if overrides.Methods == nil {
		overrides.Methods = map[string]methodOverride{}
	}
	if overrides.Commands == nil {
		overrides.Commands = []commandOverride{}
	}
	return overrides, nil
}

func loadDescriptorFiles() ([]protoreflect.FileDescriptor, error) {
	data, err := os.ReadFile(descriptorSetPath)
	if err != nil {
		return nil, fmt.Errorf("read descriptor set: %w", err)
	}

	var set descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(data, &set); err != nil {
		return nil, fmt.Errorf("parse descriptor set: %w", err)
	}

	registry, err := protodesc.NewFiles(&set)
	if err != nil {
		return nil, fmt.Errorf("build descriptor registry: %w", err)
	}

	var files []protoreflect.FileDescriptor
	registry.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		files = append(files, file)
		return true
	})

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path() < files[j].Path()
	})
	return files, nil
}

func buildTypeIndexes(files []protoreflect.FileDescriptor) (map[protoreflect.FullName]messageInfo, map[protoreflect.FullName]enumInfo, error) {
	messages := make(map[protoreflect.FullName]messageInfo)
	enums := make(map[protoreflect.FullName]enumInfo)

	for _, file := range files {
		importPath, alias, err := goPackageInfo(file)
		if err != nil {
			return nil, nil, err
		}

		indexMessages(messages, file.Messages(), importPath, alias, "")
		indexEnums(enums, file.Enums(), importPath, alias, "")
		for i := range file.Messages().Len() {
			indexNestedEnums(enums, file.Messages().Get(i), importPath, alias, toGoIdent(file.Messages().Get(i).Name()))
		}
	}

	return messages, enums, nil
}

func indexMessages(index map[protoreflect.FullName]messageInfo, messages protoreflect.MessageDescriptors, importPath, alias, prefix string) {
	for i := range messages.Len() {
		message := messages.Get(i)
		ident := toGoIdent(message.Name())
		if prefix != "" {
			ident = prefix + "_" + ident
		}
		index[message.FullName()] = messageInfo{
			GoImportPath: importPath,
			GoAlias:      alias,
			GoIdent:      ident,
			Descriptor:   message,
		}
		indexMessages(index, message.Messages(), importPath, alias, ident)
	}
}

func indexEnums(index map[protoreflect.FullName]enumInfo, enums protoreflect.EnumDescriptors, importPath, alias, prefix string) {
	for i := range enums.Len() {
		enum := enums.Get(i)
		ident := toGoIdent(enum.Name())
		if prefix != "" {
			ident = prefix + "_" + ident
		}
		index[enum.FullName()] = enumInfo{
			GoImportPath: importPath,
			GoAlias:      alias,
			GoIdent:      ident,
			Descriptor:   enum,
		}
	}
}

func indexNestedEnums(index map[protoreflect.FullName]enumInfo, message protoreflect.MessageDescriptor, importPath, alias, prefix string) {
	indexEnums(index, message.Enums(), importPath, alias, prefix)
	for i := range message.Messages().Len() {
		child := message.Messages().Get(i)
		childPrefix := prefix + "_" + toGoIdent(child.Name())
		indexNestedEnums(index, child, importPath, alias, childPrefix)
	}
}

func buildGroups(
	files []protoreflect.FileDescriptor,
	messages map[protoreflect.FullName]messageInfo,
	enums map[protoreflect.FullName]enumInfo,
	overrides overridesFile,
) ([]groupSpec, generationReport, error) {
	groupMap := make(map[string]*groupSpec)
	methodIndex := indexMethods(files, overrides.Services)
	methodKeys := sortedMethodKeys(methodIndex)
	var reports []methodReport
	generatedMethods := map[string]bool{}
	deferredMethods := map[string]bool{}

	// Methods listed in commands overrides are handled exclusively in the second loop.
	commandsMethodKeys := make(map[string]bool, len(overrides.Commands))
	for _, override := range overrides.Commands {
		commandsMethodKeys[strings.TrimPrefix(override.Method, "/")] = true
	}

	for _, methodKey := range methodKeys {
		ref := methodIndex[methodKey]
		if ref.Method.IsStreamingClient() || ref.Method.IsStreamingServer() {
			reports = append(reports, methodReport{
				Method: "/" + methodKey,
				Status: "deferred_streaming",
				Reason: "streaming RPCs are not yet generated",
			})
			deferredMethods[methodKey] = true
			continue
		}
		if ref.ServiceOverride.Skip {
			reports = append(reports, methodReport{
				Method: "/" + methodKey,
				Status: "deferred_service_skipped",
				Reason: "service is explicitly skipped in server/tools/generate-fleet-cli/overrides.json",
			})
			deferredMethods[methodKey] = true
			continue
		}
		if commandsMethodKeys[methodKey] {
			continue
		}
		methodOverride, hasMethodOverride := overrides.Methods[methodKey]
		if hasMethodOverride && methodOverride.Skip {
			reports = append(reports, methodReport{
				Method: "/" + methodKey,
				Status: "deferred_method_skipped",
				Reason: "method is explicitly skipped in server/tools/generate-fleet-cli/overrides.json",
			})
			deferredMethods[methodKey] = true
			continue
		}
		if !ref.ServiceOverride.AllMethods && !hasMethodOverride {
			continue
		}

		options := renderOptions{
			CommandName:  chooseCommandName(ref.ServiceName, ref.Method.Name(), methodOverride),
			Usage:        chooseUsage(ref.Method.Name(), methodOverride),
			Auth:         chooseOverrideAuth(ref.ServiceOverride, methodOverride.Auth),
			JSONOnly:     methodOverride.JSONOnly,
			IgnoreFields: map[string]bool{},
			FixedFields:  map[string]string{},
		}
		groupName := chooseGroupName(ref.ServiceName, ref.ServiceOverride, methodOverride)
		report, err := addGeneratedCommand(groupMap, groupName, ref, options, messages, enums)
		if err != nil {
			return nil, generationReport{}, err
		}
		reports = append(reports, report)
		if isDeferredStatus(report.Status) {
			deferredMethods[methodKey] = true
			continue
		}
		generatedMethods[methodKey] = true
	}

	for _, override := range overrides.Commands {
		methodKey := strings.TrimPrefix(override.Method, "/")
		ref, ok := methodIndex[methodKey]
		if !ok {
			return nil, generationReport{}, fmt.Errorf("unknown override method %q", override.Method)
		}
		options := renderOptions{
			CommandName:  override.Command,
			Usage:        override.Usage,
			Auth:         chooseOverrideAuth(ref.ServiceOverride, override.Auth),
			JSONOnly:     override.JSONOnly,
			IgnoreFields: sliceToSet(override.IgnoreFields),
			FixedFields:  override.FixedFields,
		}
		if options.Usage == "" {
			options.Usage = humanizeMethod(string(ref.Method.Name()))
		}
		report, err := addGeneratedCommand(groupMap, override.Group, ref, options, messages, enums)
		if err != nil {
			return nil, generationReport{}, err
		}
		reports = append(reports, report)
		if isDeferredStatus(report.Status) {
			deferredMethods[methodKey] = true
			continue
		}
		generatedMethods[methodKey] = true
	}

	for _, methodKey := range methodKeys {
		if generatedMethods[methodKey] || deferredMethods[methodKey] {
			continue
		}
		reports = append(reports, methodReport{
			Method: "/" + methodKey,
			Status: "deferred_unselected",
			Reason: "method was not generated; mark the service skip:true in server/tools/generate-fleet-cli/overrides.json to suppress it",
		})
	}

	var groups []groupSpec
	for _, group := range groupMap {
		sort.Strings(group.CommandExprs)
		groups = append(groups, *group)
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Name < groups[j].Name
	})

	sort.Slice(reports, func(i, j int) bool {
		if reports[i].Method == reports[j].Method {
			if reports[i].Status == reports[j].Status {
				if reports[i].Group == reports[j].Group {
					return reports[i].Command < reports[j].Command
				}
				return reports[i].Group < reports[j].Group
			}
			return reports[i].Status < reports[j].Status
		}
		return reports[i].Method < reports[j].Method
	})

	summary := make(map[string]int)
	for _, report := range reports {
		summary[report.Status]++
	}

	return groups, generationReport{Summary: summary, Methods: reports}, nil
}

func indexMethods(files []protoreflect.FileDescriptor, services map[string]serviceOverride) map[string]methodRef {
	result := make(map[string]methodRef)
	for _, file := range files {
		for i := range file.Services().Len() {
			service := file.Services().Get(i)
			serviceKey := string(file.Package()) + "." + string(service.Name())
			serviceOverride := services[serviceKey]
			for j := range service.Methods().Len() {
				method := service.Methods().Get(j)
				methodKey := serviceKey + "/" + string(method.Name())
				result[methodKey] = methodRef{
					ServiceKey:      serviceKey,
					ServiceName:     service.Name(),
					ServiceOverride: serviceOverride,
					Method:          method,
				}
			}
		}
	}
	return result
}

func addGeneratedCommand(
	groupMap map[string]*groupSpec,
	groupName string,
	ref methodRef,
	options renderOptions,
	messages map[protoreflect.FullName]messageInfo,
	enums map[protoreflect.FullName]enumInfo,
) (methodReport, error) {
	methodPath := "/" + ref.ServiceKey + "/" + string(ref.Method.Name())
	rendered, err := renderMethodExpr(ref, options, messages, enums)
	if err != nil {
		return methodReport{}, err
	}
	report := methodReport{
		Method:  methodPath,
		Status:  rendered.Status,
		Group:   groupName,
		Command: options.CommandName,
		Reason:  rendered.Reason,
	}
	if isDeferredStatus(rendered.Status) {
		return report, nil
	}

	group := ensureGroup(groupMap, groupName)
	for path, alias := range rendered.Imports {
		group.Imports[path] = alias
	}
	group.CommandExprs = append(group.CommandExprs, rendered.Expr)
	return report, nil
}

func ensureGroup(groups map[string]*groupSpec, name string) *groupSpec {
	group, ok := groups[name]
	if ok {
		return group
	}

	group = &groupSpec{
		Name:         name,
		FuncName:     "generated" + toGoFieldNameString(name) + "Command",
		Usage:        "Manage " + strings.ReplaceAll(name, "-", " ") + " commands",
		Imports:      map[string]string{"github.com/urfave/cli/v3": ""},
		CommandExprs: nil,
	}
	groups[name] = group
	return group
}

func renderMethodExpr(
	ref methodRef,
	options renderOptions,
	messages map[protoreflect.FullName]messageInfo,
	enums map[protoreflect.FullName]enumInfo,
) (renderResult, error) {
	request, ok := messages[ref.Method.Input().FullName()]
	if !ok {
		return renderResult{}, fmt.Errorf("missing request type for %s/%s", ref.ServiceKey, ref.Method.Name())
	}
	response, ok := messages[ref.Method.Output().FullName()]
	if !ok {
		return renderResult{}, fmt.Errorf("missing response type for %s/%s", ref.ServiceKey, ref.Method.Name())
	}

	if reason := unsupportedResponseReason(response.Descriptor); reason != "" {
		return renderResult{
			Status: "deferred_binary_response",
			Reason: reason,
		}, nil
	}

	analysis, err := analyzeRequest(request.Descriptor, request, enums, options)
	if err != nil {
		return renderResult{}, err
	}

	imports := map[string]string{
		request.GoImportPath:  request.GoAlias,
		response.GoImportPath: response.GoAlias,
	}
	for path, alias := range analysis.imports {
		imports[path] = alias
	}
	addRequestEnumImports(imports, request.Descriptor, enums, options)

	var expr string
	status := "generated"
	reason := ""
	if analysis.jsonOnly {
		imports["google.golang.org/protobuf/proto"] = "proto"
		expr = renderJSONOnlyExpr(options.CommandName, options.Usage, "/"+ref.ServiceKey+"/"+string(ref.Method.Name()), options.Auth, request, response)
		status = "generated_json_only"
		reason = analysis.Reason
	} else {
		imports["google.golang.org/protobuf/proto"] = "proto"
		imports["context"] = ""
		if analysis.needsFmt {
			imports["fmt"] = ""
		}
		expr = renderSimpleExpr(options.CommandName, options.Usage, "/"+ref.ServiceKey+"/"+string(ref.Method.Name()), options.Auth, request, response, analysis)
		if analysis.jsonFallback {
			status = "generated_json_fallback"
			reason = analysis.Reason
		}
	}

	return renderResult{
		Expr:    expr,
		Imports: imports,
		Status:  status,
		Reason:  reason,
	}, nil
}

func addRequestEnumImports(
	imports map[string]string,
	message protoreflect.MessageDescriptor,
	enums map[protoreflect.FullName]enumInfo,
	options renderOptions,
) {
	for i := range message.Fields().Len() {
		field := message.Fields().Get(i)
		fieldName := string(field.Name())
		if options.IgnoreFields[fieldName] {
			continue
		}
		if field.Kind() != protoreflect.EnumKind || field.IsList() || field.IsMap() {
			continue
		}
		enumMeta, ok := enums[field.Enum().FullName()]
		if !ok {
			continue
		}
		imports[enumMeta.GoImportPath] = enumMeta.GoAlias
	}
}

type requestAnalysis struct {
	jsonOnly            bool
	jsonFallback        bool
	needsFmt            bool
	flags               []string
	flagHelpers         []string
	lines               []string
	Reason              string
	minerSelectorField  string
	commonSelectorField string
	imports             map[string]string
}

func analyzeRequest(
	message protoreflect.MessageDescriptor,
	messageInfo messageInfo,
	enums map[protoreflect.FullName]enumInfo,
	options renderOptions,
) (requestAnalysis, error) {
	var analysis requestAnalysis
	if options.JSONOnly {
		analysis.jsonOnly = true
		analysis.Reason = "request is intentionally generated as JSON-only via overrides"
		return analysis, nil
	}
	hasUnsupported := false

	for i := range message.Oneofs().Len() {
		oneof := message.Oneofs().Get(i)
		if !oneof.IsSynthetic() {
			hasUnsupported = true
		}
	}

	for i := range message.Fields().Len() {
		field := message.Fields().Get(i)
		fieldName := string(field.Name())
		if options.IgnoreFields[fieldName] {
			continue
		}
		if _, ok := options.FixedFields[fieldName]; ok {
			continue
		}
		if isMinerSelectorField(field) {
			analysis.flagHelpers = appendUniqueString(analysis.flagHelpers, "generatedMinerSelectorFlags()")
			analysis.minerSelectorField = toGoFieldName(field.Name())
			continue
		}
		if isCommonSelectorField(field) {
			analysis.flagHelpers = appendUniqueString(analysis.flagHelpers, "generatedCommonSelectorFlags()")
			analysis.commonSelectorField = toGoFieldName(field.Name())
			continue
		}
		if isStringValueField(field) {
			flag, lines := buildStringValueFieldPlan(field)
			analysis.flags = append(analysis.flags, flag)
			analysis.lines = append(analysis.lines, lines...)
			analysis.imports = addImport(analysis.imports, "google.golang.org/protobuf/types/known/wrapperspb", "wrapperspb")
			analysis.jsonFallback = true
			if analysis.Reason == "" {
				analysis.Reason = "request includes wrapper fields, so the generated command exposes simple flags plus --json fallback"
			}
			continue
		}
		if isPoolConfigField(field) {
			flags, lines := buildPoolConfigFieldPlan(field, messageInfo)
			analysis.flags = append(analysis.flags, flags...)
			analysis.lines = append(analysis.lines, lines...)
			analysis.imports = addImport(analysis.imports, "google.golang.org/protobuf/types/known/wrapperspb", "wrapperspb")
			analysis.jsonFallback = true
			analysis.Reason = "request includes pool config, so the generated command exposes pool flags plus --json fallback"
			continue
		}
		if field.IsMap() {
			hasUnsupported = true
			continue
		}
		if field.Kind() == protoreflect.MessageKind || field.Kind() == protoreflect.GroupKind || field.Kind() == protoreflect.BytesKind {
			hasUnsupported = true
			continue
		}

		flag, lines, needsFmt, err := buildFieldPlan(field, messageInfo, enums)
		if err != nil {
			return analysis, err
		}
		if flag == "" && len(lines) == 0 {
			continue
		}
		analysis.flags = append(analysis.flags, flag)
		analysis.lines = append(analysis.lines, lines...)
		analysis.needsFmt = analysis.needsFmt || needsFmt
	}

	if len(analysis.flags) == 0 && hasUnsupported {
		analysis.jsonOnly = true
		analysis.Reason = "request uses only complex fields, so the generated command accepts --json input only"
		return analysis, nil
	}
	analysis.jsonFallback = analysis.jsonFallback || hasUnsupported
	if hasUnsupported {
		analysis.Reason = "request includes complex fields, so the generated command exposes simple flags plus --json fallback"
	}
	for i := range message.Fields().Len() {
		field := message.Fields().Get(i)
		value, ok := options.FixedFields[string(field.Name())]
		if !ok {
			continue
		}
		lines, needsFmt, err := renderFixedFieldAssignment(field, messageInfo, enums, value)
		if err != nil {
			return analysis, err
		}
		analysis.lines = append(lines, analysis.lines...)
		analysis.needsFmt = analysis.needsFmt || needsFmt
	}

	return analysis, nil
}

func addImport(imports map[string]string, path, alias string) map[string]string {
	if imports == nil {
		imports = map[string]string{}
	}
	imports[path] = alias
	return imports
}

func renderFixedFieldAssignment(
	field protoreflect.FieldDescriptor,
	messageInfo messageInfo,
	enums map[protoreflect.FullName]enumInfo,
	value string,
) ([]string, bool, error) {
	_ = messageInfo
	switch field.Kind() {
	case protoreflect.StringKind:
		return scalarAssignmentLines(field, fmt.Sprintf("%q", value)), false, nil
	case protoreflect.BoolKind:
		boolValue := strings.EqualFold(value, "true")
		return scalarAssignmentLines(field, fmt.Sprintf("%t", boolValue)), false, nil
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return scalarAssignmentLines(field, fmt.Sprintf("int32(%s)", value)), false, nil
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return scalarAssignmentLines(field, value), false, nil
	case protoreflect.EnumKind:
		enumMeta, ok := enums[field.Enum().FullName()]
		if !ok {
			return nil, false, fmt.Errorf("missing enum metadata for %s", field.Enum().FullName())
		}
		enumValues := renderableEnumValues(field.Enum())
		for _, enumValue := range enumValues {
			if normalizeInput(enumValue.Input) == normalizeInput(value) {
				return scalarAssignmentLines(field, fmt.Sprintf("%s.%s_%s", enumMeta.GoAlias, enumMeta.GoIdent, enumValue.ConstName)), false, nil
			}
		}
		return nil, false, fmt.Errorf("invalid fixed enum value %q for field %s", value, field.FullName())
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind, protoreflect.Uint64Kind,
		protoreflect.Fixed64Kind, protoreflect.FloatKind, protoreflect.DoubleKind,
		protoreflect.BytesKind, protoreflect.MessageKind, protoreflect.GroupKind:
		// No fixed-field override needs these kinds yet.
	}
	return nil, false, fmt.Errorf("unsupported fixed field type for %s", field.FullName())
}

func buildFieldPlan(
	field protoreflect.FieldDescriptor,
	messageInfo messageInfo,
	enums map[protoreflect.FullName]enumInfo,
) (string, []string, bool, error) {
	flagName := strings.ReplaceAll(string(field.Name()), "_", "-")
	goFieldName := toGoFieldName(field.Name())
	var usage string
	var flag string
	var lines []string
	needsFmt := false

	if field.IsList() {
		switch field.Kind() {
		case protoreflect.StringKind:
			usage = fieldUsage(field)
			flag = fmt.Sprintf("&cli.StringSliceFlag{Name: %q, Usage: %q}", flagName, usage)
			lines = append(lines,
				fmt.Sprintf("if cmd.IsSet(%q) {", flagName),
				fmt.Sprintf("\treq.%s = cmd.StringSlice(%q)", goFieldName, flagName),
				"}",
			)
			return flag, lines, needsFmt, nil
		case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
			usage = fieldUsage(field)
			flag = fmt.Sprintf("&cli.StringSliceFlag{Name: %q, Usage: %q}", flagName, usage)
			lines = append(lines,
				fmt.Sprintf("if cmd.IsSet(%q) {", flagName),
				fmt.Sprintf("\tvalues, err := parseInt64Slice(cmd.StringSlice(%q))", flagName),
				"\tif err != nil {",
				"\t\treturn nil, err",
				"\t}",
				fmt.Sprintf("\treq.%s = values", goFieldName),
				"}",
			)
			return flag, lines, needsFmt, nil
		case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
			usage = fieldUsage(field)
			flag = fmt.Sprintf("&cli.StringSliceFlag{Name: %q, Usage: %q}", flagName, usage)
			lines = append(lines,
				fmt.Sprintf("if cmd.IsSet(%q) {", flagName),
				fmt.Sprintf("\tvalues, err := parseInt64Slice(cmd.StringSlice(%q))", flagName),
				"\tif err != nil {",
				"\t\treturn nil, err",
				"\t}",
				fmt.Sprintf("\treq.%s = make([]int32, len(values))", goFieldName),
				"\tfor i, value := range values {",
				fmt.Sprintf("\t\treq.%s[i] = int32(value)", goFieldName),
				"\t}",
				"}",
			)
			return flag, lines, needsFmt, nil
		case protoreflect.BoolKind, protoreflect.EnumKind, protoreflect.Uint32Kind,
			protoreflect.Fixed32Kind, protoreflect.Uint64Kind, protoreflect.Fixed64Kind,
			protoreflect.FloatKind, protoreflect.DoubleKind, protoreflect.BytesKind,
			protoreflect.MessageKind, protoreflect.GroupKind:
			// Repeated fields of these kinds have no flag mapping; the command
			// falls back to --json input for them.
		}
		return "", nil, false, nil
	}

	switch field.Kind() {
	case protoreflect.StringKind:
		usage = fieldUsage(field)
		flag = fmt.Sprintf("&cli.StringFlag{Name: %q, Usage: %q}", flagName, usage)
		lines = append(lines, assignmentBlock(field, messageInfo, flagName, goFieldName, fmt.Sprintf("cmd.String(%q)", flagName))...)
	case protoreflect.BoolKind:
		usage = fieldUsage(field)
		flag = fmt.Sprintf("&cli.BoolFlag{Name: %q, Usage: %q}", flagName, usage)
		lines = append(lines, assignmentBlock(field, messageInfo, flagName, goFieldName, fmt.Sprintf("cmd.Bool(%q)", flagName))...)
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		usage = fieldUsage(field)
		flag = fmt.Sprintf("&cli.IntFlag{Name: %q, Usage: %q}", flagName, usage)
		lines = append(lines, assignmentBlock(field, messageInfo, flagName, goFieldName, fmt.Sprintf("int32(cmd.Int(%q))", flagName))...)
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		usage = fieldUsage(field)
		flag = fmt.Sprintf("&cli.Int64Flag{Name: %q, Usage: %q}", flagName, usage)
		lines = append(lines, assignmentBlock(field, messageInfo, flagName, goFieldName, fmt.Sprintf("cmd.Int64(%q)", flagName))...)
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		usage = fieldUsage(field)
		flag = fmt.Sprintf("&cli.UintFlag{Name: %q, Usage: %q}", flagName, usage)
		lines = append(lines, assignmentBlock(field, messageInfo, flagName, goFieldName, fmt.Sprintf("uint32(cmd.Uint(%q))", flagName))...)
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		usage = fieldUsage(field)
		flag = fmt.Sprintf("&cli.Uint64Flag{Name: %q, Usage: %q}", flagName, usage)
		lines = append(lines, assignmentBlock(field, messageInfo, flagName, goFieldName, fmt.Sprintf("cmd.Uint64(%q)", flagName))...)
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		usage = fieldUsage(field)
		flag = fmt.Sprintf("&cli.Float64Flag{Name: %q, Usage: %q}", flagName, usage)
		expr := fmt.Sprintf("cmd.Float64(%q)", flagName)
		if field.Kind() == protoreflect.FloatKind {
			expr = fmt.Sprintf("float32(%s)", expr)
		}
		lines = append(lines, assignmentBlock(field, messageInfo, flagName, goFieldName, expr)...)
	case protoreflect.EnumKind:
		enumMeta, ok := enums[field.Enum().FullName()]
		if !ok {
			return "", nil, false, fmt.Errorf("missing enum metadata for %s", field.Enum().FullName())
		}
		enumValues := renderableEnumValues(field.Enum())
		usage = fieldUsageWithOptions(field, enumValues.names())
		flag = fmt.Sprintf("&cli.StringFlag{Name: %q, Usage: %q}", flagName, usage)
		lines = append(lines, fmt.Sprintf("if cmd.IsSet(%q) {", flagName))
		lines = append(lines, fmt.Sprintf("\tswitch normalizeEnum(cmd.String(%q)) {", flagName))
		for _, enumValue := range enumValues {
			// Case labels must be in normalizeEnum's canonical (underscored)
			// form so the hyphenated spellings advertised in help text match.
			lines = append(lines, fmt.Sprintf("\tcase %q:", normalizeInput(enumValue.Input)))
			enumExpr := fmt.Sprintf("%s.%s_%s", enumMeta.GoAlias, enumMeta.GoIdent, enumValue.ConstName)
			if oneof := field.ContainingOneof(); oneof != nil && !oneof.IsSynthetic() {
				oneofGoFieldName := toGoFieldName(oneof.Name())
				wrapperType := messageInfo.GoAlias + "." + messageInfo.GoIdent + "_" + goFieldName
				lines = append(lines, fmt.Sprintf("\t\treq.%s = &%s{%s: %s}", oneofGoFieldName, wrapperType, goFieldName, enumExpr))
			} else {
				for _, line := range scalarAssignmentLines(field, enumExpr) {
					lines = append(lines, "\t\t"+line)
				}
			}
		}
		lines = append(lines, "\tdefault:")
		lines = append(lines, fmt.Sprintf("\t\treturn nil, fmt.Errorf(%q, cmd.String(%q))", enumErrorFormat(flagName, enumValues.names()), flagName))
		lines = append(lines, "\t}")
		lines = append(lines, "}")
		needsFmt = true
	case protoreflect.BytesKind, protoreflect.MessageKind, protoreflect.GroupKind:
		// Filtered out by analyzeRequest before flag planning; returning empty
		// values defers these fields to --json input.
	}

	return flag, lines, needsFmt, nil
}

func unsupportedResponseReason(message protoreflect.MessageDescriptor) string {
	for i := range message.Fields().Len() {
		field := message.Fields().Get(i)
		if field.Kind() == protoreflect.BytesKind {
			return "response contains bytes fields that require custom binary or file handling"
		}
	}
	return ""
}

func isMinerSelectorField(field protoreflect.FieldDescriptor) bool {
	return !field.IsList() && !field.IsMap() && field.Kind() == protoreflect.MessageKind && field.Message().FullName() == "minercommand.v1.DeviceSelector"
}

func isCommonSelectorField(field protoreflect.FieldDescriptor) bool {
	return !field.IsList() && !field.IsMap() && field.Kind() == protoreflect.MessageKind && field.Message().FullName() == "common.v1.DeviceSelector"
}

func isStringValueField(field protoreflect.FieldDescriptor) bool {
	return !field.IsList() && !field.IsMap() && field.Kind() == protoreflect.MessageKind && field.Message().FullName() == "google.protobuf.StringValue"
}

func isPoolConfigField(field protoreflect.FieldDescriptor) bool {
	return !field.IsList() && !field.IsMap() && field.Kind() == protoreflect.MessageKind && field.Message().FullName() == "pools.v1.PoolConfig"
}

func buildStringValueFieldPlan(field protoreflect.FieldDescriptor) (string, []string) {
	flagName := strings.ReplaceAll(string(field.Name()), "_", "-")
	usage := fieldUsage(field)
	goFieldName := toGoFieldName(field.Name())
	flag := fmt.Sprintf("&cli.StringFlag{Name: %q, Usage: %q}", flagName, usage)
	lines := []string{
		fmt.Sprintf("if cmd.IsSet(%q) {", flagName),
		fmt.Sprintf("\treq.%s = wrapperspb.String(cmd.String(%q))", goFieldName, flagName),
		"}",
	}
	return flag, lines
}

func buildPoolConfigFieldPlan(field protoreflect.FieldDescriptor, messageInfo messageInfo) ([]string, []string) {
	goFieldName := toGoFieldName(field.Name())
	configType := fmt.Sprintf("%s.%s", messageInfo.GoAlias, toGoIdent(field.Message().Name()))
	flags := []string{
		`&cli.StringFlag{Name: "pool-name", Usage: "pool name"}`,
		`&cli.StringFlag{Name: "url", Usage: "url"}`,
		`&cli.StringFlag{Name: "username", Usage: "username"}`,
		`&cli.StringFlag{Name: "password", Usage: "password"}`,
	}
	lines := []string{
		`if cmd.IsSet("pool-name") || cmd.IsSet("url") || cmd.IsSet("username") || cmd.IsSet("password") {`,
		fmt.Sprintf("\tif req.%s == nil {", goFieldName),
		fmt.Sprintf("\t\treq.%s = &%s{}", goFieldName, configType),
		`	}`,
		`	if cmd.IsSet("pool-name") {`,
		fmt.Sprintf("\t\treq.%s.PoolName = cmd.String(\"pool-name\")", goFieldName),
		`	}`,
		`	if cmd.IsSet("url") {`,
		fmt.Sprintf("\t\treq.%s.Url = cmd.String(\"url\")", goFieldName),
		`	}`,
		`	if cmd.IsSet("username") {`,
		fmt.Sprintf("\t\treq.%s.Username = cmd.String(\"username\")", goFieldName),
		`	}`,
		`	if cmd.IsSet("password") {`,
		fmt.Sprintf("\t\treq.%s.Password = wrapperspb.String(cmd.String(\"password\"))", goFieldName),
		`	}`,
		`}`,
	}
	return flags, lines
}

func conditionalAssignmentBlock(field protoreflect.FieldDescriptor, flagName, expr string) []string {
	lines := []string{fmt.Sprintf("if cmd.IsSet(%q) {", flagName)}
	for _, line := range scalarAssignmentLines(field, expr) {
		lines = append(lines, "\t"+line)
	}
	lines = append(lines, "}")
	return lines
}

func assignmentBlock(field protoreflect.FieldDescriptor, msgInfo messageInfo, flagName, goFieldName, expr string) []string {
	if oneof := field.ContainingOneof(); oneof != nil && !oneof.IsSynthetic() {
		oneofGoFieldName := toGoFieldName(oneof.Name())
		wrapperType := msgInfo.GoAlias + "." + msgInfo.GoIdent + "_" + goFieldName
		return []string{
			fmt.Sprintf("if cmd.IsSet(%q) {", flagName),
			fmt.Sprintf("\treq.%s = &%s{%s: %s}", oneofGoFieldName, wrapperType, goFieldName, expr),
			"}",
		}
	}
	return conditionalAssignmentBlock(field, flagName, expr)
}

func scalarAssignmentLines(field protoreflect.FieldDescriptor, expr string) []string {
	goFieldName := toGoFieldName(field.Name())
	if fieldNeedsPointer(field) {
		return []string{
			fmt.Sprintf("value := %s", expr),
			fmt.Sprintf("req.%s = &value", goFieldName),
		}
	}
	return []string{fmt.Sprintf("req.%s = %s", goFieldName, expr)}
}

func fieldNeedsPointer(field protoreflect.FieldDescriptor) bool {
	if field.IsList() || field.IsMap() {
		return false
	}
	if kind := field.Kind(); kind == protoreflect.MessageKind || kind == protoreflect.GroupKind || kind == protoreflect.BytesKind {
		return false
	}
	return field.HasPresence()
}

type enumValue struct {
	Input     string
	ConstName string
}

type enumValueList []enumValue

func (values enumValueList) names() []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.Input)
	}
	return result
}

func renderableEnumValues(enum protoreflect.EnumDescriptor) enumValueList {
	var values enumValueList
	prefix := strings.ToUpper(strings.Join(splitCamelWords(string(enum.Name())), "_")) + "_"
	for i := range enum.Values().Len() {
		value := enum.Values().Get(i)
		if value.Number() == 0 && strings.Contains(string(value.Name()), "UNSPECIFIED") {
			continue
		}
		name := string(value.Name())
		input := name
		if strings.HasPrefix(input, prefix) {
			input = strings.TrimPrefix(input, prefix)
		}
		input = strings.ToLower(strings.ReplaceAll(input, "_", "-"))
		values = append(values, enumValue{
			Input:     input,
			ConstName: name,
		})
	}
	return values
}

func enumErrorFormat(flagName string, values []string) string {
	return fmt.Sprintf("invalid value for %s: %%s. Valid options: %s", flagName, strings.Join(values, ", "))
}

func renderJSONOnlyExpr(
	commandName string,
	usage string,
	methodPath string,
	auth string,
	request messageInfo,
	response messageInfo,
) string {
	return strings.Join([]string{
		"generatedJSONRequestCommand(",
		fmt.Sprintf("\t%q,", commandName),
		fmt.Sprintf("\t%q,", usage),
		fmt.Sprintf("\t%q,", methodPath),
		fmt.Sprintf("\t%s,", auth),
		fmt.Sprintf("\tfunc() proto.Message { return &%s.%s{} },", request.GoAlias, request.GoIdent),
		fmt.Sprintf("\tfunc() proto.Message { return &%s.%s{} },", response.GoAlias, response.GoIdent),
		")",
	}, "\n")
}

func renderSimpleExpr(
	commandName string,
	usage string,
	methodPath string,
	auth string,
	request messageInfo,
	response messageInfo,
	analysis requestAnalysis,
) string {
	var buf bytes.Buffer
	buf.WriteString("generatedRequestCommand(\n")
	buf.WriteString(fmt.Sprintf("\t%q,\n", commandName))
	buf.WriteString(fmt.Sprintf("\t%q,\n", usage))
	buf.WriteString(fmt.Sprintf("\t%q,\n", methodPath))
	buf.WriteString(fmt.Sprintf("\t%s,\n", auth))
	if len(analysis.flagHelpers) > 0 {
		buf.WriteString("\tappend([]cli.Flag{\n")
	} else {
		buf.WriteString("\t[]cli.Flag{\n")
	}
	if analysis.jsonFallback {
		buf.WriteString("\t\t&cli.StringFlag{Name: \"json\", Usage: \"Path to a request JSON file, or - for stdin\"},\n")
	}
	for _, flag := range analysis.flags {
		buf.WriteString("\t\t" + flag + ",\n")
	}
	if len(analysis.flagHelpers) > 0 {
		buf.WriteString("\t}")
		for _, helper := range analysis.flagHelpers {
			buf.WriteString(", " + helper + "...")
		}
		buf.WriteString("),\n")
	} else {
		buf.WriteString("\t},\n")
	}
	buf.WriteString("\tfunc(ctx context.Context, cmd *cli.Command, client *Client) (proto.Message, error) {\n")
	buf.WriteString(fmt.Sprintf("\t\treq := &%s.%s{}\n", request.GoAlias, request.GoIdent))
	if analysis.jsonFallback {
		buf.WriteString("\t\tif jsonPath := cmd.String(\"json\"); jsonPath != \"\" {\n")
		buf.WriteString("\t\t\tif err := readProtoJSON(jsonPath, req); err != nil {\n")
		buf.WriteString("\t\t\t\treturn nil, err\n")
		buf.WriteString("\t\t\t}\n")
		buf.WriteString("\t\t}\n")
	}
	if analysis.minerSelectorField != "" {
		writeSelectorAssignment(&buf, analysis.minerSelectorField, "generatedBuildMinerSelector(ctx, cmd, client)", "generatedMinerSelectorProvided(cmd)", analysis.jsonFallback)
	}
	if analysis.commonSelectorField != "" {
		writeSelectorAssignment(&buf, analysis.commonSelectorField, "generatedBuildCommonSelector(cmd)", "generatedCommonSelectorProvided(cmd)", analysis.jsonFallback)
	}
	for _, line := range analysis.lines {
		buf.WriteString("\t\t" + line + "\n")
	}
	buf.WriteString("\t\treturn req, nil\n")
	buf.WriteString("\t},\n")
	buf.WriteString(fmt.Sprintf("\tfunc() proto.Message { return &%s.%s{} },\n", response.GoAlias, response.GoIdent))
	buf.WriteString(")")
	return strings.TrimSpace(buf.String())
}

// writeSelectorAssignment emits the code that builds a device selector and
// assigns it to req.<field>. builderCall is the selector-builder expression and
// providedCall is the runtime predicate reporting whether any selector flag was
// set. When the command also accepts a --json request body (jsonFallback), the
// assignment is guarded by providedCall so explicit selector flags override the
// JSON while an absent selector leaves the JSON value intact.
func writeSelectorAssignment(buf *bytes.Buffer, field, builderCall, providedCall string, jsonFallback bool) {
	if jsonFallback {
		buf.WriteString(fmt.Sprintf("\t\tif %s {\n", providedCall))
		buf.WriteString(fmt.Sprintf("\t\t\tselector, err := %s\n", builderCall))
		buf.WriteString("\t\t\tif err != nil {\n")
		buf.WriteString("\t\t\t\treturn nil, err\n")
		buf.WriteString("\t\t\t}\n")
		buf.WriteString(fmt.Sprintf("\t\t\treq.%s = selector\n", field))
		buf.WriteString("\t\t}\n")
		return
	}
	buf.WriteString(fmt.Sprintf("\t\tselector, err := %s\n", builderCall))
	buf.WriteString("\t\tif err != nil {\n")
	buf.WriteString("\t\t\treturn nil, err\n")
	buf.WriteString("\t\t}\n")
	buf.WriteString(fmt.Sprintf("\t\treq.%s = selector\n", field))
}

// removeStaleGeneratedFiles deletes previously generated cmd_*.go files whose
// service group no longer exists. Freshly rendered files are left untouched so
// unchanged output keeps its mtime; rewriting them on every run would
// needlessly restart fleet-api through the docker compose watch on server/.
func removeStaleGeneratedFiles(generated map[string]bool) error {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return fmt.Errorf("read output dir: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "cmd_manual.go" || generated[name] {
			continue
		}
		if strings.HasPrefix(name, "cmd_") && strings.HasSuffix(name, ".go") {
			if err := os.Remove(filepath.Join(outputDir, name)); err != nil {
				return fmt.Errorf("remove stale %s: %w", name, err)
			}
		}
	}
	return nil
}

// writeFileIfChanged writes content to path only when it differs from what is
// already on disk, keeping mtimes stable for unchanged generated files.
func writeFileIfChanged(path string, content []byte) error {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, content) {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read existing %s: %w", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func renderGroups(groups []groupSpec) (map[string]bool, error) {
	tmpl, err := template.New(filepath.Base(groupTemplatePath)).Funcs(template.FuncMap{
		"indent": indentBlock,
	}).ParseFiles(groupTemplatePath)
	if err != nil {
		return nil, fmt.Errorf("parse group template: %w", err)
	}

	generated := make(map[string]bool, len(groups))
	for _, group := range groups {
		data := groupTemplateData{
			FuncName:     group.FuncName,
			Name:         group.Name,
			Usage:        group.Usage,
			Imports:      sortedImports(group.Imports),
			CommandExprs: group.CommandExprs,
		}
		var output bytes.Buffer
		if err := tmpl.Execute(&output, data); err != nil {
			return nil, fmt.Errorf("render group %s: %w", group.Name, err)
		}
		name := "cmd_" + sanitizeFileName(group.Name) + ".go"
		if err := writeFileIfChanged(filepath.Join(outputDir, name), output.Bytes()); err != nil {
			return nil, err
		}
		generated[name] = true
	}
	return generated, nil
}

func renderCommandsFile(groups []groupSpec) error {
	tmpl, err := template.ParseFiles(commandsTemplatePath)
	if err != nil {
		return fmt.Errorf("parse commands template: %w", err)
	}

	groupFuncNames := make([]string, 0, len(groups))
	for _, group := range groups {
		groupFuncNames = append(groupFuncNames, group.FuncName)
	}

	var output bytes.Buffer
	if err := tmpl.Execute(&output, commandsTemplateData{GroupFuncNames: groupFuncNames}); err != nil {
		return fmt.Errorf("render commands file: %w", err)
	}
	if err := writeFileIfChanged(filepath.Join(outputDir, "cmd_commands.go"), output.Bytes()); err != nil {
		return fmt.Errorf("write commands file: %w", err)
	}
	return nil
}

func renderReport(report generationReport) error {
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o750); err != nil {
		return fmt.Errorf("create report dir: %w", err)
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(reportPath, data, 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

func sortedImports(imports map[string]string) []importSpec {
	var result []importSpec
	for path, alias := range imports {
		result = append(result, importSpec{Alias: alias, Path: path})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Path == result[j].Path {
			return result[i].Alias < result[j].Alias
		}
		return result[i].Path < result[j].Path
	})
	return result
}

func chooseGroupName(serviceName protoreflect.Name, serviceOverride serviceOverride, methodOverride methodOverride) string {
	if methodOverride.Group != "" {
		return methodOverride.Group
	}
	if serviceOverride.Group != "" {
		return serviceOverride.Group
	}
	name := strings.TrimSuffix(string(serviceName), "Service")
	return strings.ToLower(strings.Join(splitCamelWords(name), ""))
}

func chooseCommandName(serviceName protoreflect.Name, methodName protoreflect.Name, override methodOverride) string {
	if override.Command != "" {
		return override.Command
	}

	verb, noun := splitVerbAndNoun(string(methodName))
	if noun == "" {
		return toKebabCase(verb)
	}

	serviceBase := strings.TrimSuffix(string(serviceName), "Service")
	if sameEntity(noun, serviceBase) {
		return strings.ToLower(verb)
	}
	return strings.ToLower(verb) + "-" + toKebabCase(noun)
}

func chooseUsage(methodName protoreflect.Name, override methodOverride) string {
	if override.Usage != "" {
		return override.Usage
	}
	return humanizeMethod(string(methodName))
}

func chooseOverrideAuth(serviceOverride serviceOverride, auth string) string {
	if auth == "" {
		auth = serviceOverride.Auth
	}
	return authModeConst(auth)
}

func authModeConst(auth string) string {
	switch auth {
	case "", "bearer":
		return "generatedAuthBearer"
	case "anonymous":
		return "generatedAuthAnonymous"
	case "session":
		return "generatedAuthSession"
	default:
		return "generatedAuthBearer"
	}
}

func sliceToSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func sortedMethodKeys(methods map[string]methodRef) []string {
	keys := make([]string, 0, len(methods))
	for key := range methods {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func isDeferredStatus(status string) bool {
	return strings.HasPrefix(status, "deferred_")
}

func normalizeInput(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", "_"))
}

func goPackageInfo(file protoreflect.FileDescriptor) (string, string, error) {
	options, ok := file.Options().(*descriptorpb.FileOptions)
	if ok {
		goPackage := options.GetGoPackage()
		if goPackage != "" {
			if strings.Contains(goPackage, ";") {
				parts := strings.SplitN(goPackage, ";", 2)
				return parts[0], parts[1], nil
			}
			parts := strings.Split(goPackage, "/")
			return goPackage, parts[len(parts)-1], nil
		}
	}

	dir := filepath.ToSlash(filepath.Dir(file.Path()))
	importPath := modulePath + "/" + filepath.ToSlash(filepath.Join(generatedProtoRoot, dir))
	return importPath, packageNameForDir(dir), nil
}

func toGoIdent(name protoreflect.Name) string {
	return toGoFieldName(name)
}

func toGoFieldName(name protoreflect.Name) string {
	return toGoFieldNameString(string(name))
}

func toGoFieldNameString(name string) string {
	parts := strings.Split(name, "_")
	for i := range parts {
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}

func toKebabCase(value string) string {
	words := splitCamelWords(value)
	for i := range words {
		words[i] = strings.ToLower(words[i])
	}
	return strings.Join(words, "-")
}

func splitCamelWords(value string) []string {
	if value == "" {
		return nil
	}
	var words []string
	var current []rune
	for i, r := range value {
		if i > 0 && unicode.IsUpper(r) && (len(current) > 0 && !unicode.IsUpper(current[len(current)-1])) {
			words = append(words, string(current))
			current = current[:0]
		}
		current = append(current, r)
	}
	if len(current) > 0 {
		words = append(words, string(current))
	}
	return words
}

func splitVerbAndNoun(methodName string) (string, string) {
	verbs := []string{"List", "Get", "Create", "Update", "Delete", "Validate", "Pause", "Resume", "Reorder", "Set", "Clear"}
	for _, verb := range verbs {
		if strings.HasPrefix(methodName, verb) {
			return verb, strings.TrimPrefix(methodName, verb)
		}
	}
	return methodName, ""
}

func sameEntity(left, right string) bool {
	return singular(strings.ToLower(strings.Join(splitCamelWords(left), ""))) ==
		singular(strings.ToLower(strings.Join(splitCamelWords(right), "")))
}

func singular(value string) string {
	if strings.HasSuffix(value, "s") && len(value) > 1 {
		return strings.TrimSuffix(value, "s")
	}
	return value
}

func humanizeMethod(methodName string) string {
	words := splitCamelWords(methodName)
	for i := 1; i < len(words); i++ {
		words[i] = strings.ToLower(words[i])
	}
	return strings.Join(words, " ")
}

func fieldUsage(field protoreflect.FieldDescriptor) string {
	return strings.ReplaceAll(string(field.Name()), "_", " ")
}

func fieldUsageWithOptions(field protoreflect.FieldDescriptor, enumValues []string) string {
	if len(enumValues) == 0 {
		return fieldUsage(field)
	}
	return fmt.Sprintf("%s. Valid options: %s", fieldUsage(field), strings.Join(enumValues, ", "))
}

func sanitizeFileName(value string) string {
	value = strings.ReplaceAll(value, "-", "_")
	return value
}

func packageNameForDir(dir string) string {
	dir = filepath.ToSlash(dir)
	parts := strings.Split(dir, "/")
	for i := range parts {
		parts[i] = strings.ReplaceAll(parts[i], "-", "")
		parts[i] = strings.ReplaceAll(parts[i], "_", "")
	}
	return strings.Join(parts, "")
}

func indentBlock(level int, value string) string {
	prefix := strings.Repeat("\t", level)
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
