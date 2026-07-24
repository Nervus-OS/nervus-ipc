package sdk

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

// frameOf 手工拼一个合法 Frame，用于喂给读侧。
func frameOf(payload []byte) []byte {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(payload)))
	return append(hdr[:], payload...)
}

func TestWriteFrame_LengthPrefixIsBigEndian(t *testing.T) {
	var buf bytes.Buffer
	if err := writeFrame(&buf, []byte{0xAA, 0xBB, 0xCC}); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
	got := buf.Bytes()
	// 3 字节正文 → 前缀必须是 00 00 00 03（大端）。小端会写成 03 00 00 00，
	// 与 nervud 的 ReadFrameHeader 完全对不上——这条断言锁的就是字节序。
	want := []byte{0x00, 0x00, 0x00, 0x03, 0xAA, 0xBB, 0xCC}
	if !bytes.Equal(got, want) {
		t.Fatalf("frame bytes = % x, want % x", got, want)
	}
}

func TestWriteFrame_RejectsZeroAndOversize(t *testing.T) {
	var buf bytes.Buffer
	if err := writeFrame(&buf, nil); !errors.Is(err, ErrZeroLength) {
		t.Errorf("empty payload err = %v, want ErrZeroLength", err)
	}
	if err := writeFrame(&buf, make([]byte, MaxFrameBytes+1)); !errors.Is(err, ErrFrameTooLarge) {
		t.Errorf("oversize payload err = %v, want ErrFrameTooLarge", err)
	}
	// 本端的 bug 不应该把违规帧发给对端：两次拒绝都不该写出任何字节
	if buf.Len() != 0 {
		t.Errorf("rejected frames must not emit bytes, got %d", buf.Len())
	}
}

func TestReadFrameHeader_ZeroLengthIsProtocolError(t *testing.T) {
	r := bytes.NewReader([]byte{0, 0, 0, 0})
	if _, err := readFrameHeader(r); !errors.Is(err, ErrZeroLength) {
		t.Fatalf("err = %v, want ErrZeroLength", err)
	}
}

func TestReadFrameHeader_OversizeRejectedBeforeReadingBody(t *testing.T) {
	// 只给长度前缀，正文一个字节都不给。若实现试图排空对端自称的正文，
	// 这里会阻塞或报 EOF 而不是 ErrFrameTooLarge——那正是攻击者要的免费带宽。
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], MaxFrameBytes+1)
	r := bytes.NewReader(hdr[:])
	if _, err := readFrameHeader(r); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("err = %v, want ErrFrameTooLarge", err)
	}
}

func TestReadFrame_ExactBoundary(t *testing.T) {
	// 恰好 128 KiB 必须【通过】。边界写成 >= 而不是 > 是这类代码最常见的错。
	payload := bytes.Repeat([]byte{0x5A}, MaxFrameBytes)
	r := bytes.NewReader(frameOf(payload))
	buf := make([]byte, MaxFrameBytes)

	n, err := readFrameHeader(r)
	if err != nil {
		t.Fatalf("header at exact boundary: %v", err)
	}
	if n != MaxFrameBytes {
		t.Fatalf("n = %d, want %d", n, MaxFrameBytes)
	}
	body, err := readFrameBody(r, buf, n)
	if err != nil {
		t.Fatalf("body at exact boundary: %v", err)
	}
	if !bytes.Equal(body, payload) {
		t.Error("body mismatch at exact boundary")
	}
}

func TestReadFrame_Coalesced(t *testing.T) {
	// 粘包：一次 read 带回好几个 Frame。UDS 是字节流，read 的返回边界不是
	// 消息边界——实现必须靠长度前缀切分，不能假设「一次 read 一个消息」。
	var stream bytes.Buffer
	want := [][]byte{{1}, {2, 2}, {3, 3, 3}}
	for _, p := range want {
		stream.Write(frameOf(p))
	}

	buf := make([]byte, MaxFrameBytes)
	for i, w := range want {
		n, err := readFrameHeader(&stream)
		if err != nil {
			t.Fatalf("frame %d header: %v", i, err)
		}
		body, err := readFrameBody(&stream, buf, n)
		if err != nil {
			t.Fatalf("frame %d body: %v", i, err)
		}
		if !bytes.Equal(body, w) {
			t.Errorf("frame %d = % x, want % x", i, body, w)
		}
	}
}

// slowReader 每次最多吐 1 字节，模拟半包/慢读。
type slowReader struct{ data []byte }

func (s *slowReader) Read(p []byte) (int, error) {
	if len(s.data) == 0 {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	p[0] = s.data[0]
	s.data = s.data[1:]
	return 1, nil
}

func TestReadFrame_Fragmented(t *testing.T) {
	// 半包：长度前缀和正文都被拆成单字节到达。io.ReadFull 必须把它们凑齐，
	// 否则会读出一个截断的 Envelope 然后解码成垃圾。
	payload := []byte("fragmented-payload")
	r := &slowReader{data: frameOf(payload)}
	buf := make([]byte, MaxFrameBytes)

	n, err := readFrameHeader(r)
	if err != nil {
		t.Fatalf("header: %v", err)
	}
	body, err := readFrameBody(r, buf, n)
	if err != nil {
		t.Fatalf("body: %v", err)
	}
	if !bytes.Equal(body, payload) {
		t.Fatalf("body = %q, want %q", body, payload)
	}
}

func TestReadFrame_TruncatedBody(t *testing.T) {
	// 宣称 10 字节却只给 3：必须报错，不能返回那 3 字节当成完整消息。
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], 10)
	r := bytes.NewReader(append(hdr[:], 1, 2, 3))
	buf := make([]byte, MaxFrameBytes)

	n, err := readFrameHeader(r)
	if err != nil {
		t.Fatalf("header: %v", err)
	}
	if _, err := readFrameBody(r, buf, n); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestReadFrameHeader_CleanEOF(t *testing.T) {
	// 对端正常关闭必须是 io.EOF，调用方据此区分「正常断开」与「异常截断」。
	if _, err := readFrameHeader(bytes.NewReader(nil)); !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}
