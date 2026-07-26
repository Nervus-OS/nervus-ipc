package registry

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	ipcv1 "github.com/nervus-os/nervus-ipc/protocol/ipcv1"
)

// SchemaKey 唯一标识一个接口 major schema。
type SchemaKey struct {
	InterfaceID string
	Major       uint32
}

// Schema 是一份已经完成 hash、descriptor 与 MethodMeta 校验的接口 schema。
// 字段保持私有，避免目录消费者修改权威输入。
type Schema struct {
	bundle  *ipcv1.InterfaceSchemaBundle
	files   *protoregistry.Files
	methods map[uint32]*ipcv1.MethodMeta
}

func (s *Schema) InterfaceID() string { return s.bundle.GetInterfaceId() }
func (s *Schema) Major() uint32       { return s.bundle.GetVersion() }
func (s *Schema) Hash() []byte        { return append([]byte(nil), s.bundle.GetSchemaHash()...) }
func (s *Schema) Files() *protoregistry.Files {
	return s.files
}

func (s *Schema) Method(id uint32) (*ipcv1.MethodMeta, bool) {
	m, ok := s.methods[id]
	if !ok {
		return nil, false
	}
	return proto.Clone(m).(*ipcv1.MethodMeta), true
}

func (s *Schema) Methods() map[uint32]*ipcv1.MethodMeta {
	out := make(map[uint32]*ipcv1.MethodMeta, len(s.methods))
	for id, m := range s.methods {
		out[id] = proto.Clone(m).(*ipcv1.MethodMeta)
	}
	return out
}

// SchemaSet 是一份包内全部接口 schema 的不可变索引。
type SchemaSet struct {
	byKey map[SchemaKey]*Schema
}

func (s *SchemaSet) Lookup(interfaceID string, major uint32) (*Schema, bool) {
	if s == nil {
		return nil, false
	}
	v, ok := s.byKey[SchemaKey{InterfaceID: interfaceID, Major: major}]
	return v, ok
}

func (s *SchemaSet) Len() int {
	if s == nil {
		return 0
	}
	return len(s.byKey)
}

// BuildSchemaBundle 从生成代码的 method enum 构造自包含、确定性编码的
// FileDescriptorSet。打包器使用这个函数即可，不需要复制 descriptor 遍历逻辑。
func BuildSchemaBundle(interfaceID string, major uint32, methodEnum protoreflect.EnumDescriptor) (*ipcv1.InterfaceSchemaBundle, error) {
	if interfaceID == "" {
		return nil, errors.New("registry: empty interface id")
	}
	if major == 0 {
		return nil, errors.New("registry: interface major 0 is reserved")
	}
	if methodEnum == nil {
		return nil, errors.New("registry: nil method enum")
	}

	seen := make(map[string]protoreflect.FileDescriptor)
	var visit func(protoreflect.FileDescriptor)
	visit = func(fd protoreflect.FileDescriptor) {
		if fd == nil {
			return
		}
		name := fd.Path()
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = fd
		imports := fd.Imports()
		for i := 0; i < imports.Len(); i++ {
			visit(imports.Get(i).FileDescriptor)
		}
	}
	visit(methodEnum.ParentFile())

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)

	fds := &descriptorpb.FileDescriptorSet{}
	for _, name := range names {
		fds.File = append(fds.File, protodesc.ToFileDescriptorProto(seen[name]))
	}
	wire, err := proto.MarshalOptions{Deterministic: true}.Marshal(fds)
	if err != nil {
		return nil, fmt.Errorf("registry: marshal descriptor set: %w", err)
	}
	sum := sha256.Sum256(wire)

	return &ipcv1.InterfaceSchemaBundle{
		InterfaceId:       interfaceID,
		Version:           major,
		SchemaHash:        sum[:],
		FileDescriptorSet: wire,
		MethodEnumType:    string(methodEnum.FullName()),
	}, nil
}

