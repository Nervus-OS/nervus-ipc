from google.protobuf import descriptor_pb2 as _descriptor_pb2
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class RiskClass(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    RISK_CLASS_UNSPECIFIED: _ClassVar[RiskClass]
    RISK_CLASS_NORMAL: _ClassVar[RiskClass]
    RISK_CLASS_PRIVACY_SENSITIVE: _ClassVar[RiskClass]
    RISK_CLASS_PHYSICAL_CONTROL: _ClassVar[RiskClass]
    RISK_CLASS_CRITICAL_SAFETY: _ClassVar[RiskClass]
RISK_CLASS_UNSPECIFIED: RiskClass
RISK_CLASS_NORMAL: RiskClass
RISK_CLASS_PRIVACY_SENSITIVE: RiskClass
RISK_CLASS_PHYSICAL_CONTROL: RiskClass
RISK_CLASS_CRITICAL_SAFETY: RiskClass
METHOD_META_FIELD_NUMBER: _ClassVar[int]
method_meta: _descriptor.FieldDescriptor

class MethodMeta(_message.Message):
    __slots__ = ("method_id", "required_permission", "risk_class", "requires_control_lease", "returns_operation", "needs_user_confirmation", "request_type", "response_type", "is_read_only", "is_motion")
    METHOD_ID_FIELD_NUMBER: _ClassVar[int]
    REQUIRED_PERMISSION_FIELD_NUMBER: _ClassVar[int]
    RISK_CLASS_FIELD_NUMBER: _ClassVar[int]
    REQUIRES_CONTROL_LEASE_FIELD_NUMBER: _ClassVar[int]
    RETURNS_OPERATION_FIELD_NUMBER: _ClassVar[int]
    NEEDS_USER_CONFIRMATION_FIELD_NUMBER: _ClassVar[int]
    REQUEST_TYPE_FIELD_NUMBER: _ClassVar[int]
    RESPONSE_TYPE_FIELD_NUMBER: _ClassVar[int]
    IS_READ_ONLY_FIELD_NUMBER: _ClassVar[int]
    IS_MOTION_FIELD_NUMBER: _ClassVar[int]
    method_id: int
    required_permission: str
    risk_class: RiskClass
    requires_control_lease: bool
    returns_operation: bool
    needs_user_confirmation: bool
    request_type: str
    response_type: str
    is_read_only: bool
    is_motion: bool
    def __init__(self, method_id: _Optional[int] = ..., required_permission: _Optional[str] = ..., risk_class: _Optional[_Union[RiskClass, str]] = ..., requires_control_lease: bool = ..., returns_operation: bool = ..., needs_user_confirmation: bool = ..., request_type: _Optional[str] = ..., response_type: _Optional[str] = ..., is_read_only: bool = ..., is_motion: bool = ...) -> None: ...
