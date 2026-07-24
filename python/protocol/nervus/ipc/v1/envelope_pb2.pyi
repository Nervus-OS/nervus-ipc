from nervus.ipc.v1 import status_pb2 as _status_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Iterable as _Iterable, Mapping as _Mapping, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class EndpointDiedReason(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ENDPOINT_DIED_REASON_UNSPECIFIED: _ClassVar[EndpointDiedReason]
    ENDPOINT_DIED_REASON_SERVICE_GONE: _ClassVar[EndpointDiedReason]
    ENDPOINT_DIED_REASON_SERVICE_SHUTTING_DOWN: _ClassVar[EndpointDiedReason]
    ENDPOINT_DIED_REASON_RESOURCE_FAULT: _ClassVar[EndpointDiedReason]
    ENDPOINT_DIED_REASON_SERVICE_RESTARTED: _ClassVar[EndpointDiedReason]

class EndpointRevokedReason(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ENDPOINT_REVOKED_REASON_UNSPECIFIED: _ClassVar[EndpointRevokedReason]
    ENDPOINT_REVOKED_REASON_PERMISSION_REVOKED: _ClassVar[EndpointRevokedReason]
    ENDPOINT_REVOKED_REASON_PACKAGE_DISABLED: _ClassVar[EndpointRevokedReason]
    ENDPOINT_REVOKED_REASON_POLICY_SUSPENDED: _ClassVar[EndpointRevokedReason]

class DeliveryClass(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    DELIVERY_CLASS_UNSPECIFIED: _ClassVar[DeliveryClass]
    DELIVERY_CLASS_RELIABLE: _ClassVar[DeliveryClass]
    DELIVERY_CLASS_STATE: _ClassVar[DeliveryClass]
    DELIVERY_CLASS_LOSSY: _ClassVar[DeliveryClass]

class SubscriptionClosedReason(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SUBSCRIPTION_CLOSED_REASON_UNSPECIFIED: _ClassVar[SubscriptionClosedReason]
    SUBSCRIPTION_CLOSED_REASON_ENDPOINT_DIED: _ClassVar[SubscriptionClosedReason]
    SUBSCRIPTION_CLOSED_REASON_ENDPOINT_REVOKED: _ClassVar[SubscriptionClosedReason]
    SUBSCRIPTION_CLOSED_REASON_BACKPRESSURE: _ClassVar[SubscriptionClosedReason]
    SUBSCRIPTION_CLOSED_REASON_SERVER_SHUTDOWN: _ClassVar[SubscriptionClosedReason]

class TrustProfile(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    TRUST_PROFILE_UNSPECIFIED: _ClassVar[TrustProfile]
    TRUST_PROFILE_ORDINARY: _ClassVar[TrustProfile]
    TRUST_PROFILE_OEM: _ClassVar[TrustProfile]
    TRUST_PROFILE_PLATFORM: _ClassVar[TrustProfile]

class CancelDispatchReason(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    CANCEL_DISPATCH_REASON_UNSPECIFIED: _ClassVar[CancelDispatchReason]
    CANCEL_DISPATCH_REASON_CLIENT_CANCELLED: _ClassVar[CancelDispatchReason]
    CANCEL_DISPATCH_REASON_DEADLINE_EXCEEDED: _ClassVar[CancelDispatchReason]
    CANCEL_DISPATCH_REASON_CLIENT_GONE: _ClassVar[CancelDispatchReason]

class ControllerClass(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    CONTROLLER_CLASS_UNSPECIFIED: _ClassVar[ControllerClass]
    CONTROLLER_CLASS_HUMAN: _ClassVar[ControllerClass]
    CONTROLLER_CLASS_AI: _ClassVar[ControllerClass]

class ControlLeaseErrorReason(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    CONTROL_LEASE_ERROR_REASON_UNSPECIFIED: _ClassVar[ControlLeaseErrorReason]
    CONTROL_LEASE_ERROR_REASON_HELD_BY_HUMAN: _ClassVar[ControlLeaseErrorReason]
    CONTROL_LEASE_ERROR_REASON_HELD_BY_AI: _ClassVar[ControlLeaseErrorReason]
    CONTROL_LEASE_ERROR_REASON_RESOURCE_UNAVAILABLE: _ClassVar[ControlLeaseErrorReason]
    CONTROL_LEASE_ERROR_REASON_SAFETY_LATCHED: _ClassVar[ControlLeaseErrorReason]
    CONTROL_LEASE_ERROR_REASON_INVALID_CONTROLLER: _ClassVar[ControlLeaseErrorReason]
ENDPOINT_DIED_REASON_UNSPECIFIED: EndpointDiedReason
ENDPOINT_DIED_REASON_SERVICE_GONE: EndpointDiedReason
ENDPOINT_DIED_REASON_SERVICE_SHUTTING_DOWN: EndpointDiedReason
ENDPOINT_DIED_REASON_RESOURCE_FAULT: EndpointDiedReason
ENDPOINT_DIED_REASON_SERVICE_RESTARTED: EndpointDiedReason
ENDPOINT_REVOKED_REASON_UNSPECIFIED: EndpointRevokedReason
ENDPOINT_REVOKED_REASON_PERMISSION_REVOKED: EndpointRevokedReason
ENDPOINT_REVOKED_REASON_PACKAGE_DISABLED: EndpointRevokedReason
ENDPOINT_REVOKED_REASON_POLICY_SUSPENDED: EndpointRevokedReason
DELIVERY_CLASS_UNSPECIFIED: DeliveryClass
DELIVERY_CLASS_RELIABLE: DeliveryClass
DELIVERY_CLASS_STATE: DeliveryClass
DELIVERY_CLASS_LOSSY: DeliveryClass
SUBSCRIPTION_CLOSED_REASON_UNSPECIFIED: SubscriptionClosedReason
SUBSCRIPTION_CLOSED_REASON_ENDPOINT_DIED: SubscriptionClosedReason
SUBSCRIPTION_CLOSED_REASON_ENDPOINT_REVOKED: SubscriptionClosedReason
SUBSCRIPTION_CLOSED_REASON_BACKPRESSURE: SubscriptionClosedReason
SUBSCRIPTION_CLOSED_REASON_SERVER_SHUTDOWN: SubscriptionClosedReason
TRUST_PROFILE_UNSPECIFIED: TrustProfile
TRUST_PROFILE_ORDINARY: TrustProfile
TRUST_PROFILE_OEM: TrustProfile
TRUST_PROFILE_PLATFORM: TrustProfile
CANCEL_DISPATCH_REASON_UNSPECIFIED: CancelDispatchReason
CANCEL_DISPATCH_REASON_CLIENT_CANCELLED: CancelDispatchReason
CANCEL_DISPATCH_REASON_DEADLINE_EXCEEDED: CancelDispatchReason
CANCEL_DISPATCH_REASON_CLIENT_GONE: CancelDispatchReason
CONTROLLER_CLASS_UNSPECIFIED: ControllerClass
CONTROLLER_CLASS_HUMAN: ControllerClass
CONTROLLER_CLASS_AI: ControllerClass
CONTROL_LEASE_ERROR_REASON_UNSPECIFIED: ControlLeaseErrorReason
CONTROL_LEASE_ERROR_REASON_HELD_BY_HUMAN: ControlLeaseErrorReason
CONTROL_LEASE_ERROR_REASON_HELD_BY_AI: ControlLeaseErrorReason
CONTROL_LEASE_ERROR_REASON_RESOURCE_UNAVAILABLE: ControlLeaseErrorReason
CONTROL_LEASE_ERROR_REASON_SAFETY_LATCHED: ControlLeaseErrorReason
CONTROL_LEASE_ERROR_REASON_INVALID_CONTROLLER: ControlLeaseErrorReason

class Envelope(_message.Message):
    __slots__ = ("protocol_major", "protocol_minor", "hello", "hello_ack", "resolve_endpoint", "resolve_endpoint_result", "register_endpoint", "register_endpoint_result", "endpoint_died", "endpoint_revoked", "unregister_endpoint", "unregister_endpoint_result", "request", "response", "cancel", "subscribe", "subscribe_result", "unsubscribe", "event", "unsubscribe_result", "subscription_closed", "dispatch", "dispatch_result", "cancel_dispatch", "ping", "pong", "acquire_control", "acquire_control_result", "release_control", "release_control_result")
    PROTOCOL_MAJOR_FIELD_NUMBER: _ClassVar[int]
    PROTOCOL_MINOR_FIELD_NUMBER: _ClassVar[int]
    HELLO_FIELD_NUMBER: _ClassVar[int]
    HELLO_ACK_FIELD_NUMBER: _ClassVar[int]
    RESOLVE_ENDPOINT_FIELD_NUMBER: _ClassVar[int]
    RESOLVE_ENDPOINT_RESULT_FIELD_NUMBER: _ClassVar[int]
    REGISTER_ENDPOINT_FIELD_NUMBER: _ClassVar[int]
    REGISTER_ENDPOINT_RESULT_FIELD_NUMBER: _ClassVar[int]
    ENDPOINT_DIED_FIELD_NUMBER: _ClassVar[int]
    ENDPOINT_REVOKED_FIELD_NUMBER: _ClassVar[int]
    UNREGISTER_ENDPOINT_FIELD_NUMBER: _ClassVar[int]
    UNREGISTER_ENDPOINT_RESULT_FIELD_NUMBER: _ClassVar[int]
    REQUEST_FIELD_NUMBER: _ClassVar[int]
    RESPONSE_FIELD_NUMBER: _ClassVar[int]
    CANCEL_FIELD_NUMBER: _ClassVar[int]
    SUBSCRIBE_FIELD_NUMBER: _ClassVar[int]
    SUBSCRIBE_RESULT_FIELD_NUMBER: _ClassVar[int]
    UNSUBSCRIBE_FIELD_NUMBER: _ClassVar[int]
    EVENT_FIELD_NUMBER: _ClassVar[int]
    UNSUBSCRIBE_RESULT_FIELD_NUMBER: _ClassVar[int]
    SUBSCRIPTION_CLOSED_FIELD_NUMBER: _ClassVar[int]
    DISPATCH_FIELD_NUMBER: _ClassVar[int]
    DISPATCH_RESULT_FIELD_NUMBER: _ClassVar[int]
    CANCEL_DISPATCH_FIELD_NUMBER: _ClassVar[int]
    PING_FIELD_NUMBER: _ClassVar[int]
    PONG_FIELD_NUMBER: _ClassVar[int]
    ACQUIRE_CONTROL_FIELD_NUMBER: _ClassVar[int]
    ACQUIRE_CONTROL_RESULT_FIELD_NUMBER: _ClassVar[int]
    RELEASE_CONTROL_FIELD_NUMBER: _ClassVar[int]
    RELEASE_CONTROL_RESULT_FIELD_NUMBER: _ClassVar[int]
    protocol_major: int
    protocol_minor: int
    hello: Hello
    hello_ack: HelloAck
    resolve_endpoint: ResolveEndpoint
    resolve_endpoint_result: ResolveEndpointResult
    register_endpoint: RegisterEndpoint
    register_endpoint_result: RegisterEndpointResult
    endpoint_died: EndpointDied
    endpoint_revoked: EndpointRevoked
    unregister_endpoint: UnregisterEndpoint
    unregister_endpoint_result: UnregisterEndpointResult
    request: Request
    response: Response
    cancel: Cancel
    subscribe: Subscribe
    subscribe_result: SubscribeResult
    unsubscribe: Unsubscribe
    event: Event
    unsubscribe_result: UnsubscribeResult
    subscription_closed: SubscriptionClosed
    dispatch: Dispatch
    dispatch_result: DispatchResult
    cancel_dispatch: CancelDispatch
    ping: Ping
    pong: Pong
    acquire_control: AcquireControl
    acquire_control_result: AcquireControlResult
    release_control: ReleaseControl
    release_control_result: ReleaseControlResult
    def __init__(self, protocol_major: _Optional[int] = ..., protocol_minor: _Optional[int] = ..., hello: _Optional[_Union[Hello, _Mapping]] = ..., hello_ack: _Optional[_Union[HelloAck, _Mapping]] = ..., resolve_endpoint: _Optional[_Union[ResolveEndpoint, _Mapping]] = ..., resolve_endpoint_result: _Optional[_Union[ResolveEndpointResult, _Mapping]] = ..., register_endpoint: _Optional[_Union[RegisterEndpoint, _Mapping]] = ..., register_endpoint_result: _Optional[_Union[RegisterEndpointResult, _Mapping]] = ..., endpoint_died: _Optional[_Union[EndpointDied, _Mapping]] = ..., endpoint_revoked: _Optional[_Union[EndpointRevoked, _Mapping]] = ..., unregister_endpoint: _Optional[_Union[UnregisterEndpoint, _Mapping]] = ..., unregister_endpoint_result: _Optional[_Union[UnregisterEndpointResult, _Mapping]] = ..., request: _Optional[_Union[Request, _Mapping]] = ..., response: _Optional[_Union[Response, _Mapping]] = ..., cancel: _Optional[_Union[Cancel, _Mapping]] = ..., subscribe: _Optional[_Union[Subscribe, _Mapping]] = ..., subscribe_result: _Optional[_Union[SubscribeResult, _Mapping]] = ..., unsubscribe: _Optional[_Union[Unsubscribe, _Mapping]] = ..., event: _Optional[_Union[Event, _Mapping]] = ..., unsubscribe_result: _Optional[_Union[UnsubscribeResult, _Mapping]] = ..., subscription_closed: _Optional[_Union[SubscriptionClosed, _Mapping]] = ..., dispatch: _Optional[_Union[Dispatch, _Mapping]] = ..., dispatch_result: _Optional[_Union[DispatchResult, _Mapping]] = ..., cancel_dispatch: _Optional[_Union[CancelDispatch, _Mapping]] = ..., ping: _Optional[_Union[Ping, _Mapping]] = ..., pong: _Optional[_Union[Pong, _Mapping]] = ..., acquire_control: _Optional[_Union[AcquireControl, _Mapping]] = ..., acquire_control_result: _Optional[_Union[AcquireControlResult, _Mapping]] = ..., release_control: _Optional[_Union[ReleaseControl, _Mapping]] = ..., release_control_result: _Optional[_Union[ReleaseControlResult, _Mapping]] = ...) -> None: ...

class Hello(_message.Message):
    __slots__ = ("min_protocol_major", "max_protocol_major", "max_protocol_minor", "sdk_name", "sdk_version", "declared_component_id")
    MIN_PROTOCOL_MAJOR_FIELD_NUMBER: _ClassVar[int]
    MAX_PROTOCOL_MAJOR_FIELD_NUMBER: _ClassVar[int]
    MAX_PROTOCOL_MINOR_FIELD_NUMBER: _ClassVar[int]
    SDK_NAME_FIELD_NUMBER: _ClassVar[int]
    SDK_VERSION_FIELD_NUMBER: _ClassVar[int]
    DECLARED_COMPONENT_ID_FIELD_NUMBER: _ClassVar[int]
    min_protocol_major: int
    max_protocol_major: int
    max_protocol_minor: int
    sdk_name: str
    sdk_version: str
    declared_component_id: str
    def __init__(self, min_protocol_major: _Optional[int] = ..., max_protocol_major: _Optional[int] = ..., max_protocol_minor: _Optional[int] = ..., sdk_name: _Optional[str] = ..., sdk_version: _Optional[str] = ..., declared_component_id: _Optional[str] = ...) -> None: ...

class HelloAck(_message.Message):
    __slots__ = ("success", "failure")
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    FAILURE_FIELD_NUMBER: _ClassVar[int]
    success: HelloAckSuccess
    failure: _status_pb2.Failure
    def __init__(self, success: _Optional[_Union[HelloAckSuccess, _Mapping]] = ..., failure: _Optional[_Union[_status_pb2.Failure, _Mapping]] = ...) -> None: ...

class HelloAckSuccess(_message.Message):
    __slots__ = ("protocol_major", "protocol_minor", "package_id", "component_id", "limits")
    PROTOCOL_MAJOR_FIELD_NUMBER: _ClassVar[int]
    PROTOCOL_MINOR_FIELD_NUMBER: _ClassVar[int]
    PACKAGE_ID_FIELD_NUMBER: _ClassVar[int]
    COMPONENT_ID_FIELD_NUMBER: _ClassVar[int]
    LIMITS_FIELD_NUMBER: _ClassVar[int]
    protocol_major: int
    protocol_minor: int
    package_id: str
    component_id: str
    limits: ConnectionLimits
    def __init__(self, protocol_major: _Optional[int] = ..., protocol_minor: _Optional[int] = ..., package_id: _Optional[str] = ..., component_id: _Optional[str] = ..., limits: _Optional[_Union[ConnectionLimits, _Mapping]] = ...) -> None: ...

class ConnectionLimits(_message.Message):
    __slots__ = ("max_frame_bytes", "default_method_payload_bytes", "max_inflight_requests", "max_inflight_payload_bytes", "max_outbound_queue_bytes", "max_subscriptions", "default_timeout_ms", "max_timeout_ms", "idle_timeout_ms")
    MAX_FRAME_BYTES_FIELD_NUMBER: _ClassVar[int]
    DEFAULT_METHOD_PAYLOAD_BYTES_FIELD_NUMBER: _ClassVar[int]
    MAX_INFLIGHT_REQUESTS_FIELD_NUMBER: _ClassVar[int]
    MAX_INFLIGHT_PAYLOAD_BYTES_FIELD_NUMBER: _ClassVar[int]
    MAX_OUTBOUND_QUEUE_BYTES_FIELD_NUMBER: _ClassVar[int]
    MAX_SUBSCRIPTIONS_FIELD_NUMBER: _ClassVar[int]
    DEFAULT_TIMEOUT_MS_FIELD_NUMBER: _ClassVar[int]
    MAX_TIMEOUT_MS_FIELD_NUMBER: _ClassVar[int]
    IDLE_TIMEOUT_MS_FIELD_NUMBER: _ClassVar[int]
    max_frame_bytes: int
    default_method_payload_bytes: int
    max_inflight_requests: int
    max_inflight_payload_bytes: int
    max_outbound_queue_bytes: int
    max_subscriptions: int
    default_timeout_ms: int
    max_timeout_ms: int
    idle_timeout_ms: int
    def __init__(self, max_frame_bytes: _Optional[int] = ..., default_method_payload_bytes: _Optional[int] = ..., max_inflight_requests: _Optional[int] = ..., max_inflight_payload_bytes: _Optional[int] = ..., max_outbound_queue_bytes: _Optional[int] = ..., max_subscriptions: _Optional[int] = ..., default_timeout_ms: _Optional[int] = ..., max_timeout_ms: _Optional[int] = ..., idle_timeout_ms: _Optional[int] = ...) -> None: ...

class ResolveEndpoint(_message.Message):
    __slots__ = ("request_id", "interface_id", "min_interface_major", "max_interface_major", "selector", "explicit_component")
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    INTERFACE_ID_FIELD_NUMBER: _ClassVar[int]
    MIN_INTERFACE_MAJOR_FIELD_NUMBER: _ClassVar[int]
    MAX_INTERFACE_MAJOR_FIELD_NUMBER: _ClassVar[int]
    SELECTOR_FIELD_NUMBER: _ClassVar[int]
    EXPLICIT_COMPONENT_FIELD_NUMBER: _ClassVar[int]
    request_id: int
    interface_id: str
    min_interface_major: int
    max_interface_major: int
    selector: ResourceSelector
    explicit_component: str
    def __init__(self, request_id: _Optional[int] = ..., interface_id: _Optional[str] = ..., min_interface_major: _Optional[int] = ..., max_interface_major: _Optional[int] = ..., selector: _Optional[_Union[ResourceSelector, _Mapping]] = ..., explicit_component: _Optional[str] = ...) -> None: ...

class ResourceSelector(_message.Message):
    __slots__ = ("type", "role")
    TYPE_FIELD_NUMBER: _ClassVar[int]
    ROLE_FIELD_NUMBER: _ClassVar[int]
    type: str
    role: str
    def __init__(self, type: _Optional[str] = ..., role: _Optional[str] = ...) -> None: ...

class ResolveEndpointResult(_message.Message):
    __slots__ = ("request_id", "success", "failure")
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    FAILURE_FIELD_NUMBER: _ClassVar[int]
    request_id: int
    success: ResolveEndpointSuccess
    failure: _status_pb2.Failure
    def __init__(self, request_id: _Optional[int] = ..., success: _Optional[_Union[ResolveEndpointSuccess, _Mapping]] = ..., failure: _Optional[_Union[_status_pb2.Failure, _Mapping]] = ...) -> None: ...

class ResolveEndpointSuccess(_message.Message):
    __slots__ = ("endpoint_id", "interface_major", "interface_minor", "interface_schema_hash", "resource_handle", "catalog_revision")
    ENDPOINT_ID_FIELD_NUMBER: _ClassVar[int]
    INTERFACE_MAJOR_FIELD_NUMBER: _ClassVar[int]
    INTERFACE_MINOR_FIELD_NUMBER: _ClassVar[int]
    INTERFACE_SCHEMA_HASH_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_HANDLE_FIELD_NUMBER: _ClassVar[int]
    CATALOG_REVISION_FIELD_NUMBER: _ClassVar[int]
    endpoint_id: int
    interface_major: int
    interface_minor: int
    interface_schema_hash: bytes
    resource_handle: str
    catalog_revision: int
    def __init__(self, endpoint_id: _Optional[int] = ..., interface_major: _Optional[int] = ..., interface_minor: _Optional[int] = ..., interface_schema_hash: _Optional[bytes] = ..., resource_handle: _Optional[str] = ..., catalog_revision: _Optional[int] = ...) -> None: ...

class RegisterEndpoint(_message.Message):
    __slots__ = ("request_id", "interface_id", "interface_major", "interface_minor", "interface_schema_hash", "resource_handle")
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    INTERFACE_ID_FIELD_NUMBER: _ClassVar[int]
    INTERFACE_MAJOR_FIELD_NUMBER: _ClassVar[int]
    INTERFACE_MINOR_FIELD_NUMBER: _ClassVar[int]
    INTERFACE_SCHEMA_HASH_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_HANDLE_FIELD_NUMBER: _ClassVar[int]
    request_id: int
    interface_id: str
    interface_major: int
    interface_minor: int
    interface_schema_hash: bytes
    resource_handle: str
    def __init__(self, request_id: _Optional[int] = ..., interface_id: _Optional[str] = ..., interface_major: _Optional[int] = ..., interface_minor: _Optional[int] = ..., interface_schema_hash: _Optional[bytes] = ..., resource_handle: _Optional[str] = ...) -> None: ...

class RegisterEndpointResult(_message.Message):
    __slots__ = ("request_id", "success", "failure")
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    FAILURE_FIELD_NUMBER: _ClassVar[int]
    request_id: int
    success: RegisterEndpointSuccess
    failure: _status_pb2.Failure
    def __init__(self, request_id: _Optional[int] = ..., success: _Optional[_Union[RegisterEndpointSuccess, _Mapping]] = ..., failure: _Optional[_Union[_status_pb2.Failure, _Mapping]] = ...) -> None: ...

class RegisterEndpointSuccess(_message.Message):
    __slots__ = ("endpoint_id",)
    ENDPOINT_ID_FIELD_NUMBER: _ClassVar[int]
    endpoint_id: int
    def __init__(self, endpoint_id: _Optional[int] = ...) -> None: ...

class UnregisterEndpoint(_message.Message):
    __slots__ = ("request_id", "endpoint_id", "drain")
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    ENDPOINT_ID_FIELD_NUMBER: _ClassVar[int]
    DRAIN_FIELD_NUMBER: _ClassVar[int]
    request_id: int
    endpoint_id: int
    drain: bool
    def __init__(self, request_id: _Optional[int] = ..., endpoint_id: _Optional[int] = ..., drain: bool = ...) -> None: ...

class UnregisterEndpointResult(_message.Message):
    __slots__ = ("request_id", "success", "failure")
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    FAILURE_FIELD_NUMBER: _ClassVar[int]
    request_id: int
    success: UnregisterEndpointSuccess
    failure: _status_pb2.Failure
    def __init__(self, request_id: _Optional[int] = ..., success: _Optional[_Union[UnregisterEndpointSuccess, _Mapping]] = ..., failure: _Optional[_Union[_status_pb2.Failure, _Mapping]] = ...) -> None: ...

class UnregisterEndpointSuccess(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class EndpointDied(_message.Message):
    __slots__ = ("endpoint_id", "reason")
    ENDPOINT_ID_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    endpoint_id: int
    reason: EndpointDiedReason
    def __init__(self, endpoint_id: _Optional[int] = ..., reason: _Optional[_Union[EndpointDiedReason, str]] = ...) -> None: ...

class EndpointRevoked(_message.Message):
    __slots__ = ("endpoint_id", "reason")
    ENDPOINT_ID_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    endpoint_id: int
    reason: EndpointRevokedReason
    def __init__(self, endpoint_id: _Optional[int] = ..., reason: _Optional[_Union[EndpointRevokedReason, str]] = ...) -> None: ...

class Request(_message.Message):
    __slots__ = ("request_id", "endpoint_id", "method_id", "timeout_ms", "payload")
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    ENDPOINT_ID_FIELD_NUMBER: _ClassVar[int]
    METHOD_ID_FIELD_NUMBER: _ClassVar[int]
    TIMEOUT_MS_FIELD_NUMBER: _ClassVar[int]
    PAYLOAD_FIELD_NUMBER: _ClassVar[int]
    request_id: int
    endpoint_id: int
    method_id: int
    timeout_ms: int
    payload: bytes
    def __init__(self, request_id: _Optional[int] = ..., endpoint_id: _Optional[int] = ..., method_id: _Optional[int] = ..., timeout_ms: _Optional[int] = ..., payload: _Optional[bytes] = ...) -> None: ...

class Response(_message.Message):
    __slots__ = ("request_id", "success", "failure")
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    FAILURE_FIELD_NUMBER: _ClassVar[int]
    request_id: int
    success: _status_pb2.Success
    failure: _status_pb2.Failure
    def __init__(self, request_id: _Optional[int] = ..., success: _Optional[_Union[_status_pb2.Success, _Mapping]] = ..., failure: _Optional[_Union[_status_pb2.Failure, _Mapping]] = ...) -> None: ...

class Cancel(_message.Message):
    __slots__ = ("request_id",)
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    request_id: int
    def __init__(self, request_id: _Optional[int] = ...) -> None: ...

class Subscribe(_message.Message):
    __slots__ = ("request_id", "endpoint_id", "event_id", "payload")
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    ENDPOINT_ID_FIELD_NUMBER: _ClassVar[int]
    EVENT_ID_FIELD_NUMBER: _ClassVar[int]
    PAYLOAD_FIELD_NUMBER: _ClassVar[int]
    request_id: int
    endpoint_id: int
    event_id: int
    payload: bytes
    def __init__(self, request_id: _Optional[int] = ..., endpoint_id: _Optional[int] = ..., event_id: _Optional[int] = ..., payload: _Optional[bytes] = ...) -> None: ...

class SubscribeResult(_message.Message):
    __slots__ = ("request_id", "success", "failure")
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    FAILURE_FIELD_NUMBER: _ClassVar[int]
    request_id: int
    success: SubscribeSuccess
    failure: _status_pb2.Failure
    def __init__(self, request_id: _Optional[int] = ..., success: _Optional[_Union[SubscribeSuccess, _Mapping]] = ..., failure: _Optional[_Union[_status_pb2.Failure, _Mapping]] = ...) -> None: ...

class SubscribeSuccess(_message.Message):
    __slots__ = ("subscription_id", "delivery_class")
    SUBSCRIPTION_ID_FIELD_NUMBER: _ClassVar[int]
    DELIVERY_CLASS_FIELD_NUMBER: _ClassVar[int]
    subscription_id: int
    delivery_class: DeliveryClass
    def __init__(self, subscription_id: _Optional[int] = ..., delivery_class: _Optional[_Union[DeliveryClass, str]] = ...) -> None: ...

class Unsubscribe(_message.Message):
    __slots__ = ("request_id", "subscription_id")
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    SUBSCRIPTION_ID_FIELD_NUMBER: _ClassVar[int]
    request_id: int
    subscription_id: int
    def __init__(self, request_id: _Optional[int] = ..., subscription_id: _Optional[int] = ...) -> None: ...

class UnsubscribeResult(_message.Message):
    __slots__ = ("request_id", "success", "failure")
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    FAILURE_FIELD_NUMBER: _ClassVar[int]
    request_id: int
    success: UnsubscribeSuccess
    failure: _status_pb2.Failure
    def __init__(self, request_id: _Optional[int] = ..., success: _Optional[_Union[UnsubscribeSuccess, _Mapping]] = ..., failure: _Optional[_Union[_status_pb2.Failure, _Mapping]] = ...) -> None: ...

class UnsubscribeSuccess(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class Event(_message.Message):
    __slots__ = ("subscription_id", "sequence", "endpoint_id", "event_id", "payload", "dropped")
    SUBSCRIPTION_ID_FIELD_NUMBER: _ClassVar[int]
    SEQUENCE_FIELD_NUMBER: _ClassVar[int]
    ENDPOINT_ID_FIELD_NUMBER: _ClassVar[int]
    EVENT_ID_FIELD_NUMBER: _ClassVar[int]
    PAYLOAD_FIELD_NUMBER: _ClassVar[int]
    DROPPED_FIELD_NUMBER: _ClassVar[int]
    subscription_id: int
    sequence: int
    endpoint_id: int
    event_id: int
    payload: bytes
    dropped: int
    def __init__(self, subscription_id: _Optional[int] = ..., sequence: _Optional[int] = ..., endpoint_id: _Optional[int] = ..., event_id: _Optional[int] = ..., payload: _Optional[bytes] = ..., dropped: _Optional[int] = ...) -> None: ...

class SubscriptionClosed(_message.Message):
    __slots__ = ("subscription_id", "reason")
    SUBSCRIPTION_ID_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    subscription_id: int
    reason: SubscriptionClosedReason
    def __init__(self, subscription_id: _Optional[int] = ..., reason: _Optional[_Union[SubscriptionClosedReason, str]] = ...) -> None: ...

class Dispatch(_message.Message):
    __slots__ = ("route_id", "endpoint_id", "method_id", "remaining_ms", "payload", "caller")
    ROUTE_ID_FIELD_NUMBER: _ClassVar[int]
    ENDPOINT_ID_FIELD_NUMBER: _ClassVar[int]
    METHOD_ID_FIELD_NUMBER: _ClassVar[int]
    REMAINING_MS_FIELD_NUMBER: _ClassVar[int]
    PAYLOAD_FIELD_NUMBER: _ClassVar[int]
    CALLER_FIELD_NUMBER: _ClassVar[int]
    route_id: int
    endpoint_id: int
    method_id: int
    remaining_ms: int
    payload: bytes
    caller: CallerContext
    def __init__(self, route_id: _Optional[int] = ..., endpoint_id: _Optional[int] = ..., method_id: _Optional[int] = ..., remaining_ms: _Optional[int] = ..., payload: _Optional[bytes] = ..., caller: _Optional[_Union[CallerContext, _Mapping]] = ...) -> None: ...

class CallerContext(_message.Message):
    __slots__ = ("package_id", "component_id", "uid", "gid", "pid", "trust_profile", "granted_permissions")
    PACKAGE_ID_FIELD_NUMBER: _ClassVar[int]
    COMPONENT_ID_FIELD_NUMBER: _ClassVar[int]
    UID_FIELD_NUMBER: _ClassVar[int]
    GID_FIELD_NUMBER: _ClassVar[int]
    PID_FIELD_NUMBER: _ClassVar[int]
    TRUST_PROFILE_FIELD_NUMBER: _ClassVar[int]
    GRANTED_PERMISSIONS_FIELD_NUMBER: _ClassVar[int]
    package_id: str
    component_id: str
    uid: int
    gid: int
    pid: int
    trust_profile: TrustProfile
    granted_permissions: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, package_id: _Optional[str] = ..., component_id: _Optional[str] = ..., uid: _Optional[int] = ..., gid: _Optional[int] = ..., pid: _Optional[int] = ..., trust_profile: _Optional[_Union[TrustProfile, str]] = ..., granted_permissions: _Optional[_Iterable[str]] = ...) -> None: ...

class DispatchResult(_message.Message):
    __slots__ = ("route_id", "success", "failure")
    ROUTE_ID_FIELD_NUMBER: _ClassVar[int]
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    FAILURE_FIELD_NUMBER: _ClassVar[int]
    route_id: int
    success: _status_pb2.Success
    failure: _status_pb2.Failure
    def __init__(self, route_id: _Optional[int] = ..., success: _Optional[_Union[_status_pb2.Success, _Mapping]] = ..., failure: _Optional[_Union[_status_pb2.Failure, _Mapping]] = ...) -> None: ...

class CancelDispatch(_message.Message):
    __slots__ = ("route_id", "reason")
    ROUTE_ID_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    route_id: int
    reason: CancelDispatchReason
    def __init__(self, route_id: _Optional[int] = ..., reason: _Optional[_Union[CancelDispatchReason, str]] = ...) -> None: ...

class Ping(_message.Message):
    __slots__ = ("nonce",)
    NONCE_FIELD_NUMBER: _ClassVar[int]
    nonce: int
    def __init__(self, nonce: _Optional[int] = ...) -> None: ...

class Pong(_message.Message):
    __slots__ = ("nonce",)
    NONCE_FIELD_NUMBER: _ClassVar[int]
    nonce: int
    def __init__(self, nonce: _Optional[int] = ...) -> None: ...

class AcquireControl(_message.Message):
    __slots__ = ("request_id", "controller_class", "resource", "requested_deadline_nanos")
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    CONTROLLER_CLASS_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_FIELD_NUMBER: _ClassVar[int]
    REQUESTED_DEADLINE_NANOS_FIELD_NUMBER: _ClassVar[int]
    request_id: int
    controller_class: ControllerClass
    resource: ResourceSelector
    requested_deadline_nanos: int
    def __init__(self, request_id: _Optional[int] = ..., controller_class: _Optional[_Union[ControllerClass, str]] = ..., resource: _Optional[_Union[ResourceSelector, _Mapping]] = ..., requested_deadline_nanos: _Optional[int] = ...) -> None: ...

class AcquireControlResult(_message.Message):
    __slots__ = ("request_id", "success", "failure")
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    FAILURE_FIELD_NUMBER: _ClassVar[int]
    request_id: int
    success: AcquireControlSuccess
    failure: _status_pb2.Failure
    def __init__(self, request_id: _Optional[int] = ..., success: _Optional[_Union[AcquireControlSuccess, _Mapping]] = ..., failure: _Optional[_Union[_status_pb2.Failure, _Mapping]] = ...) -> None: ...

class AcquireControlSuccess(_message.Message):
    __slots__ = ("lease_id", "motion_epoch", "deadline_nanos", "resource_handle")
    LEASE_ID_FIELD_NUMBER: _ClassVar[int]
    MOTION_EPOCH_FIELD_NUMBER: _ClassVar[int]
    DEADLINE_NANOS_FIELD_NUMBER: _ClassVar[int]
    RESOURCE_HANDLE_FIELD_NUMBER: _ClassVar[int]
    lease_id: int
    motion_epoch: int
    deadline_nanos: int
    resource_handle: str
    def __init__(self, lease_id: _Optional[int] = ..., motion_epoch: _Optional[int] = ..., deadline_nanos: _Optional[int] = ..., resource_handle: _Optional[str] = ...) -> None: ...

class ReleaseControl(_message.Message):
    __slots__ = ("request_id", "lease_id")
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    LEASE_ID_FIELD_NUMBER: _ClassVar[int]
    request_id: int
    lease_id: int
    def __init__(self, request_id: _Optional[int] = ..., lease_id: _Optional[int] = ...) -> None: ...

class ReleaseControlResult(_message.Message):
    __slots__ = ("request_id", "success", "failure")
    REQUEST_ID_FIELD_NUMBER: _ClassVar[int]
    SUCCESS_FIELD_NUMBER: _ClassVar[int]
    FAILURE_FIELD_NUMBER: _ClassVar[int]
    request_id: int
    success: _status_pb2.Success
    failure: _status_pb2.Failure
    def __init__(self, request_id: _Optional[int] = ..., success: _Optional[_Union[_status_pb2.Success, _Mapping]] = ..., failure: _Optional[_Union[_status_pb2.Failure, _Mapping]] = ...) -> None: ...

class ControlLeaseErrorDetail(_message.Message):
    __slots__ = ("reason",)
    REASON_FIELD_NUMBER: _ClassVar[int]
    reason: ControlLeaseErrorReason
    def __init__(self, reason: _Optional[_Union[ControlLeaseErrorReason, str]] = ...) -> None: ...
