from nervus.ipc.v1 import method_registry_pb2 as _method_registry_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Iterable as _Iterable, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class RawGaitMethod(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    RAW_GAIT_METHOD_UNSPECIFIED: _ClassVar[RawGaitMethod]
    RAW_GAIT_METHOD_GET_GAIT_STATE: _ClassVar[RawGaitMethod]
    RAW_GAIT_METHOD_SET_RAW_GAIT: _ClassVar[RawGaitMethod]

class GaitType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    GAIT_TYPE_UNSPECIFIED: _ClassVar[GaitType]
    GAIT_TYPE_TROT: _ClassVar[GaitType]
    GAIT_TYPE_WALK: _ClassVar[GaitType]
    GAIT_TYPE_BOUND: _ClassVar[GaitType]

class RawGaitReason(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    RAW_GAIT_REASON_UNSPECIFIED: _ClassVar[RawGaitReason]
    RAW_GAIT_REASON_SAFETY_LATCHED: _ClassVar[RawGaitReason]
    RAW_GAIT_REASON_CONTROL_NOT_HELD: _ClassVar[RawGaitReason]
    RAW_GAIT_REASON_UNSUPPORTED_GAIT: _ClassVar[RawGaitReason]
    RAW_GAIT_REASON_PHASE_OUT_OF_RANGE: _ClassVar[RawGaitReason]
RAW_GAIT_METHOD_UNSPECIFIED: RawGaitMethod
RAW_GAIT_METHOD_GET_GAIT_STATE: RawGaitMethod
RAW_GAIT_METHOD_SET_RAW_GAIT: RawGaitMethod
GAIT_TYPE_UNSPECIFIED: GaitType
GAIT_TYPE_TROT: GaitType
GAIT_TYPE_WALK: GaitType
GAIT_TYPE_BOUND: GaitType
RAW_GAIT_REASON_UNSPECIFIED: RawGaitReason
RAW_GAIT_REASON_SAFETY_LATCHED: RawGaitReason
RAW_GAIT_REASON_CONTROL_NOT_HELD: RawGaitReason
RAW_GAIT_REASON_UNSUPPORTED_GAIT: RawGaitReason
RAW_GAIT_REASON_PHASE_OUT_OF_RANGE: RawGaitReason

class GetGaitStateRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class GaitState(_message.Message):
    __slots__ = ("active_gait", "phase", "supported_gaits")
    ACTIVE_GAIT_FIELD_NUMBER: _ClassVar[int]
    PHASE_FIELD_NUMBER: _ClassVar[int]
    SUPPORTED_GAITS_FIELD_NUMBER: _ClassVar[int]
    active_gait: GaitType
    phase: float
    supported_gaits: _containers.RepeatedScalarFieldContainer[GaitType]
    def __init__(self, active_gait: _Optional[_Union[GaitType, str]] = ..., phase: _Optional[float] = ..., supported_gaits: _Optional[_Iterable[_Union[GaitType, str]]] = ...) -> None: ...

class SetRawGaitRequest(_message.Message):
    __slots__ = ("gait", "stride_frequency_hz", "phase_offset_rad")
    GAIT_FIELD_NUMBER: _ClassVar[int]
    STRIDE_FREQUENCY_HZ_FIELD_NUMBER: _ClassVar[int]
    PHASE_OFFSET_RAD_FIELD_NUMBER: _ClassVar[int]
    gait: GaitType
    stride_frequency_hz: float
    phase_offset_rad: float
    def __init__(self, gait: _Optional[_Union[GaitType, str]] = ..., stride_frequency_hz: _Optional[float] = ..., phase_offset_rad: _Optional[float] = ...) -> None: ...

class SetRawGaitResponse(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class RawGaitErrorDetail(_message.Message):
    __slots__ = ("reason",)
    REASON_FIELD_NUMBER: _ClassVar[int]
    reason: RawGaitReason
    def __init__(self, reason: _Optional[_Union[RawGaitReason, str]] = ...) -> None: ...