// ParseSchemaBundle 校验并解析一份签名包携带的接口 bundle。
func ParseSchemaBundle(bundle *ipcv1.InterfaceSchemaBundle) (*Schema, error) {
	if bundle == nil {
		return nil, errors.New("registry: nil schema bundle")
	}
	if bundle.GetInterfaceId() == "" {
		return nil, errors.New("registry: schema bundle has empty interface id")
	}
	if bundle.GetVersion() == 0 {
		return nil, fmt.Errorf("registry: interface %q uses reserved major 0", bundle.GetInterfaceId())
	}
	if len(bundle.GetSchemaHash()) != sha256.Size {
		return nil, fmt.Errorf("registry: interface %q@%d schema hash has %d bytes, want %d",
			bundle.GetInterfaceId(), bundle.GetVersion(), len(bundle.GetSchemaHash()), sha256.Size)
	}
	if len(bundle.GetFileDescriptorSet()) == 0 {
		return nil, fmt.Errorf("registry: interface %q@%d has empty descriptor set",
			bundle.GetInterfaceId(), bundle.GetVersion())
	}
	sum := sha256.Sum256(bundle.GetFileDescriptorSet())
	if !bytes.Equal(sum[:], bundle.GetSchemaHash()) {
		return nil, fmt.Errorf("registry: interface %q@%d schema hash mismatch",
			bundle.GetInterfaceId(), bundle.GetVersion())
	}
	if bundle.GetMethodEnumType() == "" {
		return nil, fmt.Errorf("registry: interface %q@%d has empty method_enum_type",
			bundle.GetInterfaceId(), bundle.GetVersion())
	}

	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(bundle.GetFileDescriptorSet(), &fds); err != nil {
		return nil, fmt.Errorf("registry: parse interface %q@%d descriptor set: %w",
			bundle.GetInterfaceId(), bundle.GetVersion(), err)
	}
	files, err := protodesc.NewFiles(&fds)
	if err != nil {
		return nil, fmt.Errorf("registry: build interface %q@%d descriptors: %w",
			bundle.GetInterfaceId(), bundle.GetVersion(), err)
	}
	desc, err := files.FindDescriptorByName(protoreflect.FullName(bundle.GetMethodEnumType()))
	if err != nil {
		return nil, fmt.Errorf("registry: interface %q@%d method enum %q: %w",
			bundle.GetInterfaceId(), bundle.GetVersion(), bundle.GetMethodEnumType(), err)
	}
	enum, ok := desc.(protoreflect.EnumDescriptor)
	if !ok {
		return nil, fmt.Errorf("registry: interface %q@%d method_enum_type %q is not an enum",
			bundle.GetInterfaceId(), bundle.GetVersion(), bundle.GetMethodEnumType())
	}
	metas, err := ExtractMethodMetas(enum)
	if err != nil {
		return nil, err
	}
	if len(metas) == 0 {
		return nil, fmt.Errorf("registry: interface %q@%d has no method metadata",
			bundle.GetInterfaceId(), bundle.GetVersion())
	}

	methods := make(map[uint32]*ipcv1.MethodMeta, len(metas))
	for _, meta := range metas {
		if err := validateMethodMeta(files, meta); err != nil {
			return nil, fmt.Errorf("registry: interface %q@%d: %w",
				bundle.GetInterfaceId(), bundle.GetVersion(), err)
		}
		if _, dup := methods[meta.GetMethodId()]; dup {
			return nil, fmt.Errorf("registry: interface %q@%d duplicate method id %d",
				bundle.GetInterfaceId(), bundle.GetVersion(), meta.GetMethodId())
		}
		methods[meta.GetMethodId()] = meta
	}

	return &Schema{
		bundle:  proto.Clone(bundle).(*ipcv1.InterfaceSchemaBundle),
		files:   files,
		methods: methods,
	}, nil
}

