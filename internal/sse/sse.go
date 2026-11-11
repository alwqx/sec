// Package sse 提供有大小限制的 SSE 解析与 JSON 事件输出，不自动重连或重试请求。
package sse

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const MaxEventBytes = 1 << 20

var ErrTooLarge = errors.New("事件流超过大小限制")

type Event struct {
	Type string
	Data string
}

type Decoder struct {
	scan   *bufio.Scanner
	limit  *io.LimitedReader
	first  bool
	skipLF bool
}

func NewDecoder(r io.Reader, maxBytes int64) *Decoder {
	limit := &io.LimitedReader{R: r, N: maxBytes + 1}
	d := &Decoder{limit: limit, first: true}
	scan := bufio.NewScanner(limit)
	scan.Buffer(make([]byte, 4096), MaxEventBytes)
	scan.Split(d.lines)
	d.scan = scan
	return d
}

// CR 立即结束当前行，下一次扫描只跳过可选的 LF，不等待后续字节。
func (d *Decoder) lines(data []byte, eof bool) (int, []byte, error) {
	offset := 0
	if d.skipLF && len(data) > 0 {
		d.skipLF = false
		if data[0] == '\n' {
			offset = 1
		}
	}
	line := data[offset:]
	if i := bytes.IndexAny(line, "\r\n"); i >= 0 {
		d.skipLF = line[i] == '\r'
		return offset + i + 1, line[:i], nil
	}
	if eof && len(line) > 0 {
		return len(data), line, nil
	}
	return offset, nil, nil
}

func (d *Decoder) Next() (Event, error) {
	var event Event
	var data []string
	size := 0
	for d.scan.Scan() {
		if d.limit.N == 0 {
			return Event{}, ErrTooLarge
		}
		line := d.scan.Text()
		if d.first {
			line = strings.TrimPrefix(line, "\ufeff")
			d.first = false
		}
		size += len(line) + 1
		if size > MaxEventBytes {
			return Event{}, ErrTooLarge
		}
		if line == "" {
			if data != nil {
				event.Data = strings.Join(data, "\n")
				if event.Type == "" {
					event.Type = "message"
				}
				return event, nil
			}
			event, size = Event{}, 0
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event.Type = value
		case "data":
			data = append(data, value)
		}
	}
	if d.limit.N == 0 {
		return Event{}, ErrTooLarge
	}
	if err := d.scan.Err(); err != nil {
		return Event{}, fmt.Errorf("读取事件流失败：%w", err)
	}
	if data != nil {
		return Event{}, io.ErrUnexpectedEOF
	}
	return Event{}, io.EOF
}

func Write(w io.Writer, kind string, value any) error {
	if strings.ContainsAny(kind, "\r\n") {
		return errors.New("事件类型无效")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, data)
	return err
}
