from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class StopPhase(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    STOP_PHASE_UNSPECIFIED: _ClassVar[StopPhase]
    STOP_PHASE_REQUESTED: _ClassVar[StopPhase]
    STOP_PHASE_SENT: _ClassVar[StopPhase]
    STOP_PHASE_PROVIDER_ACCEPTED: _ClassVar[StopPhase]
    STOP_PHASE_MCU_ACKED: _ClassVar[StopPhase]
    STOP_PHASE_OUTPUT_DISABLED: _ClassVar[StopPhase]
    STOP_PHASE_STANDSTILL_CONFIRMED: _ClassVar[StopPhase]
    STOP_PHASE_DELIVERY_FAULT: _ClassVar[StopPhase]
    STOP_PHASE_STANDSTILL_TIMEOUT: _ClassVar[StopPhase]

class HaltReason(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    HALT_REASON_UNSPECIFIED: _ClassVar[HaltReason]
    HALT_REASON_OPERATOR_ESTOP: _ClassVar[HaltReason]
    HALT_REASON_DEADMAN_TIMEOUT: _ClassVar[HaltReason]
    HALT_REASON_PROVIDER_FAULT: _ClassVar[HaltReason]
    HALT_REASON_HEARTBEAT_LOST: _ClassVar[HaltReason]
    HALT_REASON_SUPERVISOR_ESCALATION: _ClassVar[HaltReason]
    HALT_REASON_EXTERNAL_TRIP: _ClassVar[HaltReason]

class FaultCode(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    FAULT_CODE_UNSPECIFIED: _ClassVar[FaultCode]
    FAULT_CODE_DEVICE_ERROR: _ClassVar[FaultCode]
    FAULT_CODE_LINK_LOST: _ClassVar[FaultCode]

class StandstillEvidence(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    STANDSTILL_EVIDENCE_UNSPECIFIED: _ClassVar[StandstillEvidence]
    STANDSTILL_EVIDENCE_ENCODER: _ClassVar[StandstillEvidence]
    STANDSTILL_EVIDENCE_VELOCITY_ESTIMATE: _ClassVar[StandstillEvidence]
    STANDSTILL_EVIDENCE_HARDWARE_STATE: _ClassVar[StandstillEvidence]

class BaseMotionErrorReason(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    BASE_MOTION_ERROR_REASON_UNSPECIFIED: _ClassVar[BaseMotionErrorReason]
    BASE_MOTION_ERROR_REASON_SAFETY_LATCHED: _ClassVar[BaseMotionErrorReason]
    BASE_MOTION_ERROR_REASON_STALE_EPOCH: _ClassVar[BaseMotionErrorReason]
    BASE_MOTION_ERROR_REASON_LOCOMOTION_NOT_READY: _ClassVar[BaseMotionErrorReason]
STOP_PHASE_UNSPECIFIED: StopPhase
STOP_PHASE_REQUESTED: StopPhase
STOP_PHASE_SENT: StopPhase
STOP_PHASE_PROVIDER_ACCEPTED: StopPhase
STOP_PHASE_MCU_ACKED: StopPhase
STOP_PHASE_OUTPUT_DISABLED: StopPhase
STOP_PHASE_STANDSTILL_CONFIRMED: StopPhase
STOP_PHASE_DELIVERY_FAULT: StopPhase
STOP_PHASE_STANDSTILL_TIMEOUT: StopPhase
HALT_REASON_UNSPECIFIED: HaltReason
HALT_REASON_OPERATOR_ESTOP: HaltReason
HALT_REASON_DEADMAN_TIMEOUT: HaltReason
HALT_REASON_PROVIDER_FAULT: HaltReason
HALT_REASON_HEARTBEAT_LOST: HaltReason
HALT_REASON_SUPERVISOR_ESCALATION: HaltReason
HALT_REASON_EXTERNAL_TRIP: HaltReason
FAULT_CODE_UNSPECIFIED: FaultCode
FAULT_CODE_DEVICE_ERROR: FaultCode
FAULT_CODE_LINK_LOST: FaultCode
STANDSTILL_EVIDENCE_UNSPECIFIED: StandstillEvidence
STANDSTILL_EVIDENCE_ENCODER: StandstillEvidence
STANDSTILL_EVIDENCE_VELOCITY_ESTIMATE: StandstillEvidence
STANDSTILL_EVIDENCE_HARDWARE_STATE: StandstillEvidence
BASE_MOTION_ERROR_REASON_UNSPECIFIED: BaseMotionErrorReason
BASE_MOTION_ERROR_REASON_SAFETY_LATCHED: BaseMotionErrorReason
BASE_MOTION_ERROR_REASON_STALE_EPOCH: BaseMotionErrorReason
BASE_MOTION_ERROR_REASON_LOCOMOTION_NOT_READY: BaseMotionErrorReason

class SafetyHalt(_message.Message):
    __slots__ = ("motion_epoch", "reason", "accept_deadline_ms")
    MOTION_EPOCH_FIELD_NUMBER: _ClassVar[int]
    REASON_FIELD_NUMBER: _ClassVar[int]
    ACCEPT_DEADLINE_MS_FIELD_NUMBER: _ClassVar[int]
    motion_epoch: int
    reason: HaltReason
    accept_deadline_ms: int
    def __init__(self, motion_epoch: _Optional[int] = ..., reason: _Optional[_Union[HaltReason, str]] = ..., accept_deadline_ms: _Optional[int] = ...) -> None: ...

class HaltAccepted(_message.Message):
    __slots__ = ("motion_epoch",)
    MOTION_EPOCH_FIELD_NUMBER: _ClassVar[int]
    motion_epoch: int
    def __init__(self, motion_epoch: _Optional[int] = ...) -> None: ...

class StopProgress(_message.Message):
    __slots__ = ("motion_epoch", "phase")
    MOTION_EPOCH_FIELD_NUMBER: _ClassVar[int]
    PHASE_FIELD_NUMBER: _ClassVar[int]
    motion_epoch: int
    phase: StopPhase
    def __init__(self, motion_epoch: _Optional[int] = ..., phase: _Optional[_Union[StopPhase, str]] = ...) -> None: ...

class StandstillConfirmed(_message.Message):
    __slots__ = ("motion_epoch", "evidence")
    MOTION_EPOCH_FIELD_NUMBER: _ClassVar[int]
    EVIDENCE_FIELD_NUMBER: _ClassVar[int]
    motion_epoch: int
    evidence: StandstillEvidence
    def __init__(self, motion_epoch: _Optional[int] = ..., evidence: _Optional[_Union[StandstillEvidence, str]] = ...) -> None: ...

class ProviderFault(_message.Message):
    __slots__ = ("motion_epoch", "code")
    MOTION_EPOCH_FIELD_NUMBER: _ClassVar[int]
    CODE_FIELD_NUMBER: _ClassVar[int]
    motion_epoch: int
    code: FaultCode
    def __init__(self, motion_epoch: _Optional[int] = ..., code: _Optional[_Union[FaultCode, str]] = ...) -> None: ...
