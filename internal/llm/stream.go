package llm

import (
	"bufio"
	"bytes"
	"io"
)

// readSSE reads Server-Sent Events from an io.Reader and calls onData for each "data:" payload.
func readSSE(r io.Reader, onData func([]byte) error) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if bytes.HasPrefix(line, []byte("data: ")) {
			data := bytes.TrimPrefix(line, []byte("data: "))
			if bytes.Equal(data, []byte("[DONE]")) {
				break
			}
			if err := onData(data); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}
