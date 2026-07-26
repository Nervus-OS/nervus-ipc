// Package sdk 是 Nervus OS 控制面 IPC v1 的 Go SDK：Client（消费侧）与
// ServiceHost（提供侧）。
//
// 使用者是 Go 写的系统服务与 OEM Provider（nervus-system-server 等）。nervud
// 自己【不用】本包——它是服务端，在 internal/ipc 里有自己的实现；本包是客户端。
//
// # 红线：绝不手搓信封
//
// 本包复用 protocol/ipcv1 的生成类型编解码 Envelope，不自己拼字节。与 Kotlin
// SDK（nervus-app-sdk）同一做法。手搓的代价不是麻烦，是「schema 改了而某一侧
// 没跟上」时两边都还能跑、只在某个字段的行为上不一致——这类漂移极难发现。
//
// # 本包【不】实现哪些 body
//
// nervud 目前不支持 Cancel(32)、Subscribe 组(40-45)、ControlLease 组(70-73)，
// 收到即关闭连接并审计为 UnsupportedBody。本包因此【刻意不提供】这些 API：
// 提供一个「编译得过、一调就断连」的方法比不提供更糟。
//
// 权威依据是仓库根 README 的「实现状态」表。内核接通某一项后，先更新那张表，
// 再在这里补 API。
//
// # 分帧
//
// 本文件是分帧层，与 nervud 的 internal/ipc/frame.go 逐字节对应：
//
//	4 字节大端 uint32 N + N 字节 Protobuf Envelope
//
// N 不含前缀自身；N == 0 与 N > 128 KiB 都是协议错误。
package sdk

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// MaxFrameBytes 是控制面 Frame 的绝对硬上限：128 KiB = 131072 字节。
//
// 必须与 nervud 的 ipc.MaxFrameBytes 以及握手时下发的
// ConnectionLimits.max_frame_bytes 一致。这是 Frame 上限，不是方法额度——
// 逐方法额度更小，由上层取最小值。
const MaxFrameBytes = 128 << 10

const lengthPrefixBytes = 4

// 分帧层的错误【全部】是协议违规：出现即必须关闭连接。
//
// 长度字段一旦不可信，后续 Frame 的边界就无法确定，这条连接上的字节流已经
// 失去意义——连「回一条错误」都做不到，因为错误响应自己也要靠长度前缀被读到。
var (
	// ErrZeroLength 长度前缀为 0。空 Envelope 没有任何合法用途。
	ErrZeroLength = errors.New("sdk: frame length is zero")
	// ErrFrameTooLarge 长度前缀超过 MaxFrameBytes。
	ErrFrameTooLarge = errors.New("sdk: frame length exceeds hard limit")
)

// readFrameHeader 读满 4 字节长度前缀并校验，返回正文长度 N。
//
// 与 readFrameBody 分成两个函数不是为了好看：中间那一刻是设置【正文 deadline】
// 的唯一时机。空闲 deadline（等下一个 Frame）可以很长，正文 deadline（已宣称
// 有 N 字节在路上）必须很短。用一个 deadline 覆盖两段，等于把空闲容忍度送给
// slowloris——发完 4 字节长度后每秒挤一个字节就能长期占住连接。
func readFrameHeader(r io.Reader) (uint32, error) {
	var hdr [lengthPrefixBytes]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, err // 含 io.EOF：对端正常关闭，调用方据此区分正常断开
	}

	n := binary.BigEndian.Uint32(hdr[:])
	switch {
	case n == 0:
		return 0, ErrZeroLength
	case n > MaxFrameBytes:
		// 立即返回，不分配、也不排空对端自称的正文——那正是攻击者想要的
		// 免费带宽。此刻正文一个字节都还没碰过。
		return 0, fmt.Errorf("%w: %d > %d", ErrFrameTooLarge, n, MaxFrameBytes)
	}
	return n, nil
}

// readFrameBody 读满 n 字节正文到 buf，返回 buf[:n]。
//
// buf 由调用方按连接复用，因此稳态下零堆分配。n 必须来自同一连接上刚刚成功
// 返回的 readFrameHeader。
//
// 必须 ReadFull：UDS 是字节流，一次 read 可能只带回半个 Frame，也可能一次带回
// 好几个——read 的返回边界不是消息边界。
func readFrameBody(r io.Reader, buf []byte, n uint32) ([]byte, error) {
	if int(n) > len(buf) {
		// 本端装配错误（buf 小于硬上限），不是对端的错
		return nil, fmt.Errorf("sdk: read buffer too small: need %d, have %d", n, len(buf))
	}
	if _, err := io.ReadFull(r, buf[:n]); err != nil {
		// ReadFull 读到 0 字节返回 EOF，读到一部分返回 ErrUnexpectedEOF
		return nil, err
	}
	return buf[:n], nil
}

// writeFrame 写出长度前缀 + 正文。
//
// 【调用约束】同一条连接上不得并发调用。本函数不加锁：两个 goroutine 并发写
// 同一个 stream，哪怕各自一次写满，两个 Frame 的字节也会交错，接收方从此再也
// 找不回边界。串行化是连接层的职责（见 conn.writeEnvelope 的 writeMu）。
//
// 返回错误时【必须关闭连接，不能重试本帧】：长度可能已经写出去而正文没有，
// 对端的解析器正卡在等待 N 字节，这条流无法恢复。
func writeFrame(w io.Writer, payload []byte) error {
	n := len(payload)
	switch {
	case n == 0:
		return ErrZeroLength // 本端的 bug：不要把违规帧发给对端
	case n > MaxFrameBytes:
		return fmt.Errorf("%w: %d > %d", ErrFrameTooLarge, n, MaxFrameBytes)
	}

	var hdr [lengthPrefixBytes]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(n))

	if _, err := w.Write(hdr[:]); err != nil {
		return fmt.Errorf("sdk: write frame header: %w", err)
	}
	if _, err := w.Write(payload); err != nil {
		return fmt.Errorf("sdk: write frame body: %w", err)
	}
	return nil
}
