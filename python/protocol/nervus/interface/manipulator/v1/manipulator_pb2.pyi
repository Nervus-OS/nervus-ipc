from nervus.ipc.v1 import method_registry_pb2 as _method_registry_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Iterable as _Iterable, Mapping as _Mapping, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ManipulatorMethod(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    MANIPULATOR_METHOD_UNSPECIFIED: _ClassVar[ManipulatorMethod]
    MANIPULATOR_METHOD_GET_ARM_STATE: _ClassVar[ManipulatorMethod]
    MANIPULATOR_METHOD_SUBSCRIBE_ARM_STATE: _ClassVar[ManipulatorMethod]
    MANIPULATOR_METHOD_MOVE_TO_POSE: _ClassVar[ManipulatorMethod]
    MANIPULATOR_METHOD_MOVE_JOINT_TRAJECTORY: _ClassVar[ManipulatorMethod]
    MANIPULATOR_METHOD_GO_HOME: _ClassVar[ManipulatorMethod]
    MANIPULATOR_METHOD_SET_GRIPPER: _ClassVar[ManipulatorMethod]
    MANIPULATOR_METHOD_STOP: _ClassVar[ManipulatorMethod]

class ArmReadiness(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    ARM_READINESS_UNSPECIFIED: _ClassVar[ArmReadiness]
    ARM_READINESS_READY: _ClassVar[ArmReadiness]
    ARM_READINESS_NOT_READY: _ClassVar[ArmReadiness]
    ARM_READINESS_TRANSITIONING: _ClassVar[ArmReadiness]
    ARM_READINESS_FAULT: _ClassVar[ArmReadiness]

class ManipulatorReason(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    MANIPULATOR_REASON_UNSPECIFIED: _ClassVar[ManipulatorReason]
    MANIPULATOR_REASON_NOT_READY: _ClassVar[ManipulatorReason]
    MANIPULATOR_REASON_SAFETY_LATCHED: _ClassVar[ManipulatorReason]
    MANIPULATOR_REASON_STALE_EPOCH: _ClassVar[ManipulatorReason]
    MANIPULATOR_REASON_CONTROL_NOT_HELD: _ClassVar[ManipulatorReason]
    MANIPULATOR_REASON_JOINT_LIMIT: _ClassVar[ManipulatorReason]
    MANIPULATOR_REASON_UNREACHABLE_POSE: _ClassVar[ManipulatorReason]
    MANIPULATOR_REASON_COLLISION: _ClassVar[ManipulatorReason]
    MANIPULATOR_REASON_RESOURCE_UNAVAILABLE: _ClassVar[ManipulatorReason]
    MANIPULATOR_REASON_RESOURCE_FAULT: _ClassVar[ManipulatorReason]
MANIPULATOR_METHOD_UNSPECIFIED: ManipulatorMethod
MANIPULATOR_METHOD_GET_ARM_STATE: ManipulatorMethod
MANIPULATOR_METHOD_SUBSCRIBE_ARM_STATE: ManipulatorMethod
MANIPULATOR_METHOD_MOVE_TO_POSE: ManipulatorMethod
MANIPULATOR_METHOD_MOVE_JOINT_TRAJECTORY: ManipulatorMethod
MANIPULATOR_METHOD_GO_HOME: ManipulatorMethod
MANIPULATOR_METHOD_SET_GRIPPER: ManipulatorMethod
MANIPULATOR_METHOD_STOP: ManipulatorMethod
ARM_READINESS_UNSPECIFIED: ArmReadiness
ARM_READINESS_READY: ArmReadiness
ARM_READINESS_NOT_READY: ArmReadiness
ARM_READINESS_TRANSITIONING: ArmReadiness
ARM_READINESS_FAULT: ArmReadiness
MANIPULATOR_REASON_UNSPECIFIED: ManipulatorReason
MANIPULATOR_REASON_NOT_READY: ManipulatorReason
MANIPULATOR_REASON_SAFETY_LATCHED: ManipulatorReason
MANIPULATOR_REASON_STALE_EPOCH: ManipulatorReason
MANIPULATOR_REASON_CONTROL_NOT_HELD: ManipulatorReason
MANIPULATOR_REASON_JOINT_LIMIT: ManipulatorReason
MANIPULATOR_REASON_UNREACHABLE_POSE: ManipulatorReason
MANIPULATOR_REASON_COLLISION: ManipulatorReason
MANIPULATOR_REASON_RESOURCE_UNAVAILABLE: ManipulatorReason
MANIPULATOR_REASON_RESOURCE_FAULT: ManipulatorReason

class JointState(_message.Message):
    __slots__ = ("position_rad", "velocity_rad_s")
    POSITION_RAD_FIELD_NUMBER: _ClassVar[int]
    VELOCITY_RAD_S_FIELD_NUMBER: _ClassVar[int]
    position_rad: _containers.RepeatedScalarFieldContainer[float]
    velocity_rad_s: _containers.RepeatedScalarFieldContainer[float]
    def __init__(self, position_rad: _Optional[_Iterable[float]] = ..., velocity_rad_s: _Optional[_Iterable[float]] = ...) -> None: ...

class Pose(_message.Message):
    __slots__ = ("frame", "x", "y", "z", "qx", "qy", "qz", "qw")
    FRAME_FIELD_NUMBER: _ClassVar[int]
    X_FIELD_NUMBER: _ClassVar[int]
    Y_FIELD_NUMBER: _ClassVar[int]
    Z_FIELD_NUMBER: _ClassVar[int]
    QX_FIELD_NUMBER: _ClassVar[int]
    QY_FIELD_NUMBER: _ClassVar[int]
    QZ_FIELD_NUMBER: _ClassVar[int]
    QW_FIELD_NUMBER: _ClassVar[int]
    frame: str
    x: float
    y: float
    z: float
    qx: float
    qy: float
    qz: float
    qw: float
    def __init__(self, frame: _Optional[str] = ..., x: _Optional[float] = ..., y: _Optional[float] = ..., z: _Optional[float] = ..., qx: _Optional[float] = ..., qy: _Optional[float] = ..., qz: _Optional[float] = ..., qw: _Optional[float] = ...) -> None: ...

class ArmState(_message.Message):
    __slots__ = ("readiness", "joints", "tcp_pose", "joint_lower_rad", "joint_upper_rad", "active_motion_epoch")
    READINESS_FIELD_NUMBER: _ClassVar[int]
    JOINTS_FIELD_NUMBER: _ClassVar[int]
    TCP_POSE_FIELD_NUMBER: _ClassVar[int]
    JOINT_LOWER_RAD_FIELD_NUMBER: _ClassVar[int]
    JOINT_UPPER_RAD_FIELD_NUMBER: _ClassVar[int]
    ACTIVE_MOTION_EPOCH_FIELD_NUMBER: _ClassVar[int]
    readiness: ArmReadiness
    joints: JointState
    tcp_pose: Pose
    joint_lower_rad: _containers.RepeatedScalarFieldContainer[float]
    joint_upper_rad: _containers.RepeatedScalarFieldContainer[float]
    active_motion_epoch: int
    def __init__(self, readiness: _Optional[_Union[ArmReadiness, str]] = ..., joints: _Optional[_Union[JointState, _Mapping]] = ..., tcp_pose: _Optional[_Union[Pose, _Mapping]] = ..., joint_lower_rad: _Optional[_Iterable[float]] = ..., joint_upper_rad: _Optional[_Iterable[float]] = ..., active_motion_epoch: _Optional[int] = ...) -> None: ...

class GetArmStateRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class SubscribeArmStateRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class MoveToPoseRequest(_message.Message):
    __slots__ = ("target", "max_vel_scale")
    TARGET_FIELD_NUMBER: _ClassVar[int]
    MAX_VEL_SCALE_FIELD_NUMBER: _ClassVar[int]
    target: Pose
    max_vel_scale: float
    def __init__(self, target: _Optional[_Union[Pose, _Mapping]] = ..., max_vel_scale: _Optional[float] = ...) -> None: ...

class MoveJointTrajectoryPoint(_message.Message):
    __slots__ = ("position_rad", "time_from_start_nanos")
    POSITION_RAD_FIELD_NUMBER: _ClassVar[int]
    TIME_FROM_START_NANOS_FIELD_NUMBER: _ClassVar[int]
    position_rad: _containers.RepeatedScalarFieldContainer[float]
    time_from_start_nanos: int
    def __init__(self, position_rad: _Optional[_Iterable[float]] = ..., time_from_start_nanos: _Optional[int] = ...) -> None: ...

class MoveJointTrajectoryRequest(_message.Message):
    __slots__ = ("points",)
    POINTS_FIELD_NUMBER: _ClassVar[int]
    points: _containers.RepeatedCompositeFieldContainer[MoveJointTrajectoryPoint]
    def __init__(self, points: _Optional[_Iterable[_Union[MoveJointTrajectoryPoint, _Mapping]]] = ...) -> None: ...

class GoHomeRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class SetGripperRequest(_message.Message):
    __slots__ = ("opening", "max_effort")
    OPENING_FIELD_NUMBER: _ClassVar[int]
    MAX_EFFORT_FIELD_NUMBER: _ClassVar[int]
    opening: float
    max_effort: float
    def __init__(self, opening: _Optional[float] = ..., max_effort: _Optional[float] = ...) -> None: ...

class StopRequest(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ManipulatorMoveResult(_message.Message):
    __slots__ = ("final_pose",)
    FINAL_POSE_FIELD_NUMBER: _ClassVar[int]
    final_pose: Pose
    def __init__(self, final_pose: _Optional[_Union[Pose, _Mapping]] = ...) -> None: ...

class ManipulatorErrorDetail(_message.Message):
    __slots__ = ("reason",)
    REASON_FIELD_NUMBER: _ClassVar[int]
    reason: ManipulatorReason
    def __init__(self, reason: _Optional[_Union[ManipulatorReason, str]] = ...) -> None: ...
