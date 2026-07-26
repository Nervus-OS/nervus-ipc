package sdk

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	ipcv1 "github.com/nervus-os/nervus-ipc/protocol/ipcv1"
)

const (
	// DefaultTransferSockPath 是生产镜像中的通用 Transfer 数据面 UDS。
	DefaultTransferSockPath = "/run/nervus/nervud-transfer.sock"

	transferFrameHeaderBytes = 28
)

var transferFrameMagic = [4]byte{'N', 'V', 'T', '1'}

// TransferFrame 是 FRAMED_RELAY 模式的一帧。当前版本没有定义任何 flags，
// 因此 Flags 必须为 0；未来增加 flag 时必须先冻结其 wire 语义。
type TransferFrame struct {
	Flags                   uint32
	Sequence                uint64
	MonotonicTimestampNanos uint64
	Payload                 []byte
}

// TransferConn 是已经完成 ticket 附着的 FRAMED_RELAY 连接。
type TransferConn struct {
	co                net.Conn
	role              ipcv1.TransferRole
	mode              ipcv1.TransferMode
	maxPacketBytes    uint32
	maxBytesPerSecond uint64
	readMu, writeMu   sync.Mutex
}

// AttachTransfer 使用 handle 中的一次性 ticket 附着通用数据面。
//
// 本 SDK 当前只实现协议的基线 FRAMED_RELAY；SHARED_MEMORY_RING 需要先冻结
// memfd/eventfd ring ABI，不能在 SDK 里自行猜测。
func AttachTransfer(ctx context.Context, handle *ipcv1.TransferHandle) (*TransferConn, error) {
	if err := validateTransferHandle(handle); err != nil {
		return nil, err
	}

	var dialer net.Dialer
	raw, err := dialer.DialContext(ctx, "unix", handle.GetDataPlaneEndpoint())
	if err != nil {
		return nil, fmt.Errorf("sdk: dial transfer data plane: %w", err)
	}
	stopContext := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stopContext()
	if deadline, ok := ctx.Deadline(); ok {
		_ = raw.SetDeadline(deadline)
	}
	fail := func(err error) (*TransferConn, error) {
		_ = raw.Close()
		return nil, err
	}

	attachWire, err := proto.Marshal(&ipcv1.AttachTransfer{
		TransferId:   handle.GetTransferId(),
		AttachTicket: handle.GetAttachTicket(),
		Role:         handle.GetRole(),
	})
	if err != nil {
		return fail(fmt.Errorf("sdk: marshal AttachTransfer: %w", err))
	}
	if err := writeFrame(raw, attachWire); err != nil {
		return fail(fmt.Errorf("sdk: send AttachTransfer: %w", err))
	}

	n, err := readFrameHeader(raw)
	if err != nil {
		return fail(fmt.Errorf("sdk: read AttachTransferResult header: %w", err))
	}
	buf := make([]byte, n)
	resultWire, err := readFrameBody(raw, buf, n)
	if err != nil {
		return fail(fmt.Errorf("sdk: read AttachTransferResult: %w", err))
	}
	var result ipcv1.AttachTransferResult
	if err := proto.Unmarshal(resultWire, &result); err != nil {
		return fail(fmt.Errorf("%w: malformed AttachTransferResult: %v", ErrProtocol, err))
	}
	if failure := result.GetFailure(); failure != nil {
		return fail(statusErrorFrom(failure))
	}
	success := result.GetSuccess()
	if success == nil {
		return fail(fmt.Errorf("%w: AttachTransferResult has neither outcome", ErrProtocol))
	}
	if success.GetMode() != handle.GetMode() {
		return fail(fmt.Errorf("%w: transfer mode changed from %s to %s",
			ErrProtocol, handle.GetMode(), success.GetMode()))
	}
	if success.GetMode() != ipcv1.TransferMode_TRANSFER_MODE_FRAMED_RELAY {
		return fail(fmt.Errorf("%w: transfer mode %s is not implemented by Go SDK",
			ErrProtocol, success.GetMode()))
	}
	if success.GetMaxPacketBytes() == 0 || success.GetMaxBytesPerSecond() == 0 {
		return fail(fmt.Errorf("%w: transfer limits must be non-zero", ErrProtocol))
	}
	if !stopContext() {
		return fail(ctx.Err())
	}
	if err := raw.SetDeadline(time.Time{}); err != nil {
		return fail(fmt.Errorf("sdk: clear transfer handshake deadline: %w", err))
	}

	return &TransferConn{
		co:                raw,
		role:              handle.GetRole(),
		mode:              success.GetMode(),
		maxPacketBytes:    success.GetMaxPacketBytes(),
		maxBytesPerSecond: success.GetMaxBytesPerSecond(),
	}, nil
}

func validateTransferHandle(handle *ipcv1.TransferHandle) error {
	if handle == nil {
		return errors.New("sdk: nil TransferHandle")
	}
	if len(handle.GetTransferId()) != 16 {
		return fmt.Errorf("sdk: transfer id has %d bytes, want 16", len(handle.GetTransferId()))
	}
	if len(handle.GetAttachTicket()) < 32 {
		return fmt.Errorf("sdk: attach ticket has %d bytes, want at least 32", len(handle.GetAttachTicket()))
	}
	switch handle.GetRole() {
	case ipcv1.TransferRole_TRANSFER_ROLE_PRODUCER,
		ipcv1.TransferRole_TRANSFER_ROLE_CONSUMER,
		ipcv1.TransferRole_TRANSFER_ROLE_PEER:
	default:
		return fmt.Errorf("sdk: invalid transfer role %d", handle.GetRole())
	}
	if handle.GetMode() != ipcv1.TransferMode_TRANSFER_MODE_FRAMED_RELAY {
		return fmt.Errorf("sdk: unsupported transfer mode %s", handle.GetMode())
	}
	if handle.GetExpiresAtMonotonicNanos() == 0 {
		return errors.New("sdk: transfer handle has zero expiry")
	}
	if handle.GetDataPlaneEndpoint() == "" {
		return errors.New("sdk: transfer handle has empty data plane endpoint")
	}
	return nil
}

