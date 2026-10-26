package hub

import (
	"context"
	"limmr/internal/dbutil"
	"limmr/internal/wa"
)

func (s *Service) shared() *Service {
	if s.parent != nil {
		return s.parent.shared()
	}
	return s
}
func (s *Service) WithContext(ctx context.Context) *Service {
	messages := &wa.Store{DB: dbutil.WithContext(s.Messages.DB, ctx)}
	return &Service{MayRunAutomations: s.shared().MayRunAutomations, TaskSendValidate: s.TaskSendValidate, TaskSent: s.TaskSent, TaskSuggestionDone: s.TaskSuggestionDone, parent: s.shared(), Repo: &Repo{DB: dbutil.WithContext(s.Repo.DB, ctx)}, Messages: messages, Messenger: s.Messenger, Home: s.Home, Now: s.Now, Watches: s.Watches, WatchStore: s.WatchStore}
}
