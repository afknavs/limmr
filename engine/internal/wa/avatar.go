package wa

import (
	"context"
	"io"
	"limmr/internal/model"
	"net/http"
	"net/url"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// FetchAvatar performs only a profile-photo query and bounded HTTPS download.
func (l *Live) FetchAvatar(ctx context.Context, jid string) ([]byte, error) {
	l.lifecycle.RLock()
	defer l.lifecycle.RUnlock()
	if l.closed || !l.Connected() {
		return nil, model.ErrNotFound
	}
	j, err := types.ParseJID(jid)
	if err != nil || j.User == "" || (j.Server != types.DefaultUserServer && j.Server != types.HiddenUserServer && j.Server != types.GroupServer) {
		return nil, model.ErrNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	info, err := l.Client.GetProfilePictureInfo(ctx, j, &whatsmeow.GetProfilePictureParams{Preview: true})
	if err != nil || info == nil || info.URL == "" {
		return nil, model.ErrNotFound
	}
	u, err := url.Parse(info.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return nil, model.ErrNotFound
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, model.ErrNotFound
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, model.ErrNotFound
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, model.ErrNotFound
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return nil, model.ErrNotFound
	}
	return data, nil
}