func validateMethodMeta(files *protoregistry.Files, meta *ipcv1.MethodMeta) error {
	if meta.GetMethodId() == 0 {
		return errors.New("method id 0 is reserved")
	}
	if !knownRiskClass(meta.GetRiskClass()) {
		return fmt.Errorf("method %d has invalid risk class %d", meta.GetMethodId(), meta.GetRiskClass())
	}
	if max := meta.GetMaxTimeoutMs(); max != 0 &&
		meta.GetDefaultTimeoutMs() != 0 && meta.GetDefaultTimeoutMs() > max {
		return fmt.Errorf("method %d default timeout exceeds maximum", meta.GetMethodId())
	}
	for label, name := range map[string]string{
		"request":      meta.GetRequestType(),
		"response":     meta.GetResponseType(),
		"error detail": meta.GetErrorDetailType(),
	} {
		if name == "" {
			continue
		}
		d, err := files.FindDescriptorByName(protoreflect.FullName(name))
		if err != nil {
			return fmt.Errorf("method %d %s type %q not found: %w", meta.GetMethodId(), label, name, err)
		}
		if _, ok := d.(protoreflect.MessageDescriptor); !ok {
			return fmt.Errorf("method %d %s type %q is not a message", meta.GetMethodId(), label, name)
		}
	}
	if p := meta.GetTransfer(); p != nil {
		if !knownTransferDirection(p.GetDirection()) {
			return fmt.Errorf("method %d transfer has invalid direction %d",
				meta.GetMethodId(), p.GetDirection())
		}
		if p.GetMaxStreams() == 0 {
			return fmt.Errorf("method %d transfer has zero max_streams", meta.GetMethodId())
		}
		if p.GetMaxPacketBytes() == 0 {
			return fmt.Errorf("method %d transfer has zero max_packet_bytes", meta.GetMethodId())
		}
		if p.GetMaxBytesPerSecond() == 0 {
			return fmt.Errorf("method %d transfer has zero max_bytes_per_second", meta.GetMethodId())
		}
		seen := make(map[ipcv1.TransferMode]struct{}, len(p.GetAllowedModes()))
		for _, mode := range p.GetAllowedModes() {
			if !knownTransferMode(mode) {
				return fmt.Errorf("method %d transfer allows invalid mode %d", meta.GetMethodId(), mode)
			}
			if _, dup := seen[mode]; dup {
				return fmt.Errorf("method %d transfer repeats mode %s", meta.GetMethodId(), mode)
			}
			seen[mode] = struct{}{}
		}
	}
	return nil
}

func ParseSchemaBundleSet(set *ipcv1.InterfaceSchemaBundleSet) (*SchemaSet, error) {
	if set == nil {
		return nil, errors.New("registry: nil schema bundle set")
	}
	out := &SchemaSet{byKey: make(map[SchemaKey]*Schema, len(set.GetBundles()))}
	for _, bundle := range set.GetBundles() {
		schema, err := ParseSchemaBundle(bundle)
		if err != nil {
			return nil, err
		}
		key := SchemaKey{InterfaceID: schema.InterfaceID(), Major: schema.Major()}
		if _, dup := out.byKey[key]; dup {
			return nil, fmt.Errorf("registry: duplicate schema bundle for %q@%d", key.InterfaceID, key.Major)
		}
		out.byKey[key] = schema
	}
	return out, nil
}

// ProviderArtifacts 是一个包在签名/digest 验证后可交给 nervud Catalog Builder 的
// 结构化结果。签名与信任裁决仍属于 nervud，本包只做与 wire 数据自身有关的校验。
type ProviderArtifacts struct {
	Descriptor *ipcv1.ProviderDescriptor
	Schemas    *SchemaSet
}

func ParseProviderArtifacts(descriptorWire, schemaWire []byte) (*ProviderArtifacts, error) {
	var descriptor ipcv1.ProviderDescriptor
	if err := proto.Unmarshal(descriptorWire, &descriptor); err != nil {
		return nil, fmt.Errorf("registry: parse provider descriptor: %w", err)
	}
	var bundles ipcv1.InterfaceSchemaBundleSet
	if err := proto.Unmarshal(schemaWire, &bundles); err != nil {
		return nil, fmt.Errorf("registry: parse schema bundle set: %w", err)
	}
	schemas, err := ParseSchemaBundleSet(&bundles)
	if err != nil {
		return nil, err
	}
	if err := ValidateProviderArtifacts(&descriptor, schemas); err != nil {
		return nil, err
	}
	return &ProviderArtifacts{
		Descriptor: proto.Clone(&descriptor).(*ipcv1.ProviderDescriptor),
		Schemas:    schemas,
	}, nil
}

