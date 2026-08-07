package sdk

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync/atomic"
	"unsafe"

	ipcv1 "github.com/nervus-os/nervus-ipc/protocol/ipcv1"
)

// SHARED_MEMORY_RING 的用户态实现。ABI 定义见 transfer.proto 的 TransferRingConfig。
//
// # 本文件的核心是「不信共享内存里的任何数字」
//
// 环内存两端都能写。若消费者从头部读 slot_count / slot_size 再据此算偏移，
// 一个恶意或有 bug 的生产者只要改写头部就能让对端越界读——而那发生在对端自己的
// 地址空间里，没有任何东西能拦。
//
// 因此几何参数一律取自控制面下发的 TransferRingConfig（经 nervud、不可被对端
// 篡改）；头部里那份只在附着时做一次自检，之后再不读。

const (
	ringMagic   = "NVR1"
	ringVersion = 1

	// RingHeaderBytes 是环头部长度，固定一页——让描述符区页对齐。
	RingHeaderBytes = 4096
	// RingDescriptorBytes 是每个 slot 描述符的长度。
	RingDescriptorBytes = 32

	// 头部字段偏移（小端）
	ringOffMagic           = 0
	ringOffVersion         = 4
	ringOffSlotCount       = 8
	ringOffSlotSize        = 12
	ringOffDescriptorBytes = 16
	ringOffProducerCursor  = 24
	ringOffConsumerCursor  = 32

	// 描述符字段偏移（小端）
	ringDescOffSequence  = 0
	ringDescOffLength    = 8
	ringDescOffFlags     = 12
	ringDescOffTimestamp = 16
)

var (
	// ErrRingGeometry 表示共享内存头部与控制面下发的几何参数不符。
	// 这是自检点，不是可协商的参数——不符即协议违规。
	ErrRingGeometry = errors.New("sdk: shared memory ring geometry does not match the control plane")
	// ErrRingCorrupt 表示对端写出了本 ABI 不允许的值（越界长度、不可能的游标）。
	ErrRingCorrupt = errors.New("sdk: shared memory ring contains an invalid value")
	// ErrRingLapped 表示消费者被生产者套圈，中间的帧已被覆盖。
	ErrRingLapped = errors.New("sdk: consumer was lapped by the producer")
	// ErrRingEmpty 表示当前没有可读的 slot。
	ErrRingEmpty = errors.New("sdk: ring is empty")
	// ErrRingFull 表示环已满，生产者必须等消费者推进。
	ErrRingFull = errors.New("sdk: ring is full")
)

// RingGeometry 是经过校验的环几何参数，全部取自控制面。
type RingGeometry struct {
	SlotCount       uint32
	SlotSize        uint32
	HeaderBytes     uint32
	DescriptorBytes uint32
}

// ValidateRingConfig 校验控制面下发的几何参数本身是否自洽。
//
// 在映射任何内存【之前】做：一个 slot_count=0 或非 2 的幂的配置会让后续所有
// 掩码取模失效，而那种错误表现为随机读到错误的 slot，极难定位。
func ValidateRingConfig(cfg *ipcv1.TransferRingConfig) (RingGeometry, error) {
	if cfg == nil {
		return RingGeometry{}, fmt.Errorf("%w: nil ring config", ErrRingGeometry)
	}
	g := RingGeometry{
		SlotCount:       cfg.GetSlotCount(),
		SlotSize:        cfg.GetSlotSize(),
		HeaderBytes:     cfg.GetHeaderBytes(),
		DescriptorBytes: cfg.GetDescriptorBytes(),
	}
	if g.SlotCount == 0 || g.SlotCount&(g.SlotCount-1) != 0 {
		return RingGeometry{}, fmt.Errorf("%w: slot_count %d is not a power of two",
			ErrRingGeometry, g.SlotCount)
	}
	if g.SlotSize == 0 {
		return RingGeometry{}, fmt.Errorf("%w: slot_size is zero", ErrRingGeometry)
	}
	if g.HeaderBytes != RingHeaderBytes {
		return RingGeometry{}, fmt.Errorf("%w: header_bytes %d, want %d",
			ErrRingGeometry, g.HeaderBytes, RingHeaderBytes)
	}
	if g.DescriptorBytes != RingDescriptorBytes {
		return RingGeometry{}, fmt.Errorf("%w: descriptor_bytes %d, want %d",
			ErrRingGeometry, g.DescriptorBytes, RingDescriptorBytes)
	}
	if _, err := g.TotalBytes(); err != nil {
		return RingGeometry{}, err
	}
	return g, nil
}

