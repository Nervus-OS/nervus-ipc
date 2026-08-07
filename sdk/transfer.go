//go:build linux

// 通用高速数据面的客户端。
// 它连的是 nervud 的 /run/nervus/nervud-transfer.sock
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

	// 以下仅在 mode == SHARED_MEMORY_RING 时有效。
	//
	// ring 模式下 nervud 不在数据路径上：两端 mmap 同一块内存直接收发，
	// co 那条 socket 只用来握手、收 fd、以及感知对端断开。
	ring        *Ring
	ringMem     []byte
	ringMemFD   int
	ringEventFD int
}

// Ring 返回共享内存环视图。仅在 mode == SHARED_MEMORY_RING 时非 nil。
//
// 【Frame.Payload 指向环内存，Advance 之后即失效】——需要留存必须自己复制。
// 这是零拷贝的代价，也是它的全部意义。
func (c *TransferConn) Ring() *Ring { return c.ring }

// WriteRing 写一帧并唤醒对端。生产者专用。
func (c *TransferConn) WriteRing(payload []byte, flags uint32, timestampNanos uint64) error {
	if c.ring == nil {
		return fmt.Errorf("%w: transfer is not in shared-memory ring mode", ErrProtocol)
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.ring.Write(payload, flags, timestampNanos); err != nil {
		return err
	}
	return c.notifyRing()
}

// ReadRing 取一帧。没有数据时按 timeoutMillis 等待对端通知（负数无限等）。
//
// 返回 ErrRingEmpty 表示等到超时仍无数据。取到 ErrRingLapped 时调用方应当
// SkipToLatest 之后重试——视频流的正确行为是丢积压看最新帧，而不是从一批已被
// 覆盖的 slot 里读撕裂内容。
func (c *TransferConn) ReadRing(timeoutMillis int) (Frame, error) {
	if c.ring == nil {
		return Frame{}, fmt.Errorf("%w: transfer is not in shared-memory ring mode", ErrProtocol)
	}
	c.readMu.Lock()
	defer c.readMu.Unlock()
	frame, err := c.ring.Read()
	if err == nil || !errors.Is(err, ErrRingEmpty) {
		return frame, err
	}
	if err := c.waitRing(timeoutMillis); err != nil {
		return Frame{}, err
	}
	return c.ring.Read()
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
	switch success.GetMode() {
	case ipcv1.TransferMode_TRANSFER_MODE_FRAMED_RELAY,
		ipcv1.TransferMode_TRANSFER_MODE_SHARED_MEMORY_RING:
	default:
		return fail(fmt.Errorf("%w: transfer mode %s is not implemented by Go SDK",
			ErrProtocol, success.GetMode()))
	}
	if success.GetMaxPacketBytes() == 0 || success.GetMaxBytesPerSecond() == 0 {
		return fail(fmt.Errorf("%w: transfer limits must be non-zero", ErrProtocol))
	}

	conn := &TransferConn{
		co:                raw,
		role:              handle.GetRole(),
		mode:              success.GetMode(),
		maxPacketBytes:    success.GetMaxPacketBytes(),
		maxBytesPerSecond: success.GetMaxBytesPerSecond(),
	}

	// ring 模式：结果帧之后 nervud 会用 SCM_RIGHTS 送 memfd 与 eventfd。
	// 【在清除 deadline 之前收】——收 fd 仍属握手，超时必须仍然生效。
	if success.GetMode() == ipcv1.TransferMode_TRANSFER_MODE_SHARED_MEMORY_RING {
		if err := conn.attachRing(raw, success.GetRing()); err != nil {
			return fail(err)
		}
	}

	if !stopContext() {
		return fail(ctx.Err())
	}
	if err := raw.SetDeadline(time.Time{}); err != nil {
		return fail(fmt.Errorf("sdk: clear transfer handshake deadline: %w", err))
	}
	return conn, nil
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
	switch handle.GetMode() {
	case ipcv1.TransferMode_TRANSFER_MODE_FRAMED_RELAY,
		ipcv1.TransferMode_TRANSFER_MODE_SHARED_MEMORY_RING:
	default:
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

// Close 关闭数据面连接，并在 ring 模式下解除映射、关掉两个 fd。
//
// 顺序是先解映射再关 socket：反过来的话对端会先看到断开、再有一小段时间
// 我们仍持有内存映射，那段窗口里的行为不好推理。
func (c *TransferConn) Close() error {
	c.closeRing()
	return c.co.Close()
}

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