func ValidateProviderArtifacts(descriptor *ipcv1.ProviderDescriptor, schemas *SchemaSet) error {
	if descriptor == nil {
		return errors.New("registry: nil provider descriptor")
	}
	if descriptor.GetPackageId() == "" {
		return errors.New("registry: provider descriptor has empty package id")
	}
	if schemas == nil {
		return errors.New("registry: nil provider schema set")
	}
	if nsErrs := ValidateOEMNamespace(descriptor); len(nsErrs) != 0 {
		return errors.Join(nsErrs...)
	}

	customPermissions := make(map[string]struct{}, len(descriptor.GetPermissions()))
	for _, permission := range descriptor.GetPermissions() {
		if permission.GetId() == "" {
			return errors.New("registry: permission has empty id")
		}
		if _, dup := customPermissions[permission.GetId()]; dup {
			return fmt.Errorf("registry: duplicate permission %q", permission.GetId())
		}
		customPermissions[permission.GetId()] = struct{}{}
		if !knownGrantMode(permission.GetGrantMode()) {
			return fmt.Errorf("registry: permission %q has invalid grant mode %d",
				permission.GetId(), permission.GetGrantMode())
		}
		if !knownRiskClass(permission.GetRiskClass()) {
			return fmt.Errorf("registry: permission %q has invalid risk class %d",
				permission.GetId(), permission.GetRiskClass())
		}
		if !knownPermissionTrustFloor(permission.GetMinimumTrust()) {
			return fmt.Errorf("registry: permission %q has invalid minimum trust %d",
				permission.GetId(), permission.GetMinimumTrust())
		}
		if permission.GetDisplayName().GetZhCn() == "" || permission.GetDisplayName().GetEn() == "" ||
			permission.GetDescription().GetZhCn() == "" || permission.GetDescription().GetEn() == "" {
			return fmt.Errorf("registry: permission %q requires zh_cn and en UI text", permission.GetId())
		}
	}

	resourceRisks := make(map[string]ipcv1.RiskClass)
	resourceKeys := make(map[string]struct{}, len(descriptor.GetResources()))
	for _, resource := range descriptor.GetResources() {
		if resource.GetResourceType() == "" || resource.GetStableRole() == "" {
			return errors.New("registry: resource requires resource_type and stable_role")
		}
		key := resource.GetResourceType() + "\x00" + resource.GetStableRole()
		if _, dup := resourceKeys[key]; dup {
			return fmt.Errorf("registry: duplicate resource type=%q role=%q",
				resource.GetResourceType(), resource.GetStableRole())
		}
		resourceKeys[key] = struct{}{}
		if !knownResourceAccessMode(resource.GetAccessMode()) {
			return fmt.Errorf("registry: resource type=%q role=%q has invalid access mode %d",
				resource.GetResourceType(), resource.GetStableRole(), resource.GetAccessMode())
		}
		if !knownRiskClass(resource.GetRiskClass()) {
			return fmt.Errorf("registry: resource type=%q role=%q has invalid risk class %d",
				resource.GetResourceType(), resource.GetStableRole(), resource.GetRiskClass())
		}
		if current, ok := resourceRisks[resource.GetResourceType()]; !ok || resource.GetRiskClass() < current {
			resourceRisks[resource.GetResourceType()] = resource.GetRiskClass()
		}
	}

	declaredSchemas := make(map[SchemaKey]struct{})
	interfaceIDs := make(map[string]struct{}, len(descriptor.GetInterfaces()))
	for _, iface := range descriptor.GetInterfaces() {
		if iface.GetInterfaceId() == "" {
			return errors.New("registry: interface has empty id")
		}
		if _, dup := interfaceIDs[iface.GetInterfaceId()]; dup {
			return fmt.Errorf("registry: duplicate interface %q", iface.GetInterfaceId())
		}
		interfaceIDs[iface.GetInterfaceId()] = struct{}{}
		if iface.GetResourceRiskFloor() != ipcv1.RiskClass_RISK_CLASS_UNSPECIFIED &&
			!knownRiskClass(iface.GetResourceRiskFloor()) {
			return fmt.Errorf("registry: interface %q has invalid resource risk floor %d",
				iface.GetInterfaceId(), iface.GetResourceRiskFloor())
		}
		if iface.GetResourceRiskFloor() == ipcv1.RiskClass_RISK_CLASS_UNSPECIFIED &&
			len(iface.GetCompatibleResourceTypes()) != 0 {
			return fmt.Errorf("registry: resource interface %q has unspecified risk floor", iface.GetInterfaceId())
		}

		defaultType, defaultRole := iface.GetDefaultResourceType(), iface.GetDefaultResourceRole()
		if (defaultType == "") != (defaultRole == "") {
			return fmt.Errorf("registry: interface %q default resource type/role must be set together",
				iface.GetInterfaceId())
		}
		compatible := make(map[string]struct{}, len(iface.GetCompatibleResourceTypes()))
		for _, rt := range iface.GetCompatibleResourceTypes() {
			if rt == "" {
				return fmt.Errorf("registry: interface %q has empty compatible resource type", iface.GetInterfaceId())
			}
			if _, dup := compatible[rt]; dup {
				return fmt.Errorf("registry: interface %q repeats compatible resource type %q",
					iface.GetInterfaceId(), rt)
			}
			compatible[rt] = struct{}{}
			risk, exists := resourceRisks[rt]
			if !exists {
				return fmt.Errorf("registry: interface %q references unmanaged resource type %q",
					iface.GetInterfaceId(), rt)
			}
			if risk < iface.GetResourceRiskFloor() {
				return fmt.Errorf("registry: interface %q risk floor exceeds resource type %q risk",
					iface.GetInterfaceId(), rt)
			}
		}
		if defaultType != "" {
			if _, ok := compatible[defaultType]; !ok {
				return fmt.Errorf("registry: interface %q default resource type %q is not compatible",
					iface.GetInterfaceId(), defaultType)
			}
			if _, ok := resourceKeys[defaultType+"\x00"+defaultRole]; !ok {
				return fmt.Errorf("registry: interface %q default resource type=%q role=%q is not managed",
					iface.GetInterfaceId(), defaultType, defaultRole)
			}
		}

		versions, err := providedVersions(iface)
		if err != nil {
			return fmt.Errorf("registry: interface %q: %w", iface.GetInterfaceId(), err)
		}
		for major, hash := range versions {
			schema, ok := schemas.Lookup(iface.GetInterfaceId(), major)
			if !ok {
				return fmt.Errorf("registry: interface %q@%d has no schema bundle", iface.GetInterfaceId(), major)
			}
			if !bytes.Equal(hash, schema.Hash()) {
				return fmt.Errorf("registry: interface %q@%d descriptor/schema hash mismatch",
					iface.GetInterfaceId(), major)
			}
			declaredSchemas[SchemaKey{InterfaceID: iface.GetInterfaceId(), Major: major}] = struct{}{}
			for _, method := range schema.methods {
				if err := validatePermissionReference(
					method.GetRequiredPermission(),
					descriptor.GetPackageId(),
					customPermissions,
				); err != nil {
					return fmt.Errorf("registry: method %q@%d/%d: %w",
						iface.GetInterfaceId(), major, method.GetMethodId(), err)
				}
			}
		}
		if err := validatePermissionReference(
			iface.GetRequiredPermission(),
			descriptor.GetPackageId(),
			customPermissions,
		); err != nil {
			return fmt.Errorf("registry: interface %q: %w", iface.GetInterfaceId(), err)
		}
	}

	if len(declaredSchemas) != schemas.Len() {
		return fmt.Errorf("registry: schema set contains %d undeclared bundle(s)",
			schemas.Len()-len(declaredSchemas))
	}
	return nil
}

