// SPDX-FileCopyrightText: (C) 2026 Intel Corporation
// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"bufio"
	"bytes"
	"errors"
	"io"
)

type event struct {
	name string
	data []byte
}

// readEvents parses a text/event-stream until EOF, the callback returns false,
// or a read error. It returns nil when the stream ended because the callback
// stopped it or the server closed it cleanly.
func readEvents(r io.Reader, fn func(event) bool) error {
	br := bufio.NewReaderSize(r, 64*1024)
	var cur event
	var data [][]byte
	flush := func() bool {
		if cur.name == "" && len(data) == 0 {
			return true
		}
		cur.data = bytes.Join(data, []byte("\n"))
		if cur.name == "" {
			cur.name = "message"
		}
		keep := fn(cur)
		cur = event{}
		data = nil
		return keep
	}
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			line = bytes.TrimRight(line, "\r\n")
			switch {
			case len(line) == 0:
				if !flush() {
					return nil
				}
			case line[0] == ':':
				// heartbeat
			default:
				field, value, _ := bytes.Cut(line, []byte(":"))
				value = bytes.TrimPrefix(value, []byte(" "))
				switch string(field) {
				case "event":
					cur.name = string(value)
				case "data":
					data = append(data, append([]byte(nil), value...))
				}
			}
		}
		if err != nil {
			flush()
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}