// Role 返回本端在 Transfer 中的角色。
func (c *TransferConn) Role() ipcv1.TransferRole { return c.role }

// Mode 返回握手后生效的传输模式。
func (c *TransferConn) Mode() ipcv1.TransferMode { return c.mode }

// MaxPacketBytes 返回 nervud 收紧后的单帧 payload 上限。
func (c *TransferConn) MaxPacketBytes() uint32 { return c.maxPacketBytes }

// MaxBytesPerSecond 返回 nervud 收紧后的速率上限。
func (c *TransferConn) MaxBytesPerSecond() uint64 { return c.maxBytesPerSecond }

func (c *TransferConn) Close() error { return c.co.Close() }

func (c *TransferConn) SetDeadline(deadline time.Time) error {
	return c.co.SetDeadline(deadline)
}

func (c *TransferConn) SetReadDeadline(deadline time.Time) error {
	return c.co.SetReadDeadline(deadline)
}

func (c *TransferConn) SetWriteDeadline(deadline time.Time) error {
	return c.co.SetWriteDeadline(deadline)
}

// ReadFrame 读取一帧。纯 PRODUCER handle 不允许读。
func (c *TransferConn) ReadFrame() (TransferFrame, error) {
	if c.role == ipcv1.TransferRole_TRANSFER_ROLE_PRODUCER {
		return TransferFrame{}, errors.New("sdk: producer transfer handle cannot read")
	}
	c.readMu.Lock()
	defer c.readMu.Unlock()
	return readTransferFrame(c.co, c.maxPacketBytes)
}

// WriteFrame 写一帧。纯 CONSUMER handle 不允许写。
func (c *TransferConn) WriteFrame(frame TransferFrame) error {
	if c.role == ipcv1.TransferRole_TRANSFER_ROLE_CONSUMER {
		return errors.New("sdk: consumer transfer handle cannot write")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return writeTransferFrame(c.co, frame, c.maxPacketBytes)
}

func readTransferFrame(r io.Reader, maxPacketBytes uint32) (TransferFrame, error) {
	if maxPacketBytes == 0 {
		return TransferFrame{}, errors.New("sdk: zero transfer packet limit")
	}
	var header [transferFrameHeaderBytes]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return TransferFrame{}, err
	}
	if !bytes.Equal(header[0:4], transferFrameMagic[:]) {
		return TransferFrame{}, fmt.Errorf("%w: invalid transfer frame magic", ErrProtocol)
	}
	flags := binary.BigEndian.Uint32(header[4:8])
	if flags != 0 {
		return TransferFrame{}, fmt.Errorf("%w: unknown transfer frame flags %#x", ErrProtocol, flags)
	}
	payloadBytes := binary.BigEndian.Uint32(header[24:28])
	if payloadBytes > maxPacketBytes {
		return TransferFrame{}, fmt.Errorf("%w: transfer payload %d exceeds limit %d",
			ErrFrameTooLarge, payloadBytes, maxPacketBytes)
	}
	maxInt := uint64(^uint(0) >> 1)
	if uint64(payloadBytes) > maxInt {
		return TransferFrame{}, fmt.Errorf("%w: transfer payload does not fit platform int", ErrFrameTooLarge)
	}
	payload := make([]byte, int(payloadBytes))
	if _, err := io.ReadFull(r, payload); err != nil {
		return TransferFrame{}, err
	}
	return TransferFrame{
		Flags:                   flags,
		Sequence:                binary.BigEndian.Uint64(header[8:16]),
		MonotonicTimestampNanos: binary.BigEndian.Uint64(header[16:24]),
		Payload:                 payload,
	}, nil
}

func writeTransferFrame(w io.Writer, frame TransferFrame, maxPacketBytes uint32) error {
	if maxPacketBytes == 0 {
		return errors.New("sdk: zero transfer packet limit")
	}
	if frame.Flags != 0 {
		return fmt.Errorf("%w: unknown transfer frame flags %#x", ErrProtocol, frame.Flags)
	}
	if uint64(len(frame.Payload)) > uint64(maxPacketBytes) {
		return fmt.Errorf("%w: transfer payload %d exceeds limit %d",
			ErrFrameTooLarge, len(frame.Payload), maxPacketBytes)
	}
	var header [transferFrameHeaderBytes]byte
	copy(header[0:4], transferFrameMagic[:])
	binary.BigEndian.PutUint32(header[4:8], frame.Flags)
	binary.BigEndian.PutUint64(header[8:16], frame.Sequence)
	binary.BigEndian.PutUint64(header[16:24], frame.MonotonicTimestampNanos)
	binary.BigEndian.PutUint32(header[24:28], uint32(len(frame.Payload)))
	if err := writeAll(w, header[:]); err != nil {
		return err
	}
	return writeAll(w, frame.Payload)
}

func writeAll(w io.Writer, payload []byte) error {
	for len(payload) != 0 {
		n, err := w.Write(payload)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		payload = payload[n:]
	}
	return nil
}
