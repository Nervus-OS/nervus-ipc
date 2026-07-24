from nervus.ipc.v1 import method_registry_pb2 as _method_registry_pb2
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Mapping as _Mapping, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class BaseMotionMethod(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    BASE_MOTION_METHOD_UNSPECIFIED: _ClassVar[BaseMotionMethod]
    BASE_MOTION_METHOD_SET_VELOCITY: _ClassVar[BaseMotionMethod]
    BASE_MOTION_METHOD_STOP: _ClassVar[BaseMotionMethod]
    BASE_MOTION_METHOD_GET_MOTION_STATE: _ClassVar[BaseMotionMethod]
    BASE_MOTION_METHOD_SUBSCRIBE_MOTION_STATE: _ClassVar[BaseMotionMethod]

class Readiness(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    READINESS_UNSPECIFIED: _ClassVar[Readiness]
    READINESS_READY: _ClassVar[Readiness]
    READINESS_NOT_READY: _ClassVar[Readiness]
    READINESS_TRANSITIONING: _ClassVar[Readiness]
    READINESS_FAULT: _ClassVar[Readiness]

class BaseMotionReason(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    BASE_MOTION_REASON_UNSPECIFIED: _ClassVar[BaseMotionReason]
    BASE_MOTION_REASON_LOCOMOTION_NOT_READY: _ClassVar[BaseMotionReason]
    BASE_MOTION_REASON_SAFETY_LATCHED: _ClassVar[BaseMotionReason]
    BASE_MOTION_REASON_STALE_EPOCH: _ClassVar[BaseMotionReason]
    BASE_MOTION_REASON_CONTROL_NOT_HELD: _ClassVar[BaseMotionReason]
    BASE_MOTION_REASON_RESOURCE_UNAVAILABLE: _ClassVar[BaseMotionReason]
    BASE_MOTION_REASON_RESOURCE_FAULT: _ClassVar[BaseMotionReason]
BASE_MOTION_METHOD_UNSPECIFIED: BaseMotionMethod
BASE_MOTION_METHOD_SET_VELOCITY: BaseMotionMethod
BASE_MOTION_METHOD_STOP: BaseMotionMethod
BASE_MOTION_METHOD_GET_MOTION_STATE: BaseMotionMethod
BASE_MOTION_METHOD_SUBSCRIBE_MOTION_STATE: BaseMotionMethod
READINESS_UNSPECIFIED: Readiness
READINESS_READY: Readiness
READINESS_NOT_READY: Readiness
READINESS_TRANSITIONING: Readiness
READINESS_FAULT: Readiness
BASE_MOTION_REASON_UNSPECIFIED: BaseMotionReason
BASE_MOTION_REASON_LOCOMOTION_NOT_READY: BaseMotionReason
BASE_MOTION_REASON_SAFETY_LATCHED: BaseMotionReason
BASE_MOTION_REASON_STALE_EPOCH: BaseMotionReason
BASE_MOTION_REASON_CONTROL_NOT_HELD: BaseMotionReason
BASE_MOTION_REASON_RESOURCE_UNAVAILABLE: BaseMotionReason
BASE_MOTION_REASON_RESOURCE_FAULT: BaseMotionReason

class SetVelocityRequest(_message.Message):
    __slots__ = ("linear_x", "linear_y", "angular_z")
    LINEAR_X_FIELD_NUMBER: _ClassVar[int]
    LINEAR_Y_FIELD_NUMBER: _ClassVar[int]
    ANGULAR_Z_FIELD_NUMBER: _ClassVar[int]
    linear_x: float
    linear_y: float
    angular_z: float
    def __init__(self, linear_x: _Optional[float] = ..., linear_y: _Optional[float] = ..., angular_z: _Optional[float] = ...) -> None: ...

class StopRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class GetMotionStateRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class SubscribeMotionStateRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class VelocityLimits(_message.Message):
    __slots__ = ("max_linear", "max_angular")
    MAX_LINEAR_FIELD_NUMBER: _ClassVar[int]
    MAX_ANGULAR_FIELD_NUMBER: _ClassVar[int]
    max_linear: float
    max_angular: float
    def __init__(self, max_linear: _Optional[float] = ..., max_angular: _Optional[float] = ...) -> None: ...

class MotionState(_message.Message):
    __slots__ = ("readiness", "supports_linear_x", "supports_linear_y", "supports_angular_z", "velocity_limits", "active_motion_epoch")
    READINESS_FIELD_NUMBER: _ClassVar[int]
    SUPPORTS_LINEAR_X_FIELD_NUMBER: _ClassVar[int]
    SUPPORTS_LINEAR_Y_FIELD_NUMBER: _ClassVar[int]
    SUPPORTS_ANGULAR_Z_FIELD_NUMBER: _ClassVar[int]
    VELOCITY_LIMITS_FIELD_NUMBER: _ClassVar[int]
    ACTIVE_MOTION_EPOCH_FIELD_NUMBER: _ClassVar[int]
    readiness: Readiness
    supports_linear_x: bool
    supports_linear_y: bool
    supports_angular_z: bool
    velocity_limits: VelocityLimits
    active_motion_epoch: int
    def __init__(self, readiness: _Optional[_Union[Readiness, str]] = ..., supports_linear_x: bool = ..., supports_linear_y: bool = ..., supports_angular_z: bool = ..., velocity_limits: _Optional[_Union[VelocityLimits, _Mapping]] = ..., active_motion_epoch: _Optional[int] = ...) -> None: ...

class BaseMotionErrorDetail(_message.Message):
    __slots__ = ("reason",)
    REASON_FIELD_NUMBER: _ClassVar[int]
    reason: BaseMotionReason
    def __init__(self, reason: _Optional[_Union[BaseMotionReason, str]] = ...) -> None: ...
