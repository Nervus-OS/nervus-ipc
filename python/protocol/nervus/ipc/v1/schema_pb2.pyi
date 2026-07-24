from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Iterable as _Iterable, Mapping as _Mapping, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class InterfaceSchemaBundle(_message.Message):
    __slots__ = ("interface_id", "version", "schema_hash", "file_descriptor_set")
    INTERFACE_ID_FIELD_NUMBER: _ClassVar[int]
    VERSION_FIELD_NUMBER: _ClassVar[int]
    SCHEMA_HASH_FIELD_NUMBER: _ClassVar[int]
    FILE_DESCRIPTOR_SET_FIELD_NUMBER: _ClassVar[int]
    interface_id: str
    version: int
    schema_hash: bytes
    file_descriptor_set: bytes
    def __init__(self, interface_id: _Optional[str] = ..., version: _Optional[int] = ..., schema_hash: _Optional[bytes] = ..., file_descriptor_set: _Optional[bytes] = ...) -> None: ...

class InterfaceSchemaBundleSet(_message.Message):
    __slots__ = ("bundles",)
    BUNDLES_FIELD_NUMBER: _ClassVar[int]
    bundles: _containers.RepeatedCompositeFieldContainer[InterfaceSchemaBundle]
    def __init__(self, bundles: _Optional[_Iterable[_Union[InterfaceSchemaBundle, _Mapping]]] = ...) -> None: ...