// TotalBytes 是环内存的总长度，必须与 memfd 实际大小完全相等。
//
// 逐步检查溢出而不是算完再看：在 32 位上 slot_count*slot_size 可以静默回绕成
// 一个很小的数，那样映射会成功、边界检查会通过、而读写落在别处。
func (g RingGeometry) TotalBytes() (uint64, error) {
	payload := uint64(g.SlotCount) * uint64(g.SlotSize)
	descriptors := uint64(g.SlotCount) * uint64(g.DescriptorBytes)
	total := uint64(g.HeaderBytes) + descriptors + payload
	if total < uint64(g.HeaderBytes) || total > 1<<40 {
		return 0, fmt.Errorf("%w: ring size overflow", ErrRingGeometry)
	}
	return total, nil
}

func (g RingGeometry) descriptorOffset(slot uint32) uint64 {
	return uint64(g.HeaderBytes) + uint64(slot)*uint64(g.DescriptorBytes)
}

func (g RingGeometry) payloadOffset(slot uint32) uint64 {
	return uint64(g.HeaderBytes) +
		uint64(g.SlotCount)*uint64(g.DescriptorBytes) +
		uint64(slot)*uint64(g.SlotSize)
}

// Ring 是映射好的共享内存环的一侧视图。
//
// 【不持有 fd】：fd 的生命周期由 TransferConn 管理；Ring 只操作已经 mmap 出来
// 的那段内存加一份可信几何参数。
type Ring struct {
	mem      []byte
	geometry RingGeometry
	producer bool
}

// NewRing 在一段已映射内存上建立环视图，并对头部做一次自检。
//
// mem 必须恰好是 geometry.TotalBytes() 长。producer 为 true 时本端是生产者
// （写 producer_cursor、写载荷），否则是消费者。
func NewRing(mem []byte, geometry RingGeometry, producer bool) (*Ring, error) {
	total, err := geometry.TotalBytes()
	if err != nil {
		return nil, err
	}
	if uint64(len(mem)) != total {
		return nil, fmt.Errorf("%w: mapped %d bytes, geometry needs %d",
			ErrRingGeometry, len(mem), total)
	}
	r := &Ring{mem: mem, geometry: geometry, producer: producer}
	if err := r.verifyHeader(); err != nil {
		return nil, err
	}
	return r, nil
}

// verifyHeader 把头部里的几何副本与控制面下发的那份比对。
//
// 【这是自检，不是取值】。对不上说明实现有 bug 或内存被踩，两种都必须立刻停，
// 而不是采信其中一方继续跑。
func (r *Ring) verifyHeader() error {
	if string(r.mem[ringOffMagic:ringOffMagic+4]) != ringMagic {
		return fmt.Errorf("%w: bad magic", ErrRingGeometry)
	}
	if v := binary.LittleEndian.Uint32(r.mem[ringOffVersion:]); v != ringVersion {
		return fmt.Errorf("%w: version %d, want %d", ErrRingGeometry, v, ringVersion)
	}
	for _, check := range []struct {
		name   string
		offset int
		want   uint32
	}{
		{"slot_count", ringOffSlotCount, r.geometry.SlotCount},
		{"slot_size", ringOffSlotSize, r.geometry.SlotSize},
		{"descriptor_bytes", ringOffDescriptorBytes, r.geometry.DescriptorBytes},
	} {
		if got := binary.LittleEndian.Uint32(r.mem[check.offset:]); got != check.want {
			return fmt.Errorf("%w: header %s = %d, control plane says %d",
				ErrRingGeometry, check.name, got, check.want)
		}
	}
	return nil
}

