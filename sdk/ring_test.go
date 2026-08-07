package sdk

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	ipcv1 "github.com/nervus-os/nervus-ipc/protocol/ipcv1"
)

func testGeometry() RingGeometry {
	return RingGeometry{
		SlotCount:       4,
		SlotSize:        64,
		HeaderBytes:     RingHeaderBytes,
		DescriptorBytes: RingDescriptorBytes,
	}
}

// newRingPair 造一段共享内存并在其上开出生产者与消费者两个视图，
// 模拟两个进程 mmap 同一个 memfd。
func newRingPair(t *testing.T) (producer, consumer *Ring, mem []byte) {
	t.Helper()
	g := testGeometry()
	total, err := g.TotalBytes()
	if err != nil {
		t.Fatalf("TotalBytes: %v", err)
	}
	mem = make([]byte, total)
	if err := InitHeader(mem, g); err != nil {
		t.Fatalf("InitHeader: %v", err)
	}
	if producer, err = NewRing(mem, g, true); err != nil {
		t.Fatalf("NewRing producer: %v", err)
	}
	if consumer, err = NewRing(mem, g, false); err != nil {
		t.Fatalf("NewRing consumer: %v", err)
	}
	return producer, consumer, mem
}

func TestRing_WriteReadRoundTrip(t *testing.T) {
	producer, consumer, _ := newRingPair(t)

	payload := []byte("frame-0")
	if err := producer.Write(payload, 0x11, 12345); err != nil {
		t.Fatalf("Write: %v", err)
	}
	frame, err := consumer.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(frame.Payload, payload) {
		t.Fatalf("payload = %q, want %q", frame.Payload, payload)
	}
	if frame.Flags != 0x11 || frame.Timestamp != 12345 || frame.Sequence != 0 {
		t.Fatalf("frame = %+v", frame)
	}
	consumer.Advance()
	if _, err := consumer.Read(); !errors.Is(err, ErrRingEmpty) {
		t.Fatalf("Read after Advance = %v, want ErrRingEmpty", err)
	}
}

// 帧号必须从 0 开始且连续。seqlock 的就绪值是 2N+2，解码写错一位就会整体偏移，
// 而那种错误只在跨帧比对时才看得出来。
func TestRing_SequenceIsContiguousFromZero(t *testing.T) {
	producer, consumer, _ := newRingPair(t)

	for round := 0; round < 3; round++ {
		for i := 0; i < int(testGeometry().SlotCount); i++ {
			want := uint64(round*int(testGeometry().SlotCount) + i)
			if err := producer.Write([]byte{byte(want)}, 0, 0); err != nil {
				t.Fatalf("Write %d: %v", want, err)
			}
			frame, err := consumer.Read()
			if err != nil {
				t.Fatalf("Read %d: %v", want, err)
			}
			if frame.Sequence != want {
				t.Fatalf("第 %d 帧的 Sequence = %d", want, frame.Sequence)
			}
			consumer.Advance()
		}
	}
}

