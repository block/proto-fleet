package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	commandsPath         = "server/tools/generate-fleet-cli/commands.json"
	outputDir            = "server/cmd/fleetcli"
	groupTemplatePath    = "server/tools/generate-fleet-cli/templates/fleet-group.gotmpl"
	commandsTemplatePath = "server/tools/generate-fleet-cli/templates/fleet-commands.gotmpl"
)

type commandsManifest struct {
	Commands []commandSpec `json:"commands"`
}

type commandSpec struct {
	Method                string            `json:"method"`
	Group                 string            `json:"group"`
	Subgroup              string            `json:"subgroup,omitempty"`
	Command               string            `json:"command"`
	Usage                 string            `json:"usage,omitempty"`
	Auth                  string            `json:"auth,omitempty"`
	IgnoreFields          []string          `json:"ignore_fields,omitempty"`
	RequiredFields        []string          `json:"required_fields,omitempty"`
	FixedFields           map[string]string `json:"fixed_fields,omitempty"`
	DefaultFields         map[string]string `json:"default_fields,omitempty"`
	RequireCollectionType string            `json:"require_collection_type,omitempty"`
	CommonSelector        string            `json:"common_selector,omitempty"`
	FieldFlags            []fieldFlagSpec   `json:"field_flags,omitempty"`
	JSONOnly              bool              `json:"json_only,omitempty"`
	JSONOptional          bool              `json:"json_optional,omitempty"`
}

