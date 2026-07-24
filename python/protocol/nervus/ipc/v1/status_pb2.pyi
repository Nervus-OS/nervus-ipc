from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class StatusCode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    STATUS_CODE_UNSPECIFIED: _ClassVar[StatusCode]
    STATUS_CODE_OK: _ClassVar[StatusCode]
    STATUS_CODE_ACCEPTED: _ClassVar[StatusCode]
    STATUS_CODE_INVALID_ARGUMENT: _ClassVar[StatusCode]
    STATUS_CODE_UNAUTHENTICATED: _ClassVar[StatusCode]
    STATUS_CODE_PERMISSION_DENIED: _ClassVar[StatusCode]
    STATUS_CODE_NOT_FOUND: _ClassVar[StatusCode]
    STATUS_CODE_FAILED_PRECONDITION: _ClassVar[StatusCode]
    STATUS_CODE_RESOURCE_EXHAUSTED: _ClassVar[StatusCode]
    STATUS_CODE_DEADLINE_EXCEEDED: _ClassVar[StatusCode]
    STATUS_CODE_CANCELLED: _ClassVar[StatusCode]
    STATUS_CODE_UNAVAILABLE: _ClassVar[StatusCode]
    STATUS_CODE_INTERNAL: _ClassVar[StatusCode]

class ResolveEndpointReason(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    RESOLVE_ENDPOINT_REASON_UNSPECIFIED: _ClassVar[ResolveEndpointReason]
    RESOLVE_ENDPOINT_REASON_INTERFACE_NOT_FOUND: _ClassVar[ResolveEndpointReason]
    RESOLVE_ENDPOINT_REASON_VERSION_MISMATCH: _ClassVar[ResolveEndpointReason]
    RESOLVE_ENDPOINT_REASON_RESOURCE_NOT_FOUND: _ClassVar[ResolveEndpointReason]
    RESOLVE_ENDPOINT_REASON_RESOURCE_AMBIGUOUS: _ClassVar[ResolveEndpointReason]
STATUS_CODE_UNSPECIFIED: StatusCode
STATUS_CODE_OK: StatusCode
STATUS_CODE_ACCEPTED: StatusCode
STATUS_CODE_INVALID_ARGUMENT: StatusCode
STATUS_CODE_UNAUTHENTICATED: StatusCode
STATUS_CODE_PERMISSION_DENIED: StatusCode
STATUS_CODE_NOT_FOUND: StatusCode
STATUS_CODE_FAILED_PRECONDITION: StatusCode
STATUS_CODE_RESOURCE_EXHAUSTED: StatusCode
STATUS_CODE_DEADLINE_EXCEEDED: StatusCode
STATUS_CODE_CANCELLED: StatusCode
STATUS_CODE_UNAVAILABLE: StatusCode
STATUS_CODE_INTERNAL: StatusCode
RESOLVE_ENDPOINT_REASON_UNSPECIFIED: ResolveEndpointReason
RESOLVE_ENDPOINT_REASON_INTERFACE_NOT_FOUND: ResolveEndpointReason
RESOLVE_ENDPOINT_REASON_VERSION_MISMATCH: ResolveEndpointReason
RESOLVE_ENDPOINT_REASON_RESOURCE_NOT_FOUND: ResolveEndpointReason
RESOLVE_ENDPOINT_REASON_RESOURCE_AMBIGUOUS: ResolveEndpointReason

class Success(_message.Message):
    __slots__ = ("code", "payload")
    CODE_FIELD_NUMBER: _ClassVar[int]
    PAYLOAD_FIELD_NUMBER: _ClassVar[int]
    code: StatusCode
    payload: bytes
    def __init__(self, code: _Optional[_Union[StatusCode, str]] = ..., payload: _Optional[bytes] = ...) -> None: ...

class Failure(_message.Message):
    __slots__ = ("code", "public_message", "error_detail")
    CODE_FIELD_NUMBER: _ClassVar[int]
    PUBLIC_MESSAGE_FIELD_NUMBER: _ClassVar[int]
    ERROR_DETAIL_FIELD_NUMBER: _ClassVar[int]
    code: StatusCode
    public_message: str
    error_detail: bytes
    def __init__(self, code: _Optional[_Union[StatusCode, str]] = ..., public_message: _Optional[str] = ..., error_detail: _Optional[bytes] = ...) -> None: ...

class ResolveEndpointErrorDetail(_message.Message):
    __slots__ = ("reason",)
    REASON_FIELD_NUMBER: _ClassVar[int]
    reason: ResolveEndpointReason
    def __init__(self, reason: _Optional[_Union[ResolveEndpointReason, str]] = ...) -> None: ...
