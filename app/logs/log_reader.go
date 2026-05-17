package logs

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"time"
)

type LogReader struct {
	filePath string
	store    *Store
	service  string
	done     chan struct{}
}

func NewLogReader(filePath, service string, store *Store) *LogReader {
	return &LogReader{
		filePath: filePath,
		store:    store,
		service:  service,
		done:     make(chan struct{}),
	}
}

func (lr *LogReader) Start() error {
	f, err := os.Open(lr.filePath)
	if err != nil {
		return fmt.Errorf("log reader: opening %s: %w", lr.filePath, err)
	}

	f.Seek(0, io.SeekEnd)

	go lr.tail(f)
	return nil
}

func (lr *LogReader) Stop() {
	close(lr.done)
}

func (lr *LogReader) tail(f *os.File) {
	defer f.Close()

	reader := bufio.NewReader(f)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-lr.done:
			return
		case <-ticker.C:
			for {
				line, err := reader.ReadString('\n')
				if line != "" {
					lr.store.Add(lr.service, line)
				}
				if err != nil {
					break
				}
			}
		}
	}
}

func ReadLastLines(filePath string, n int) ([]string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("log reader: opening %s: %w", filePath, err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}

	size := stat.Size()
	if size == 0 {
		return nil, nil
	}

	var lines []string
	buf := make([]byte, 1)
	offset := size - 1
	var currentLine []byte

	for offset >= 0 && len(lines) <= n {
		f.Seek(offset, io.SeekStart)
		f.Read(buf)

		if buf[0] == '\n' {
			if len(currentLine) > 0 {
				reversed := reverseBytes(currentLine)
				lines = append([]string{string(reversed)}, lines...)
				currentLine = nil
			}
		} else {
			currentLine = append(currentLine, buf[0])
		}
		offset--
	}

	if len(currentLine) > 0 {
		reversed := reverseBytes(currentLine)
		lines = append([]string{string(reversed)}, lines...)
	}

	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	return lines, nil
}

func reverseBytes(b []byte) []byte {
	r := make([]byte, len(b))
	for i, j := 0, len(b)-1; i <= j; i, j = i+1, j-1 {
		r[i], r[j] = b[j], b[i]
	}
	return r
}