func providedVersions(iface *ipcv1.ProvidedInterface) (map[uint32][]byte, error) {
	out := make(map[uint32][]byte)
	if len(iface.GetInterfaceVersions()) != 0 {
		if len(iface.GetVersions()) != 0 || len(iface.GetSchemaHash()) != 0 {
			return nil, errors.New("legacy versions/schema_hash and interface_versions cannot be mixed")
		}
		for _, version := range iface.GetInterfaceVersions() {
			if version.GetMajor() == 0 {
				return nil, errors.New("major 0 is reserved")
			}
			if len(version.GetSchemaHash()) != sha256.Size {
				return nil, fmt.Errorf("major %d schema hash has %d bytes, want %d",
					version.GetMajor(), len(version.GetSchemaHash()), sha256.Size)
			}
			if _, dup := out[version.GetMajor()]; dup {
				return nil, fmt.Errorf("duplicate major %d", version.GetMajor())
			}
			out[version.GetMajor()] = append([]byte(nil), version.GetSchemaHash()...)
		}
	} else {
		if len(iface.GetVersions()) == 0 {
			return nil, errors.New("no interface versions")
		}
		if len(iface.GetSchemaHash()) != sha256.Size {
			return nil, fmt.Errorf("legacy schema hash has %d bytes, want %d",
				len(iface.GetSchemaHash()), sha256.Size)
		}
		for _, major := range iface.GetVersions() {
			if major == 0 {
				return nil, errors.New("major 0 is reserved")
			}
			if _, dup := out[major]; dup {
				return nil, fmt.Errorf("duplicate major %d", major)
			}
			out[major] = append([]byte(nil), iface.GetSchemaHash()...)
		}
	}
	return out, nil
}