// InitHeader 由创建方（nervud）在分发 fd 之前写入头部。
func InitHeader(mem []byte, geometry RingGeometry) error {
	total, err := geometry.TotalBytes()
	if err != nil {
		return err
	}
	if uint64(len(mem)) != total {
		return fmt.Errorf("%w: mapped %d bytes, geometry needs %d",
			ErrRingGeometry, len(mem), total)
	}
	copy(mem[ringOffMagic:], ringMagic)
	binary.LittleEndian.PutUint32(mem[ringOffVersion:], ringVersion)
	binary.LittleEndian.PutUint32(mem[ringOffSlotCount:], geometry.SlotCount)
	binary.LittleEndian.PutUint32(mem[ringOffSlotSize:], geometry.SlotSize)
	binary.LittleEndian.PutUint32(mem[ringOffDescriptorBytes:], geometry.DescriptorBytes)
	binary.LittleEndian.PutUint64(mem[ringOffProducerCursor:], 0)
	binary.LittleEndian.PutUint64(mem[ringOffConsumerCursor:], 0)
	return nil
}

func (r *Ring) cursorPtr(offset int) *uint64 {
	return (*uint64)(unsafe.Pointer(&r.mem[offset]))
}

func (r *Ring) loadProducerCursor() uint64 {
	return atomic.LoadUint64(r.cursorPtr(ringOffProducerCursor))
}

func (r *Ring) loadConsumerCursor() uint64 {
	return atomic.LoadUint64(r.cursorPtr(ringOffConsumerCursor))
}

// Frame 是一次读取的结果。Payload 指向环内存，【下一次 Advance 之后即失效】——
// 需要留存必须自己复制。这是零拷贝的代价，也是它的全部意义。
type Frame struct {
	Payload   []byte
	Sequence  uint64
	Flags     uint32
	Timestamp uint64
}

// Write 把一帧写进环。生产者专用。
//
// 顺序是本 ABI 的关键：先写载荷与元数据，最后以 release 语义写 sequence。
// 反过来做的话消费者可能读到一个「已就绪」但载荷还没写完的 slot。
func (r *Ring) Write(payload []byte, flags uint32, timestampNanos uint64) error {
	if !r.producer {
		return errors.New("sdk: Write on a consumer ring")
	}
	if uint32(len(payload)) > r.geometry.SlotSize {
		return fmt.Errorf("%w: payload %d exceeds slot size %d",
			ErrRingCorrupt, len(payload), r.geometry.SlotSize)
	}

	produced := r.loadProducerCursor()
	consumed := r.loadConsumerCursor()
	// 对端游标不可信：消费者写了个未来值时 produced-consumed 会回绕成一个巨大的
	// 数。这里只在「看起来合法且已满」时拒绝，其余情况按满处理更安全
	if produced-consumed >= uint64(r.geometry.SlotCount) {
		return ErrRingFull
	}

	slot := uint32(produced & uint64(r.geometry.SlotCount-1))
	desc := r.mem[r.geometry.descriptorOffset(slot):]

	// 奇数 = 正在写入。消费者读到奇数即跳过这个 slot
	seq := produced*2 + 1
	atomic.StoreUint64((*uint64)(unsafe.Pointer(&desc[ringDescOffSequence])), seq)

	payloadStart := r.geometry.payloadOffset(slot)
	copy(r.mem[payloadStart:payloadStart+uint64(r.geometry.SlotSize)], payload)
	binary.LittleEndian.PutUint32(desc[ringDescOffLength:], uint32(len(payload)))
	binary.LittleEndian.PutUint32(desc[ringDescOffFlags:], flags)
	binary.LittleEndian.PutUint64(desc[ringDescOffTimestamp:], timestampNanos)

	// release：偶数 = 就绪。必须在载荷与元数据全部写完之后
	atomic.StoreUint64((*uint64)(unsafe.Pointer(&desc[ringDescOffSequence])), seq+1)
	atomic.StoreUint64(r.cursorPtr(ringOffProducerCursor), produced+1)
	return nil
}