type fieldFlagSpec struct {
	Path     string `json:"path"`
	Flag     string `json:"flag"`
	Usage    string `json:"usage,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Required bool   `json:"required,omitempty"`
	Prompt   bool   `json:"prompt,omitempty"`
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
	Subgroups    []subgroupTemplateData
}

type subgroupTemplateData struct {
	Name         string
	Usage        string
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
	Subgroups    map[string][]string
}

type methodRef struct {
	ServiceKey  string
	ServiceName protoreflect.Name
	Method      protoreflect.MethodDescriptor
}

type renderOptions struct {
	CommandName           string
	Usage                 string
	Auth                  string
	JSONOnly              bool
	JSONOptional          bool
	IgnoreFields          map[string]bool
	RequiredFields        map[string]bool
	FixedFields           map[string]string
	DefaultFields         map[string]string
	RequireCollectionType string
	CommonSelector        string
	FieldFlags            []fieldFlagSpec
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
	Method   string `json:"method"`
	Status   string `json:"status"`
	Group    string `json:"group,omitempty"`
	Subgroup string `json:"subgroup,omitempty"`
	Command  string `json:"command,omitempty"`
	Reason   string `json:"reason,omitempty"`
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
	manifest, err := loadCommandsManifest()
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

	groups, report, err := buildGroups(files, messages, enums, manifest)
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

func loadCommandsManifest() (commandsManifest, error) {
	var manifest commandsManifest
	data, err := os.ReadFile(commandsPath)
	if err != nil {
		return manifest, fmt.Errorf("read commands manifest: %w", err)
	}
	manifest, err = parseCommandsManifest(data)
	if err != nil {
		return manifest, err
	}
	return manifest, nil
}

func parseCommandsManifest(data []byte) (commandsManifest, error) {
	var manifest commandsManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, fmt.Errorf("parse commands manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return manifest, fmt.Errorf("parse commands manifest: trailing JSON data")
	} else if err != io.EOF {
		return manifest, fmt.Errorf("parse commands manifest: %w", err)
	}
	if manifest.Commands == nil {
		manifest.Commands = []commandSpec{}
	}
	if err := validateCommandsManifest(manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func validateCommandsManifest(manifest commandsManifest) error {
	seenCommands := map[string]bool{}
	groupChildren := map[string]map[string]string{}
	for _, command := range manifest.Commands {
		for label, value := range map[string]string{"group": command.Group, "subgroup": command.Subgroup, "command": command.Command} {
			if label == "subgroup" && value == "" {
				continue
			}
			if !validCommandComponent(value) {
				return fmt.Errorf("invalid %s %q; use lowercase letters, digits, and internal hyphens", label, value)
			}
		}
		parts := []string{command.Group}
		if command.Subgroup != "" {
			parts = append(parts, command.Subgroup)
		}
		parts = append(parts, command.Command)
		key := strings.Join(parts, " ")
		if seenCommands[key] {
			return fmt.Errorf("duplicate generated command %q", key)
		}
		seenCommands[key] = true
		if groupChildren[command.Group] == nil {
			groupChildren[command.Group] = map[string]string{}
		}
		childName := command.Command
		childKind := "command"
		if command.Subgroup != "" {
			childName = command.Subgroup
			childKind = "subgroup"
		}
		if existingKind, ok := groupChildren[command.Group][childName]; ok && existingKind != childKind {
			return fmt.Errorf("generated group %q uses %q as both a command and subgroup", command.Group, childName)
		}
		groupChildren[command.Group][childName] = childKind
		if _, err := authPolicyConst(command.Auth); err != nil {
			return fmt.Errorf("command %q: %w", key, err)
		}
		seenFieldFlags := map[string]bool{}
		for _, fieldFlag := range command.FieldFlags {
			if fieldFlag.Path == "" {
				return fmt.Errorf("command %q: field_flags path is required", key)
			}
			if fieldFlag.Flag == "" {
				return fmt.Errorf("command %q: field_flags flag is required for path %q", key, fieldFlag.Path)
			}
			if seenFieldFlags[fieldFlag.Flag] {
				return fmt.Errorf("command %q: duplicate field flag %q", key, fieldFlag.Flag)
			}
			seenFieldFlags[fieldFlag.Flag] = true
			switch fieldFlagKind(fieldFlag) {
			case "string", "secret":
			default:
				return fmt.Errorf("command %q: unsupported field flag kind %q for path %q", key, fieldFlag.Kind, fieldFlag.Path)
			}
		}
	}
	return nil
}

func validCommandComponent(value string) bool {
	if value == "" || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
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
	manifest commandsManifest,
) ([]groupSpec, generationReport, error) {
	groupMap := make(map[string]*groupSpec)
	methodIndex := indexMethods(files)
	methodKeys := sortedMethodKeys(methodIndex)
	var reports []methodReport
	generatedMethods := map[string]bool{}
	deferredMethods := map[string]bool{}

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
	}

	for _, command := range manifest.Commands {
		methodKey := strings.TrimPrefix(command.Method, "/")
		ref, ok := methodIndex[methodKey]
		if !ok {
			return nil, generationReport{}, fmt.Errorf("unknown command method %q", command.Method)
		}
		authPolicy, err := authPolicyConst(command.Auth)
		if err != nil {
			return nil, generationReport{}, fmt.Errorf("command %s %s: %w", command.Group, command.Command, err)
		}
		options := renderOptions{
			CommandName:           command.Command,
			Usage:                 command.Usage,
			Auth:                  authPolicy,
			JSONOnly:              command.JSONOnly,
			JSONOptional:          command.JSONOptional,
			IgnoreFields:          sliceToSet(command.IgnoreFields),
			RequiredFields:        sliceToSet(command.RequiredFields),
			FixedFields:           command.FixedFields,
			DefaultFields:         command.DefaultFields,
			RequireCollectionType: command.RequireCollectionType,
			CommonSelector:        command.CommonSelector,
			FieldFlags:            command.FieldFlags,
		}
		if options.Usage == "" {
			options.Usage = humanizeMethod(string(ref.Method.Name()))
		}
		report, err := addGeneratedCommand(groupMap, command.Group, command.Subgroup, ref, options, messages, enums)
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
			Reason: "method was not generated because it is not listed in server/tools/generate-fleet-cli/commands.json",
		})
	}

	var groups []groupSpec
	for _, group := range groupMap {
		sort.Strings(group.CommandExprs)
		for subgroup := range group.Subgroups {
			sort.Strings(group.Subgroups[subgroup])
		}
		groups = append(groups, *group)
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Name < groups[j].Name
	})

	sort.Slice(reports, func(i, j int) bool {
		if reports[i].Method == reports[j].Method {
			if reports[i].Status == reports[j].Status {
				if reports[i].Group == reports[j].Group {
					if reports[i].Subgroup != reports[j].Subgroup {
						return reports[i].Subgroup < reports[j].Subgroup
					}
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

func indexMethods(files []protoreflect.FileDescriptor) map[string]methodRef {
	result := make(map[string]methodRef)
	for _, file := range files {
		for i := range file.Services().Len() {
			service := file.Services().Get(i)
			serviceKey := string(file.Package()) + "." + string(service.Name())
			for j := range service.Methods().Len() {
				method := service.Methods().Get(j)
				methodKey := serviceKey + "/" + string(method.Name())
				result[methodKey] = methodRef{
					ServiceKey:  serviceKey,
					ServiceName: service.Name(),
					Method:      method,
				}
			}
		}
	}
	return result
}

func addGeneratedCommand(
	groupMap map[string]*groupSpec,
	groupName string,
	subgroupName string,
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
		Method:   methodPath,
		Status:   rendered.Status,
		Group:    groupName,
		Subgroup: subgroupName,
		Command:  options.CommandName,
		Reason:   rendered.Reason,
	}
	if isDeferredStatus(rendered.Status) {
		return report, nil
	}

	group := ensureGroup(groupMap, groupName)
	for path, alias := range rendered.Imports {
		group.Imports[path] = alias
	}
	if subgroupName == "" {
		group.CommandExprs = append(group.CommandExprs, rendered.Expr)
	} else {
		group.Subgroups[subgroupName] = append(group.Subgroups[subgroupName], rendered.Expr)
	}
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
		Subgroups:    map[string][]string{},
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
	if options.JSONOptional && !analysis.jsonOnly {
		return renderResult{}, fmt.Errorf("json_optional requires a JSON-only request for %s/%s", ref.ServiceKey, ref.Method.Name())
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
		imports["context"] = ""
		imports["github.com/urfave/cli/v3"] = "cli"
		expr, err = renderJSONOnlyExpr(options.CommandName, options.Usage, "/"+ref.ServiceKey+"/"+string(ref.Method.Name()), options.Auth, request, response, analysis, options)
		if err != nil {
			return renderResult{}, err
		}
		status = "generated_json_only"
		reason = analysis.Reason
	} else {
		imports["google.golang.org/protobuf/proto"] = "proto"
		imports["context"] = ""
		if analysis.needsFmt {
			imports["fmt"] = ""
		}
		expr, err = renderSimpleExpr(options.CommandName, options.Usage, "/"+ref.ServiceKey+"/"+string(ref.Method.Name()), options.Auth, request, response, analysis, options)
		if err != nil {
			return renderResult{}, err
		}
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
	fleetSelectorField  string
	commonSelectorField string
	commonSelectorBuild string
	commonSelectorSet   string
	requiredFlagIndexes []int
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
		if err := validateRequiredFieldPaths(message, options.RequiredFields); err != nil {
			return analysis, err
		}
		analysis.jsonOnly = true
		analysis.Reason = "request is intentionally generated as JSON-only via commands.json"
		return analysis, nil
	}
	hasUnsupported := false
	seenRequiredFields := map[string]bool{}
	usedFieldFlagPaths := map[string]bool{}
	fieldFlagsByRoot := groupFieldFlagsByRoot(options.FieldFlags)

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
		if fieldFlags := fieldFlagsByRoot[fieldName]; len(fieldFlags) > 0 {
			plan, err := buildFieldFlagPlan(message, messageInfo, field, fieldFlags)
			if err != nil {
				return analysis, err
			}
			for index, fieldFlag := range fieldFlags {
				if !options.RequiredFields[fieldFlag.Path] {
					continue
				}
				plan.flags[index] = markFlagRequiredViaJSON(plan.flags[index])
				seenRequiredFields[fieldFlag.Path] = true
			}
			analysis.flags = append(analysis.flags, plan.flags...)
			analysis.lines = append(analysis.lines, plan.lines...)
			analysis.imports = mergeImports(analysis.imports, plan.imports)
			analysis.jsonFallback = analysis.jsonFallback || plan.jsonFallback
			if plan.jsonFallback && analysis.Reason == "" {
				analysis.Reason = "request includes manifest field flags for wrapper or nested fields, so the generated command exposes simple flags plus --json fallback"
			}
			for _, fieldFlag := range fieldFlags {
				usedFieldFlagPaths[fieldFlag.Path] = true
			}
			continue
		}
		if isMinerSelectorField(field) {
			analysis.flagHelpers = appendUniqueString(analysis.flagHelpers, "generatedMinerSelectorFlags()")
			analysis.minerSelectorField = toGoFieldName(field.Name())
			continue
		}
		if isFleetSelectorField(field) {
			analysis.flagHelpers = appendUniqueString(analysis.flagHelpers, "generatedMinerSelectorFlags()")
			analysis.fleetSelectorField = toGoFieldName(field.Name())
			continue
		}
		if isCommonSelectorField(field) {
			helper, builder, provided, err := commonSelectorMode(options.CommonSelector)
			if err != nil {
				return analysis, err
			}
			analysis.flagHelpers = appendUniqueString(analysis.flagHelpers, helper)
			analysis.commonSelectorField = toGoFieldName(field.Name())
			analysis.commonSelectorBuild = builder
			analysis.commonSelectorSet = provided
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
		if options.RequiredFields[fieldName] {
			analysis.requiredFlagIndexes = append(analysis.requiredFlagIndexes, len(analysis.flags))
			seenRequiredFields[fieldName] = true
		}
		analysis.flags = append(analysis.flags, flag)
		analysis.lines = append(analysis.lines, lines...)
		analysis.needsFmt = analysis.needsFmt || needsFmt
	}

	for fieldName := range options.RequiredFields {
		if !seenRequiredFields[fieldName] {
			return analysis, fmt.Errorf("required_fields references non-flag field %q on %s", fieldName, message.FullName())
		}
	}
	for _, fieldFlag := range options.FieldFlags {
		if !usedFieldFlagPaths[fieldFlag.Path] {
			return analysis, fmt.Errorf("field_flags references unknown root field %q on %s", fieldFlag.Path, message.FullName())
		}
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
	for _, index := range analysis.requiredFlagIndexes {
		if analysis.jsonFallback {
			analysis.flags[index] = markFlagRequiredViaJSON(analysis.flags[index])
		} else {
			analysis.flags[index] = markFlagRequired(analysis.flags[index])
		}
	}
	var defaultLines []string
	seenDefaultFields := map[string]bool{}
	for i := range message.Fields().Len() {
		field := message.Fields().Get(i)
		value, ok := options.DefaultFields[string(field.Name())]
		if !ok {
			continue
		}
		lines, needsFmt, err := renderDefaultFieldAssignment(field, messageInfo, enums, value)
		if err != nil {
			return analysis, err
		}
		defaultLines = append(defaultLines, lines...)
		seenDefaultFields[string(field.Name())] = true
		analysis.needsFmt = analysis.needsFmt || needsFmt
	}
	for fieldName := range options.DefaultFields {
		if !seenDefaultFields[fieldName] {
			return analysis, fmt.Errorf("default_fields references unknown field %q on %s", fieldName, message.FullName())
		}
	}
	analysis.lines = append(defaultLines, analysis.lines...)
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

func validateRequiredFieldPaths(message protoreflect.MessageDescriptor, requiredFields map[string]bool) error {
	for fieldPath := range requiredFields {
		current := message
		parts := strings.Split(fieldPath, ".")
		for index, part := range parts {
			field := current.Fields().ByName(protoreflect.Name(part))
			if field == nil {
				return fmt.Errorf("required_fields references unknown field path %q on %s", fieldPath, message.FullName())
			}
			if index == len(parts)-1 {
				break
			}
			if field.IsList() || field.IsMap() || (field.Kind() != protoreflect.MessageKind && field.Kind() != protoreflect.GroupKind) {
				return fmt.Errorf("required_fields references non-message field in path %q on %s", fieldPath, message.FullName())
			}
			current = field.Message()
		}
	}
	return nil
}

func markFlagRequired(flag string) string {
	if flag == "" || !strings.HasSuffix(flag, "}") {
		return flag
	}
	flag = strings.Replace(flag, `Usage: "`, `Usage: "(required) `, 1)
	return strings.TrimSuffix(flag, "}") + ", Required: true}"
}

func markFlagRequiredViaJSON(flag string) string {
	return strings.Replace(flag, `Usage: "`, `Usage: "(required unless provided by --json) `, 1)
}

func commonSelectorMode(value string) (helper, builder, provided string, err error) {
	switch normalizeInput(value) {
	case "", "default":
		return "generatedCommonSelectorFlags()", "generatedBuildCommonSelector(cmd)", "generatedCommonSelectorProvided(cmd)", nil
	case "device_list":
		return "generatedCommonDeviceListSelectorFlags()", "generatedBuildCommonDeviceListSelector(cmd)", "generatedCommonDeviceListSelectorProvided(cmd)", nil
	default:
		return "", "", "", fmt.Errorf("unsupported common_selector %q", value)
	}
}

func fieldFlagKind(spec fieldFlagSpec) string {
	if spec.Kind == "" {
		return "string"
	}
	return spec.Kind
}

func groupFieldFlagsByRoot(specs []fieldFlagSpec) map[string][]fieldFlagSpec {
	result := map[string][]fieldFlagSpec{}
	for _, spec := range specs {
		root, _, _ := strings.Cut(spec.Path, ".")
		result[root] = append(result[root], spec)
	}
	return result
}

func addImport(imports map[string]string, path, alias string) map[string]string {
	if imports == nil {
		imports = map[string]string{}
	}
	imports[path] = alias
	return imports
}

func mergeImports(dst, src map[string]string) map[string]string {
	for path, alias := range src {
		dst = addImport(dst, path, alias)
	}
	return dst
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

func renderDefaultFieldAssignment(
	field protoreflect.FieldDescriptor,
	messageInfo messageInfo,
	enums map[protoreflect.FullName]enumInfo,
	value string,
) ([]string, bool, error) {
	lines, needsFmt, err := renderFixedFieldAssignment(field, messageInfo, enums, value)
	if err != nil {
		return nil, false, fmt.Errorf("default field %s: %w", field.FullName(), err)
	}
	zeroCondition, err := zeroValueCondition(field)
	if err != nil {
		return nil, false, err
	}
	wrapped := []string{"if " + zeroCondition + " {"}
	for _, line := range lines {
		wrapped = append(wrapped, "\t"+line)
	}
	wrapped = append(wrapped, "}")
	return wrapped, needsFmt, nil
}

func zeroValueCondition(field protoreflect.FieldDescriptor) (string, error) {
	if field.IsList() || field.IsMap() {
		return "", fmt.Errorf("default field %s: repeated and map fields are not supported", field.FullName())
	}
	goFieldName := toGoFieldName(field.Name())
	if fieldNeedsPointer(field) {
		return fmt.Sprintf("req.%s == nil", goFieldName), nil
	}
	switch field.Kind() {
	case protoreflect.StringKind:
		return fmt.Sprintf("req.%s == \"\"", goFieldName), nil
	case protoreflect.BoolKind:
		return fmt.Sprintf("!req.%s", goFieldName), nil
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind, protoreflect.Uint64Kind,
		protoreflect.Fixed64Kind, protoreflect.FloatKind, protoreflect.DoubleKind,
		protoreflect.EnumKind:
		return fmt.Sprintf("req.%s == 0", goFieldName), nil
	case protoreflect.BytesKind, protoreflect.MessageKind, protoreflect.GroupKind:
		return "", fmt.Errorf("default field %s: unsupported field type", field.FullName())
	}
	return "", fmt.Errorf("default field %s: unsupported field type", field.FullName())
}

type fieldFlagPlan struct {
	flags        []string
	lines        []string
	imports      map[string]string
	jsonFallback bool
}

func buildFieldFlagPlan(
	message protoreflect.MessageDescriptor,
	messageInfo messageInfo,
	rootField protoreflect.FieldDescriptor,
	specs []fieldFlagSpec,
) (fieldFlagPlan, error) {
	var plan fieldFlagPlan
	for _, spec := range specs {
		target, err := resolveFieldFlagTarget(message, messageInfo, spec)
		if err != nil {
			return plan, err
		}
		flag, lines, imports, err := buildFieldFlagTargetPlan(target)
		if err != nil {
			return plan, err
		}
		plan.flags = append(plan.flags, flag)
		plan.lines = append(plan.lines, lines...)
		plan.imports = mergeImports(plan.imports, imports)
		if len(target.parentAssignments) > 0 || isStringValueField(target.field) {
			plan.jsonFallback = true
		}
	}

	if rootField.Kind() == protoreflect.MessageKind || rootField.Kind() == protoreflect.GroupKind {
		plan.jsonFallback = true
	}
	return plan, nil
}

type fieldFlagTarget struct {
	spec              fieldFlagSpec
	field             protoreflect.FieldDescriptor
	assignExpr        string
	parentAssignments []fieldFlagParentAssignment
}

type fieldFlagParentAssignment struct {
	expr    string
	goIdent string
}

func resolveFieldFlagTarget(
	message protoreflect.MessageDescriptor,
	messageInfo messageInfo,
	spec fieldFlagSpec,
) (fieldFlagTarget, error) {
	var target fieldFlagTarget
	target.spec = spec

	segments := strings.Split(spec.Path, ".")
	if len(segments) == 0 {
		return target, fmt.Errorf("field_flags path is required on %s", message.FullName())
	}
	if len(segments) > 2 {
		return target, fmt.Errorf("field_flags path %q on %s has too many segments; only one nested message level is supported", spec.Path, message.FullName())
	}

	currentMessage := message
	currentExpr := "req"
	for i, segment := range segments {
		field := currentMessage.Fields().ByName(protoreflect.Name(segment))
		if field == nil {
			return target, fmt.Errorf("field_flags references unknown field %q on %s", spec.Path, currentMessage.FullName())
		}
		if field.IsList() || field.IsMap() {
			return target, fmt.Errorf("field_flags references repeated or map field %q on %s", spec.Path, currentMessage.FullName())
		}
		goFieldName := toGoFieldName(field.Name())
		fieldExpr := currentExpr + "." + goFieldName
		if i == len(segments)-1 {
			target.field = field
			target.assignExpr = fieldExpr
			return target, nil
		}
		if field.Kind() != protoreflect.MessageKind && field.Kind() != protoreflect.GroupKind {
			return target, fmt.Errorf("field_flags path %q traverses non-message field %q on %s", spec.Path, segment, currentMessage.FullName())
		}
		target.parentAssignments = append(target.parentAssignments, fieldFlagParentAssignment{
			expr:    fieldExpr,
			goIdent: messageInfo.GoAlias + "." + toGoIdent(field.Message().Name()),
		})
		currentExpr = fieldExpr
		currentMessage = field.Message()
	}

	return target, fmt.Errorf("field_flags path %q did not resolve on %s", spec.Path, message.FullName())
}

func buildFieldFlagTargetPlan(target fieldFlagTarget) (string, []string, map[string]string, error) {
	usage := target.spec.Usage
	if usage == "" {
		usage = fieldUsage(target.field)
	}
	switch fieldFlagKind(target.spec) {
	case "string":
		return buildStringFieldFlagTargetPlan(target, usage)
	case "secret":
		return buildSecretFieldFlagTargetPlan(target, usage)
	default:
		return "", nil, nil, fmt.Errorf("unsupported field flag kind %q for %s", target.spec.Kind, target.spec.Path)
	}
}

func buildStringFieldFlagTargetPlan(target fieldFlagTarget, usage string) (string, []string, map[string]string, error) {
	if target.field.Kind() != protoreflect.StringKind {
		return "", nil, nil, fmt.Errorf("field_flags string kind references non-string scalar field %q", target.spec.Path)
	}
	flag := fmt.Sprintf("&cli.StringFlag{Name: %q, Usage: %q}", target.spec.Flag, usage)
	if target.spec.Required {
		flag = markFlagRequired(flag)
	}
	lines := []string{fmt.Sprintf("if cmd.IsSet(%q) {", target.spec.Flag)}
	for _, line := range fieldFlagParentInitLines(target.parentAssignments) {
		lines = append(lines, "\t"+line)
	}
	if fieldNeedsPointer(target.field) {
		lines = append(lines,
			fmt.Sprintf("\tvalue := cmd.String(%q)", target.spec.Flag),
			fmt.Sprintf("\t%s = &value", target.assignExpr),
		)
	} else {
		lines = append(lines, fmt.Sprintf("\t%s = cmd.String(%q)", target.assignExpr, target.spec.Flag))
	}
	lines = append(lines, "}")
	return flag, lines, nil, nil
}

func buildSecretFieldFlagTargetPlan(target fieldFlagTarget, usage string) (string, []string, map[string]string, error) {
	if target.field.Kind() != protoreflect.StringKind && !isStringValueField(target.field) {
		return "", nil, nil, fmt.Errorf("field_flags secret kind references non-string field %q", target.spec.Path)
	}
	flag := fmt.Sprintf("&cli.BoolFlag{Name: %q, Usage: %q}", target.spec.Flag, usage)
	label := strings.TrimSuffix(strings.ReplaceAll(target.spec.Flag, "-", " "), " stdin")
	secretVar := secretVarForFieldFlag(target.spec.Path)
	var lines []string
	if target.spec.Required || target.spec.Prompt {
		lines = append(lines, fmt.Sprintf("%s, err := generatedReadSecret(cmd, %q, %q)", secretVar, target.spec.Flag, label))
		lines = append(lines, "if err != nil {", "\treturn nil, err", "}")
		for _, line := range fieldFlagParentInitLines(target.parentAssignments) {
			lines = append(lines, line)
		}
		lines = append(lines, fieldFlagAssignmentLine(target, secretVar))
	} else {
		lines = append(lines, fmt.Sprintf("if cmd.IsSet(%q) {", target.spec.Flag))
		lines = append(lines, fmt.Sprintf("\t%s, err := generatedReadSecret(cmd, %q, %q)", secretVar, target.spec.Flag, label))
		lines = append(lines, "\tif err != nil {", "\t\treturn nil, err", "\t}")
		for _, line := range fieldFlagParentInitLines(target.parentAssignments) {
			lines = append(lines, "\t"+line)
		}
		lines = append(lines, "\t"+fieldFlagAssignmentLine(target, secretVar))
		lines = append(lines, "}")
	}
	imports := map[string]string{}
	if isStringValueField(target.field) {
		imports = addImport(imports, "google.golang.org/protobuf/types/known/wrapperspb", "wrapperspb")
	}
	return flag, lines, imports, nil
}

func secretVarForFieldFlag(path string) string {
	parts := strings.FieldsFunc(path, func(r rune) bool {
		return r == '.' || r == '_' || r == '-'
	})
	for i := range parts {
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return "secret" + strings.Join(parts, "")
}

func fieldFlagParentInitLines(assignments []fieldFlagParentAssignment) []string {
	var lines []string
	for _, assignment := range assignments {
		lines = append(lines,
			fmt.Sprintf("if %s == nil {", assignment.expr),
			fmt.Sprintf("\t%s = &%s{}", assignment.expr, assignment.goIdent),
			"}",
		)
	}
	return lines
}

func fieldFlagAssignmentLine(target fieldFlagTarget, secretVar string) string {
	if isStringValueField(target.field) {
		return fmt.Sprintf("%s = wrapperspb.String(%s)", target.assignExpr, secretVar)
	}
	if fieldNeedsPointer(target.field) {
		return fmt.Sprintf("%s = &%s", target.assignExpr, secretVar)
	}
	return fmt.Sprintf("%s = %s", target.assignExpr, secretVar)
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

func isFleetSelectorField(field protoreflect.FieldDescriptor) bool {
	return !field.IsList() && !field.IsMap() && field.Kind() == protoreflect.MessageKind && field.Message().FullName() == "fleetmanagement.v1.DeviceSelector"
}

func isCommonSelectorField(field protoreflect.FieldDescriptor) bool {
	return !field.IsList() && !field.IsMap() && field.Kind() == protoreflect.MessageKind && field.Message().FullName() == "common.v1.DeviceSelector"
}

func isStringValueField(field protoreflect.FieldDescriptor) bool {
	return !field.IsList() && !field.IsMap() && field.Kind() == protoreflect.MessageKind && field.Message().FullName() == "google.protobuf.StringValue"
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
	analysis requestAnalysis,
	options renderOptions,
) (string, error) {
	var buf strings.Builder
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
	if options.JSONOptional {
		buf.WriteString("\t\t&cli.StringFlag{Name: \"json\", Usage: \"Path to a request JSON file, or - for stdin\"},\n")
	} else {
		buf.WriteString("\t\t&cli.StringFlag{Name: \"json\", Usage: \"(required) Path to a request JSON file, or - for stdin\", Required: true},\n")
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
	if options.JSONOptional {
		buf.WriteString("\t\tif jsonPath := cmd.String(\"json\"); jsonPath != \"\" {\n")
		buf.WriteString("\t\t\tif err := readProtoJSON(jsonPath, req); err != nil {\n")
		buf.WriteString("\t\t\t\treturn nil, err\n")
		buf.WriteString("\t\t\t}\n")
		buf.WriteString("\t\t}\n")
	} else {
		buf.WriteString("\t\tif err := readProtoJSON(cmd.String(\"json\"), req); err != nil {\n")
		buf.WriteString("\t\t\treturn nil, err\n")
		buf.WriteString("\t\t}\n")
	}
	if analysis.minerSelectorField != "" {
		writeSelectorAssignmentToBuilder(&buf, analysis.minerSelectorField, "generatedBuildMinerSelector(ctx, cmd, client)", "generatedMinerSelectorProvided(cmd)")
	}
	if analysis.fleetSelectorField != "" {
		writeSelectorAssignmentToBuilder(&buf, analysis.fleetSelectorField, "generatedBuildFleetSelector(ctx, cmd, client)", "generatedMinerSelectorProvided(cmd)")
	}
	if options.RequireCollectionType != "" {
		lines, err := requireCollectionTypeLines(request.Descriptor, options.RequireCollectionType)
		if err != nil {
			return "", err
		}
		for _, line := range lines {
			buf.WriteString("\t\t" + line + "\n")
		}
	}
	writeRequiredFieldValidation(&buf, options.RequiredFields)
	buf.WriteString("\t\treturn req, nil\n")
	buf.WriteString("\t},\n")
	buf.WriteString(fmt.Sprintf("\tfunc() proto.Message { return &%s.%s{} },\n", response.GoAlias, response.GoIdent))
	buf.WriteString(")")
	return strings.TrimSpace(buf.String()), nil
}

func writeSelectorAssignmentToBuilder(buf *strings.Builder, fieldName, builderCall, providedCall string) {
	buf.WriteString(fmt.Sprintf("\t\tif %s {\n", providedCall))
	buf.WriteString(fmt.Sprintf("\t\t\tselector, err := %s\n", builderCall))
	buf.WriteString("\t\t\tif err != nil {\n")
	buf.WriteString("\t\t\t\treturn nil, err\n")
	buf.WriteString("\t\t\t}\n")
	buf.WriteString(fmt.Sprintf("\t\t\treq.%s = selector\n", fieldName))
	buf.WriteString("\t\t}\n")
}

func renderSimpleExpr(
	commandName string,
	usage string,
	methodPath string,
	auth string,
	request messageInfo,
	response messageInfo,
	analysis requestAnalysis,
	options renderOptions,
) (string, error) {
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
	if analysis.fleetSelectorField != "" {
		writeSelectorAssignment(&buf, analysis.fleetSelectorField, "generatedBuildFleetSelector(ctx, cmd, client)", "generatedMinerSelectorProvided(cmd)", analysis.jsonFallback)
	}
	if analysis.commonSelectorField != "" {
		writeSelectorAssignment(&buf, analysis.commonSelectorField, analysis.commonSelectorBuild, analysis.commonSelectorSet, analysis.jsonFallback)
	}
	for _, line := range analysis.lines {
		buf.WriteString("\t\t" + line + "\n")
	}
	if options.RequireCollectionType != "" {
		lines, err := requireCollectionTypeLines(request.Descriptor, options.RequireCollectionType)
		if err != nil {
			return "", err
		}
		for _, line := range lines {
			buf.WriteString("\t\t" + line + "\n")
		}
	}
	if analysis.jsonFallback {
		writeRequiredFieldValidation(&buf, options.RequiredFields)
	}
	buf.WriteString("\t\treturn req, nil\n")
	buf.WriteString("\t},\n")
	buf.WriteString(fmt.Sprintf("\tfunc() proto.Message { return &%s.%s{} },\n", response.GoAlias, response.GoIdent))
	buf.WriteString(")")
	return strings.TrimSpace(buf.String()), nil
}

type stringWriter interface {
	WriteString(string) (int, error)
}

func writeRequiredFieldValidation(buf stringWriter, requiredFields map[string]bool) {
	if len(requiredFields) == 0 {
		return
	}
	fieldNames := make([]string, 0, len(requiredFields))
	for fieldName := range requiredFields {
		fieldNames = append(fieldNames, fieldName)
	}
	sort.Strings(fieldNames)
	quotedNames := make([]string, 0, len(fieldNames))
	for _, fieldName := range fieldNames {
		quotedNames = append(quotedNames, fmt.Sprintf("%q", fieldName))
	}
	_, _ = buf.WriteString(fmt.Sprintf("\t\tif err := generatedValidateRequiredFields(req, %s); err != nil {\n", strings.Join(quotedNames, ", ")))
	_, _ = buf.WriteString("\t\t\treturn nil, err\n")
	_, _ = buf.WriteString("\t\t}\n")
}

func requireCollectionTypeLines(message protoreflect.MessageDescriptor, collectionType string) ([]string, error) {
	typeExpr, err := requiredCollectionTypeExpr(collectionType)
	if err != nil {
		return nil, err
	}

	if field := message.Fields().ByName("device_set_ids"); field != nil && field.IsList() {
		goFieldName := toGoFieldName(field.Name())
		return []string{
			fmt.Sprintf("if err := generatedRequireDeviceSetTypes(ctx, client, req.%s, %s); err != nil {", goFieldName, typeExpr),
			"\treturn nil, err",
			"}",
		}, nil
	}

	field := firstExistingField(message, []protoreflect.Name{"device_set_id", "target_group_id", "target_rack_id"})
	if field == nil {
		return nil, fmt.Errorf("require_collection_type needs device_set_id, device_set_ids, target_group_id, or target_rack_id field on %s", message.FullName())
	}
	goFieldName := toGoFieldName(field.Name())
	if fieldNeedsPointer(field) {
		return []string{
			fmt.Sprintf("if req.%s != nil {", goFieldName),
			fmt.Sprintf("\tif err := generatedRequireDeviceSetType(ctx, client, *req.%s, %s); err != nil {", goFieldName, typeExpr),
			"\t\treturn nil, err",
			"\t}",
			"}",
		}, nil
	}
	return []string{
		fmt.Sprintf("if err := generatedRequireDeviceSetType(ctx, client, req.%s, %s); err != nil {", goFieldName, typeExpr),
		"\treturn nil, err",
		"}",
	}, nil
}

func requiredCollectionTypeExpr(value string) (string, error) {
	switch normalizeInput(value) {
	case "group":
		return "devicesetv1.DeviceSetType_DEVICE_SET_TYPE_GROUP", nil
	case "rack":
		return "devicesetv1.DeviceSetType_DEVICE_SET_TYPE_RACK", nil
	default:
		return "", fmt.Errorf("unsupported require_collection_type %q", value)
	}
}

func firstExistingField(message protoreflect.MessageDescriptor, names []protoreflect.Name) protoreflect.FieldDescriptor {
	for _, name := range names {
		if field := message.Fields().ByName(name); field != nil {
			return field
		}
	}
	return nil
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
		subgroups := make([]subgroupTemplateData, 0, len(group.Subgroups))
		for name, commandExprs := range group.Subgroups {
			subgroups = append(subgroups, subgroupTemplateData{
				Name:         name,
				Usage:        "Manage " + strings.ReplaceAll(name, "-", " ") + " commands",
				CommandExprs: commandExprs,
			})
		}
		sort.Slice(subgroups, func(i, j int) bool { return subgroups[i].Name < subgroups[j].Name })
		data := groupTemplateData{
			FuncName:     group.FuncName,
			Name:         group.Name,
			Usage:        group.Usage,
			Imports:      sortedImports(group.Imports),
			CommandExprs: group.CommandExprs,
			Subgroups:    subgroups,
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

func authPolicyConst(auth string) (string, error) {
	switch auth {
	case "", "authenticated":
		return "generatedAuthAuthenticated", nil
	case "unauthenticated":
		return "generatedAuthUnauthenticated", nil
	case "session_only":
		return "generatedAuthSessionOnly", nil
	default:
		return "", fmt.Errorf("invalid auth policy %q; valid values are unauthenticated, authenticated, session_only", auth)
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