func isPrivatePermission(permission, packageID string) bool {
	return permission != "" && packageID != "" && strings.HasPrefix(permission, packageID+".")
}

func validatePermissionReference(
	permission string,
	packageID string,
	customPermissions map[string]struct{},
) error {
	if permission == "" || strings.HasPrefix(permission, "perm.") {
		return nil
	}
	if !isPrivatePermission(permission, packageID) {
		return fmt.Errorf("permission %q is neither platform-owned nor under package namespace %q",
			permission, packageID)
	}
	if _, ok := customPermissions[permission]; !ok {
		return fmt.Errorf("undefined custom permission %q", permission)
	}
	return nil
}

func knownRiskClass(value ipcv1.RiskClass) bool {
	return value >= ipcv1.RiskClass_RISK_CLASS_NORMAL &&
		value <= ipcv1.RiskClass_RISK_CLASS_CRITICAL_SAFETY
}

func knownGrantMode(value ipcv1.GrantMode) bool {
	return value >= ipcv1.GrantMode_GRANT_MODE_NORMAL &&
		value <= ipcv1.GrantMode_GRANT_MODE_SYSTEM_ONLY
}

func knownPermissionTrustFloor(value ipcv1.PermissionTrustFloor) bool {
	return value >= ipcv1.PermissionTrustFloor_PERMISSION_TRUST_FLOOR_UNSPECIFIED &&
		value <= ipcv1.PermissionTrustFloor_PERMISSION_TRUST_FLOOR_PLATFORM
}

func knownResourceAccessMode(value ipcv1.ResourceAccessMode) bool {
	return value >= ipcv1.ResourceAccessMode_RESOURCE_ACCESS_MODE_EXCLUSIVE_CONTROL &&
		value <= ipcv1.ResourceAccessMode_RESOURCE_ACCESS_MODE_SHARED_OBSERVE
}

func knownTransferDirection(value ipcv1.TransferDirection) bool {
	return value >= ipcv1.TransferDirection_TRANSFER_DIRECTION_PROVIDER_TO_CALLER &&
		value <= ipcv1.TransferDirection_TRANSFER_DIRECTION_BIDIRECTIONAL
}

func knownTransferMode(value ipcv1.TransferMode) bool {
	return value >= ipcv1.TransferMode_TRANSFER_MODE_FRAMED_RELAY &&
		value <= ipcv1.TransferMode_TRANSFER_MODE_SHARED_MEMORY_RING
}
