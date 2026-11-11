package sse

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type byteReader struct{ io.Reader }

func (r byteReader) Read(p []byte) (int, error) { return r.Reader.Read(p[:1]) }

func TestDecoder(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n", "\r"} {
		raw := strings.Join([]string{"\ufeff: 心跳", "", "event: delta", "data: 招商", "data: 银行", "", "data:", "", ""}, newline)
		for _, r := range []io.Reader{strings.NewReader(raw), byteReader{strings.NewReader(raw)}} {
			d := NewDecoder(r, 1024)
			event, err := d.Next()
			require.NoError(t, err)
			require.Equal(t, Event{Type: "delta", Data: "招商\n银行"}, event)
			event, err = d.Next()
			require.NoError(t, err)
			require.Equal(t, Event{Type: "message"}, event)
			_, err = d.Next()
			require.ErrorIs(t, err, io.EOF)
		}
	}
}

func TestDecoderIncompleteAndLimits(t *testing.T) {
	_, err := NewDecoder(strings.NewReader("data: partial\n"), 1024).Next()
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	_, err = NewDecoder(strings.NewReader("data: 123456\n\n"), 8).Next()
	require.ErrorIs(t, err, ErrTooLarge)
	_, err = NewDecoder(strings.NewReader("data: "+strings.Repeat("x", MaxEventBytes)+"\n\n"), 2*MaxEventBytes).Next()
	require.Error(t, err)
}

func TestDecoderCRDispatchesBeforeNextRead(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	d := NewDecoder(byteReader{reader}, 1024)
	type decoded struct {
		event Event
		err   error
	}
	for _, item := range []struct {
		wire string
		want Event
	}{
		{"event: delta\rdata: 首段\r\r", Event{Type: "delta", Data: "首段"}},
		// 上次 CR 后的可选 LF 可以跨 read，不能被当作新的空行。
		{"\nevent: done\r\ndata: 完整\r\ndata: 回复\r\n\r", Event{Type: "done", Data: "完整\n回复"}},
	} {
		ready := make(chan decoded, 1)
		go func() { event, err := d.Next(); ready <- decoded{event, err} }()
		_, err := io.WriteString(writer, item.wire)
		require.NoError(t, err)
		select {
		case got := <-ready:
			require.NoError(t, got.err)
			require.Equal(t, item.want, got.event)
		case <-time.After(time.Second):
			t.Fatal("完整 CR 事件必须在下一字节或 EOF 到达之前返回")
		}
	}
	// 最后的可选 LF 不产生额外事件。
	go func() { _, _ = io.WriteString(writer, "\n"); _ = writer.Close() }()
	_, err := d.Next()
	require.ErrorIs(t, err, io.EOF)
}
