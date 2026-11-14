package sync

import "fmt"

// First contacts must not enter the deferred peer-send queue. Unlike ordinary
// approved sends, they require a current directory grant and a single budget.
func (s *Service) FirstContactReady() error {
	if s.paired() || !s.standaloneReady() {
		return fmt.Errorf("New chats are unavailable while engines are paired or sending recovery is pending; nothing was queued. Use WhatsApp directly for this first message")
	}
	return nil
}