// Read 取出下一帧。消费者专用。
//
// 返回 ErrRingEmpty 表示暂无数据；ErrRingLapped 表示被套圈，调用方应当
// SkipToLatest 之后重试——中间那些帧已经被覆盖，读它们只会得到撕裂的内容。
func (r *Ring) Read() (Frame, error) {
	if r.producer {
		return Frame{}, errors.New("sdk: Read on a producer ring")
	}
	consumed := r.loadConsumerCursor()
	produced := r.loadProducerCursor()
	if produced == consumed {
		return Frame{}, ErrRingEmpty
	}

	// 【对端游标不可信】。produced 落后于 consumed 说明生产者写了个非法值
	available := produced - consumed
	if available > uint64(r.geometry.SlotCount) {
		return Frame{}, ErrRingLapped
	}

	slot := uint32(consumed & uint64(r.geometry.SlotCount-1))
	desc := r.mem[r.geometry.descriptorOffset(slot):]

	// seqlock：写入前置奇（正在写），写完置偶（就绪）。
	// 帧号 N 的就绪值是 2N+2，因此 0 表示这个 slot 从未被写过。
	seq := atomic.LoadUint64((*uint64)(unsafe.Pointer(&desc[ringDescOffSequence])))
	if seq == 0 || seq%2 != 0 {
		return Frame{}, ErrRingEmpty
	}

	length := binary.LittleEndian.Uint32(desc[ringDescOffLength:])
	// 【必须自己校验，且不 clamp】。clamp 后继续读会把一个 bug 变成静默的
	// 数据损坏——调用方拿到一段长度对了但内容不对的数据，比拿到错误难查得多
	if length > r.geometry.SlotSize {
		return Frame{}, fmt.Errorf("%w: slot %d declares length %d > slot size %d",
			ErrRingCorrupt, slot, length, r.geometry.SlotSize)
	}

	payloadStart := r.geometry.payloadOffset(slot)
	frame := Frame{
		Payload: r.mem[payloadStart : payloadStart+uint64(length)],
		// 就绪值 2N+2 → 帧号 N
		Sequence:  seq/2 - 1,
		Flags:     binary.LittleEndian.Uint32(desc[ringDescOffFlags:]),
		Timestamp: binary.LittleEndian.Uint64(desc[ringDescOffTimestamp:]),
	}

	// 读完之后复核 sequence：中途被生产者覆盖的话两次读到的值不同，
	// 说明拿到的是撕裂内容
	if after := atomic.LoadUint64(
		(*uint64)(unsafe.Pointer(&desc[ringDescOffSequence]))); after != seq {
		return Frame{}, ErrRingLapped
	}
	return frame, nil
}

// Advance 确认上一帧已消费完毕，推进消费者游标。
//
// 与 Read 分开是刻意的：Frame.Payload 指向环内存，推进之后生产者随时可能覆盖它。
// 调用方因此可以先把数据用完（或复制走）再推进。
func (r *Ring) Advance() {
	if r.producer {
		return
	}
	atomic.StoreUint64(r.cursorPtr(ringOffConsumerCursor), r.loadConsumerCursor()+1)
}

// SkipToLatest 在被套圈之后把消费者游标跳到最新一圈的起点。
//
// 视频流的正确行为是丢掉积压看最新的一帧，而不是从一批已被覆盖的 slot 里
// 读出撕裂的内容。
func (r *Ring) SkipToLatest() {
	if r.producer {
		return
	}
	produced := r.loadProducerCursor()
	target := produced
	if produced > uint64(r.geometry.SlotCount) {
		target = produced - uint64(r.geometry.SlotCount) + 1
	}
	atomic.StoreUint64(r.cursorPtr(ringOffConsumerCursor), target)
}
