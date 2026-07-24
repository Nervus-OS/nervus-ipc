from nervus.ipc.v1 import method_registry_pb2 as _method_registry_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Iterable as _Iterable, Mapping as _Mapping, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ResourceAccessMode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    RESOURCE_ACCESS_MODE_UNSPECIFIED: _ClassVar[ResourceAccessMode]
    RESOURCE_ACCESS_MODE_EXCLUSIVE_CONTROL: _ClassVar[ResourceAccessMode]
    RESOURCE_ACCESS_MODE_SHARED_OBSERVE: _ClassVar[ResourceAccessMode]

class GrantMode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    GRANT_MODE_UNSPECIFIED: _ClassVar[GrantMode]
    GRANT_MODE_NORMAL: _ClassVar[GrantMode]
    GRANT_MODE_USER_CONSENT: _ClassVar[GrantMode]
    GRANT_MODE_SIGNATURE: _ClassVar[GrantMode]
    GRANT_MODE_PRIVILEGED: _ClassVar[GrantMode]
    GRANT_MODE_SYSTEM_ONLY: _ClassVar[GrantMode]
RESOURCE_ACCESS_MODE_UNSPECIFIED: ResourceAccessMode
RESOURCE_ACCESS_MODE_EXCLUSIVE_CONTROL: ResourceAccessMode
RESOURCE_ACCESS_MODE_SHARED_OBSERVE: ResourceAccessMode
GRANT_MODE_UNSPECIFIED: GrantMode
GRANT_MODE_NORMAL: GrantMode
GRANT_MODE_USER_CONSENT: GrantMode
GRANT_MODE_SIGNATURE: GrantMode
GRANT_MODE_PRIVILEGED: GrantMode
GRANT_MODE_SYSTEM_ONLY: GrantMode

class ProviderDescriptor(_message.Message):
    __slots__ = ("package_id", "interfaces", "resources", "permissions")
    PACKAGE_ID_FIELD_NUMBER: _ClassVar[int]
    INTERFACES_FIELD_NUMBER: _ClassVar[int]
    RESOURCES_FIELD_NUMBER: _ClassVar[int]
    PERMISSIONS_FIELD_NUMBER: _ClassVar[int]
    package_id: str
    interfaces: _containers.RepeatedCompositeFieldContainer[ProvidedInterface]
    resources: _containers.RepeatedCompositeFieldContainer[ManagedResource]
    permissions: _containers.RepeatedCompositeFieldContainer[DefinedPermission]
    def __init__(self, package_id: _Optional[str] = ..., interfaces: _Optional[_Iterable[_Union[ProvidedInterface, _Mapping]]] = ..., resources: _Optional[_Iterable[_Union[ManagedResource, _Mapping]]] = ..., permissions: _Optional[_Iterable[_Union[DefinedPermission, _Mapping]]] = ...) -> None: ...

class ProvidedInterface(_message.Message):
    __slots__ = ("interface_id", "versions", "schema_hash", "required_permission", "resource_risk_floor")
    INTERFACE_ID_FIELD_NUMBER: _ClassVar[int]
    VERSIONS_FIELD_NUMBER: _ClassVar[int]
    SCHEMA_HASH_FIELD_NUMBER: _ClassVar[int]
    REQUIRED_PERMISSION_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_RISK_FLOOR_FIELD_NUMBER: _ClassVar[int]
    interface_id: str
    versions: _containers.RepeatedScalarFieldContainer[int]
    schema_hash: bytes
    required_permission: str
    resource_risk_floor: _method_registry_pb2.RiskClass
    def __init__(self, interface_id: _Optional[str] = ..., versions: _Optional[_Iterable[int]] = ..., schema_hash: _Optional[bytes] = ..., required_permission: _Optional[str] = ..., resource_risk_floor: _Optional[_Union[_method_registry_pb2.RiskClass, str]] = ...) -> None: ...

class ManagedResource(_message.Message):
    __slots__ = ("stable_role", "resource_type", "access_mode", "risk_class")
    STABLE_ROLE_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_TYPE_FIELD_NUMBER: _ClassVar[int]
    ACCESS_MODE_FIELD_NUMBER: _ClassVar[int]
    RISK_CLASS_FIELD_NUMBER: _ClassVar[int]
    stable_role: str
    resource_type: str
    access_mode: ResourceAccessMode
    risk_class: _method_registry_pb2.RiskClass
    def __init__(self, stable_role: _Optional[str] = ..., resource_type: _Optional[str] = ..., access_mode: _Optional[_Union[ResourceAccessMode, str]] = ..., risk_class: _Optional[_Union[_method_registry_pb2.RiskClass, str]] = ...) -> None: ...

class DefinedPermission(_message.Message):
    __slots__ = ("id", "grant_mode", "risk_class", "display_name", "description")
    ID_FIELD_NUMBER: _ClassVar[int]
    GRANT_MODE_FIELD_NUMBER: _ClassVar[int]
    RISK_CLASS_FIELD_NUMBER: _ClassVar[int]
    DISPLAY_NAME_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    id: str
    grant_mode: GrantMode
    risk_class: _method_registry_pb2.RiskClass
    display_name: LocalizedText
    description: LocalizedText
    def __init__(self, id: _Optional[str] = ..., grant_mode: _Optional[_Union[GrantMode, str]] = ..., risk_class: _Optional[_Union[_method_registry_pb2.RiskClass, str]] = ..., display_name: _Optional[_Union[LocalizedText, _Mapping]] = ..., description: _Optional[_Union[LocalizedText, _Mapping]] = ...) -> None: ...

class LocalizedText(_message.Message):
    __slots__ = ("zh_cn", "en")
    ZH_CN_FIELD_NUMBER: _ClassVar[int]
    EN_FIELD_NUMBER: _ClassVar[int]
    zh_cn: str
    en: str
    def __init__(self, zh_cn: _Optional[str] = ..., en: _Optional[str] = ...) -> None: ...
