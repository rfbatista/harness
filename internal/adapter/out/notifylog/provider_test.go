package notifylog_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"

	"operators-mcp/internal/adapter/out/notifylog"
	"operators-mcp/internal/ports"
	"operators-mcp/internal/ports/deliverytest"
)

// syncBuffer is a buffer the logger and the test share.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) ids() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	scan := bufio.NewScanner(bytes.NewReader(s.b.Bytes()))
	for scan.Scan() {
		var line struct{ Msg, ID string }
		if json.Unmarshal(scan.Bytes(), &line) == nil && line.Msg == "notification" {
			ids = append(ids, line.ID)
		}
	}
	return ids
}

func TestNotificationDeliveryConformance(t *testing.T) {
	deliverytest.NotificationDeliveryConformance(t, func(t *testing.T) (ports.NotificationDelivery, deliverytest.Observed) {
		buf := &syncBuffer{}
		return notifylog.New(slog.New(slog.NewJSONHandler(buf, nil))), buf.ids
	})
}