func TestRing_FullAndDrain(t *testing.T) {
	producer, consumer, _ := newRingPair(t)
	g := testGeometry()

	for i := 0; i < int(g.SlotCount); i++ {
		if err := producer.Write([]byte{byte(i)}, 0, 0); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
	if err := producer.Write([]byte{9}, 0, 0); !errors.Is(err, ErrRingFull) {
		t.Fatalf("第 %d 次写入 = %v, want ErrRingFull", g.SlotCount+1, err)
	}

	for i := 0; i < int(g.SlotCount); i++ {
		frame, err := consumer.Read()
		if err != nil {
			t.Fatalf("Read %d: %v", i, err)
		}
		if len(frame.Payload) != 1 || frame.Payload[0] != byte(i) {
			t.Fatalf("第 %d 帧 = %v", i, frame.Payload)
		}
		consumer.Advance()
	}
	// 排空之后又能写
	if err := producer.Write([]byte{9}, 0, 0); err != nil {
		t.Fatalf("排空后写入失败: %v", err)
	}
}

// 【核心安全断言】：恶意生产者声明一个超出 slot 的长度时，消费者必须拒绝，
// 而不是 clamp 后继续读。clamp 会把一个 bug 变成静默的数据损坏。
func TestRing_RejectsOversizedPayloadLength(t *testing.T) {
	producer, consumer, mem := newRingPair(t)
	g := testGeometry()

	if err := producer.Write([]byte("ok"), 0, 0); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// 直接改写描述符里的长度，模拟对端作恶
	descOff := g.descriptorOffset(0)
	binary.LittleEndian.PutUint32(mem[descOff+ringDescOffLength:], g.SlotSize+1)

	_, err := consumer.Read()
	if !errors.Is(err, ErrRingCorrupt) {
		t.Fatalf("Read = %v, want ErrRingCorrupt", err)
	}
}

// 生产者自己也不能写超长载荷。
func TestRing_WriteRejectsOversizedPayload(t *testing.T) {
	producer, _, _ := newRingPair(t)
	if err := producer.Write(make([]byte, testGeometry().SlotSize+1), 0, 0); !errors.Is(
		err, ErrRingCorrupt,
	) {
		t.Fatalf("Write oversized = %v, want ErrRingCorrupt", err)
	}
}

// 生产者正在写（sequence 为奇数）的 slot 必须被跳过，不能读到半成品。
func TestRing_SkipsInFlightSlot(t *testing.T) {
	producer, consumer, mem := newRingPair(t)
	if err := producer.Write([]byte("x"), 0, 0); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// 把 sequence 改成奇数，模拟「正在写入」
	descOff := testGeometry().descriptorOffset(0)
	binary.LittleEndian.PutUint64(mem[descOff+ringDescOffSequence:], 1)

	if _, err := consumer.Read(); !errors.Is(err, ErrRingEmpty) {
		t.Fatalf("Read in-flight slot = %v, want ErrRingEmpty", err)
	}
}

// 【对端游标不可信】：生产者写了个不可能的游标时，消费者必须报套圈而不是
// 据此算出一个越界的 slot。
func TestRing_RejectsImpossibleProducerCursor(t *testing.T) {
	_, consumer, mem := newRingPair(t)
	binary.LittleEndian.PutUint64(mem[ringOffProducerCursor:], 1<<40)

	if _, err := consumer.Read(); !errors.Is(err, ErrRingLapped) {
		t.Fatalf("Read = %v, want ErrRingLapped", err)
	}
}

// 被套圈之后跳到最新一圈，而不是从已被覆盖的 slot 读撕裂内容。
func TestRing_SkipToLatest(t *testing.T) {
	producer, consumer, _ := newRingPair(t)
	g := testGeometry()

	// 写满两圈，消费者一直没推进
	for i := 0; i < int(g.SlotCount); i++ {
		if err := producer.Write([]byte{byte(i)}, 0, 0); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
	consumer.SkipToLatest()
	// 跳过之后不应当报套圈
	if _, err := consumer.Read(); errors.Is(err, ErrRingLapped) {
		t.Fatalf("SkipToLatest 之后仍报套圈")
	}
}

// 头部里的几何参数与控制面不符 = 协议违规，不是可协商的参数。
func TestRing_HeaderMustMatchControlPlane(t *testing.T) {
	g := testGeometry()
	total, _ := g.TotalBytes()
	mem := make([]byte, total)
	if err := InitHeader(mem, g); err != nil {
		t.Fatalf("InitHeader: %v", err)
	}
	// 篡改头部里的 slot_size
	binary.LittleEndian.PutUint32(mem[ringOffSlotSize:], g.SlotSize*2)

	if _, err := NewRing(mem, g, false); !errors.Is(err, ErrRingGeometry) {
		t.Fatalf("NewRing = %v, want ErrRingGeometry", err)
	}
}

func TestRing_RejectsBadMagicAndVersion(t *testing.T) {
	g := testGeometry()
	total, _ := g.TotalBytes()

	bad := make([]byte, total)
	_ = InitHeader(bad, g)
	copy(bad[ringOffMagic:], "XXXX")
	if _, err := NewRing(bad, g, false); !errors.Is(err, ErrRingGeometry) {
		t.Fatalf("bad magic = %v, want ErrRingGeometry", err)
	}

	badVersion := make([]byte, total)
	_ = InitHeader(badVersion, g)
	binary.LittleEndian.PutUint32(badVersion[ringOffVersion:], 99)
	if _, err := NewRing(badVersion, g, false); !errors.Is(err, ErrRingGeometry) {
		t.Fatalf("bad version = %v, want ErrRingGeometry", err)
	}
}

// 映射长度与几何参数对不上必须拒绝——那说明 memfd 大小与控制面下发的不一致。
func TestRing_RejectsWrongMappingSize(t *testing.T) {
	g := testGeometry()
	total, _ := g.TotalBytes()
	mem := make([]byte, total)
	_ = InitHeader(mem, g)

	if _, err := NewRing(mem[:total-1], g, false); !errors.Is(err, ErrRingGeometry) {
		t.Fatalf("short mapping = %v, want ErrRingGeometry", err)
	}
}

// 控制面参数本身必须自洽：slot_count 非 2 的幂会让掩码取模失效，
// 症状是随机读到错误的 slot，极难定位。
func TestValidateRingConfig(t *testing.T) {
	base := func() *ipcv1.TransferRingConfig {
		return &ipcv1.TransferRingConfig{
			SlotCount: 4, SlotSize: 64,
			HeaderBytes: RingHeaderBytes, DescriptorBytes: RingDescriptorBytes,
		}
	}
	if _, err := ValidateRingConfig(base()); err != nil {
		t.Fatalf("合法配置被拒: %v", err)
	}

	for _, tc := range []struct {
		name   string
		mutate func(*ipcv1.TransferRingConfig)
	}{
		{"slot_count 非 2 的幂", func(c *ipcv1.TransferRingConfig) { c.SlotCount = 3 }},
		{"slot_count 为 0", func(c *ipcv1.TransferRingConfig) { c.SlotCount = 0 }},
		{"slot_size 为 0", func(c *ipcv1.TransferRingConfig) { c.SlotSize = 0 }},
		{"header_bytes 不对", func(c *ipcv1.TransferRingConfig) { c.HeaderBytes = 8192 }},
		{"descriptor_bytes 不对", func(c *ipcv1.TransferRingConfig) { c.DescriptorBytes = 64 }},
		{"nil", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg *ipcv1.TransferRingConfig
			if tc.mutate != nil {
				cfg = base()
				tc.mutate(cfg)
			}
			if _, err := ValidateRingConfig(cfg); !errors.Is(err, ErrRingGeometry) {
				t.Fatalf("err = %v, want ErrRingGeometry", err)
			}
		})
	}
}

// 角色不能串：消费者不能写、生产者不能读。
func TestRing_RoleIsEnforced(t *testing.T) {
	producer, consumer, _ := newRingPair(t)
	if err := consumer.Write([]byte("x"), 0, 0); err == nil {
		t.Error("消费者写入被放行")
	}
	if _, err := producer.Read(); err == nil {
		t.Error("生产者读取被放行")
	}
}
