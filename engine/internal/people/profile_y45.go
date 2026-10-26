package people

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"limmr/internal/dbutil"
	"limmr/internal/hub"
	"limmr/internal/model"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// NormalizePhoto strips metadata, bounds decoding, and emits a 512px JPEG.
func NormalizePhoto(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > MaxAvatarBytes {
		return nil, model.ErrInvalid
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		// ImageIO via the system utility is the macOS HEIC decoder; never a network service.
		if len(data) < 12 || string(data[4:8]) != "ftyp" || !strings.Contains("heic heix hevc hevx mif1", string(data[8:12])) {
			return nil, model.ErrInvalid
		}
		dir, e := os.MkdirTemp("", "limmr-photo-")
		if e != nil {
			return nil, e
		}
		defer os.RemoveAll(dir)
		in, out := filepath.Join(dir, "input.heic"), filepath.Join(dir, "output.jpg")
		if e = os.WriteFile(in, data, 0600); e != nil {
			return nil, e
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if exec.CommandContext(ctx, "/usr/bin/sips", "-s", "format", "jpeg", "-Z", "512", in, "--out", out).Run() != nil {
			return nil, model.ErrInvalid
		}
		data, e = os.ReadFile(out)
		if e != nil {
			return nil, e
		}
		cfg, format, err = image.DecodeConfig(bytes.NewReader(data))
	}
	if err != nil || (format != "jpeg" && format != "png") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > 32_000_000 {
		return nil, model.ErrInvalid
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, model.ErrInvalid
	}
	dst := image.NewRGBA(image.Rect(0, 0, 512, 512))
	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	x0, y0 := b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2
	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
			c := color.RGBAModel.Convert(src.At(x0+x*side/512, y0+y*side/512)).(color.RGBA)
			if c.A < 255 {
				c.R += 255 - c.A
				c.G += 255 - c.A
				c.B += 255 - c.A
				c.A = 255
			}
			dst.SetRGBA(x, y, c)
		}
	}
	var out bytes.Buffer
	err = jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85})
	return out.Bytes(), err
}
func (s *Service) photoRef(data []byte, source string) (*model.PhotoReference, error) {
	data, e := NormalizePhoto(data)
	if e != nil {
		return nil, e
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	dir := filepath.Join(s.Hub.Home, "avatars", "custom")
	if e = os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	if e = os.WriteFile(filepath.Join(dir, digest+".jpg"), data, 0600); e != nil {
		return nil, e
	}
	return &model.PhotoReference{Source: source, SHA256: digest, UpdatedAt: s.Hub.Now().UTC()}, nil
}
func (s *Service) SetPhoto(id string, data []byte) error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	var p model.Person
	if e := s.Hub.Repo.Get("people", id, &p); e != nil {
		return e
	}
	if data == nil {
		p.Photo = p.ContactsPhoto
	} else {
		ref, e := s.photoRef(data, "custom")
		if e != nil {
			return e
		}
		p.Photo = ref
	}
	p.UpdatedAt = s.Hub.Now().UTC()
	return s.Hub.Repo.Put("people", id, p)
}

type ContactMatch struct {
	PersonID string               `json:"person_id"`
	Emails   []model.LabeledValue `json:"emails"`
	Birthday string               `json:"birthday"`
	Org      string               `json:"org"`
	Title    string               `json:"title"`
	Photo    []byte               `json:"photo_b64,omitempty"`
}

// A complete matched snapshot: removed contacts lose only Contacts-owned values.
func (s *Service) ImportContacts(matches []ContactMatch) error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	if len(matches) > 10000 {
		return model.ErrInvalid
	}
	byID := map[string]ContactMatch{}
	for _, m := range matches {
		if _, ok := byID[m.PersonID]; ok {
			return model.ErrInvalid
		}
		byID[m.PersonID] = m
	}
	ps, e := hub.List[model.Person](s.Hub.Repo, "people")
	if e != nil {
		return e
	}
	xs := []hub.Mutation{}
	for _, p := range ps {
		before := p
		if p.Details.Sources != nil {
			copy := map[string]string{}
			for k, v := range p.Details.Sources {
				copy[k] = v
			}
			p.Details.Sources = copy
		}
		m, matched := byID[p.ID]
		delete(byID, p.ID)
		emails := []model.LabeledValue{}
		for _, v := range p.Details.Emails {
			if v.Source != "contacts" {
				emails = append(emails, v)
			}
		}
		p.Details.Emails = emails
		if p.Details.Sources == nil {
			p.Details.Sources = map[string]string{}
		}
		for key, dst := range map[string]*string{"birthday": &p.Details.Birthday, "org": &p.Details.Org, "title": &p.Details.Title} {
			if p.Details.Sources[key] == "contacts" {
				*dst = ""
				delete(p.Details.Sources, key)
			}
		}
		p.ContactsPhoto = nil
		if p.Photo != nil && p.Photo.Source == "contacts" {
			p.Photo = nil
		}
		if matched {
			raw := map[string]any{"emails": m.Emails, "birthday": m.Birthday, "org": m.Org, "title": m.Title}
			var validated model.Person
			if e = applyDetails(&validated, raw, nil); e != nil {
				return e
			}
			for _, v := range m.Emails {
				v.Source = "contacts"
				found := false
				for _, old := range p.Details.Emails {
					found = found || strings.EqualFold(old.Value, v.Value)
				}
				if !found {
					p.Details.Emails = append(p.Details.Emails, v)
				}
			}
			for key, pair := range map[string]struct {
				dst   *string
				value string
			}{"birthday": {&p.Details.Birthday, m.Birthday}, "org": {&p.Details.Org, m.Org}, "title": {&p.Details.Title, m.Title}} {
				if *pair.dst == "" && pair.value != "" {
					*pair.dst = pair.value
					p.Details.Sources[key] = "contacts"
				}
			}
			if len(m.Photo) > 0 {
				ref, e := s.photoRef(m.Photo, "contacts")
				if e != nil {
					return e
				}
				p.ContactsPhoto = ref
				if p.Photo == nil {
					p.Photo = ref
				}
			}
		}
		if reflect.DeepEqual(before, p) {
			continue
		}
		p.UpdatedAt = s.Hub.Now().UTC()
		c := Change{ID: dbutil.ID(), At: p.UpdatedAt, Source: "you", Kind: "person", SubjectID: p.ID, Before: &before, After: &p}
		xs = append(xs, hub.Mutation{Bucket: "people", ID: p.ID, Value: p}, hub.Mutation{Bucket: "changes", ID: c.ID, Value: c})
	}
	if len(byID) > 0 {
		return model.ErrNotFound
	}
	return s.Hub.Repo.Apply(xs...)
}

var genderStatement = regexp.MustCompile(`(?i)^(?:(?:[\pL .'-]+) (?:is|uses) )?(male|man|he|he/him|female|woman|she|she/her|nonbinary|non-binary|they|they/them)[.!]?$`)

func Pronouns(text string) string {
	m := genderStatement.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return ""
	}
	switch strings.ToLower(m[1]) {
	case "male", "man", "he", "he/him":
		return "he/him"
	case "female", "woman", "she", "she/her":
		return "she/her"
	default:
		return "they/them"
	}
}

// Lazy one-time migration runs at the control boundary and is reversible through changes.
func (s *Service) MigrateAgentNotes() error {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	var done bool
	if s.Hub.Repo.Get("people_migrations", "agent_notes_y45", &done) == nil && done {
		return nil
	}
	changes, e := s.Changes()
	if e != nil {
		return e
	}
	latest := map[string]Change{}
	for _, c := range changes {
		if c.Kind == "person" && !c.Reverted && c.Before != nil && c.After != nil && c.Before.Notes != c.After.Notes {
			if old, ok := latest[c.SubjectID]; !ok || c.At.After(old.At) {
				latest[c.SubjectID] = c
			}
		}
	}
	xs := []hub.Mutation{}
	for id, c := range latest {
		if c.Source == "you" {
			continue
		}
		var p model.Person
		if s.Hub.Repo.Get("people", id, &p) != nil || p.Notes == "" || p.Notes != c.After.Notes {
			continue
		}
		before := p
		field, value := "notes", p.Notes
		if v := Pronouns(value); v != "" {
			field, value = "pronouns", v
		}
		proposeDetail(&p, field, "", value, c.Source)
		for i := range p.DetailSuggestions {
			if p.DetailSuggestions[i].Field == field && p.DetailSuggestions[i].Value == value && p.DetailSuggestions[i].Source == c.Source {
				p.DetailSuggestions[i].CreatedAt = c.At
			}
		}
		p.Notes = c.Before.Notes
		p.UpdatedAt = s.Hub.Now().UTC()
		ch := Change{ID: dbutil.ID(), At: p.UpdatedAt, Source: "you", Kind: "person", SubjectID: id, Before: &before, After: &p}
		xs = append(xs, hub.Mutation{Bucket: "people", ID: id, Value: p}, hub.Mutation{Bucket: "changes", ID: ch.ID, Value: ch})
	}
	xs = append(xs, hub.Mutation{Bucket: "people_migrations", ID: "agent_notes_y45", Value: true})
	return s.Hub.Repo.Apply(xs...)
}

type PersonSummary struct {
	PersonID      string    `json:"person_id"`
	Text          string    `json:"text"`
	WrittenBy     string    `json:"written_by"`
	WrittenAt     time.Time `json:"written_at"`
	BasedOnUntil  time.Time `json:"based_on_until"`
	EvidenceChats []string  `json:"evidence_chats,omitempty"`
}

func (s *Service) SavePersonSummary(a *model.Agent, id, text string, until time.Time) (PersonSummary, error) {
	v := PersonSummary{}
	if a != nil && (!a.Permissions.SaveSummaries || !a.Permissions.ReadMessages || !a.Permissions.ReadPeople) {
		return v, model.ErrForbidden
	}
	if utf8.RuneCountInString(text) > 600 || strings.TrimSpace(text) == "" || (until.IsZero() || until.After(s.Hub.Now())) {
		return v, model.ErrInvalid
	}
	p, e := s.Profile(a, id)
	if e != nil {
		return v, e
	}
	if p.Stats.DM30 < 5 {
		return v, model.ErrInvalid
	}
	chats := []string{}
	for _, j := range p.Person.JIDs {
		if s.Hub.CanChat(a, j) {
			chats = append(chats, j)
		}
	}
	source := "This Mac"
	if a != nil {
		source = a.ID
	}
	v = PersonSummary{id, text, source, s.Hub.Now().UTC(), until, chats}
	return v, s.Hub.Repo.Put("person_summaries", id, v)
}

func (s *Service) ProposePronouns(a model.Agent, id, text string, evidence []string) (model.Person, error) {
	s.shared().mu.Lock()
	defer s.shared().mu.Unlock()
	if !a.Permissions.WritePeople {
		return model.Person{}, model.ErrForbidden
	}
	if _, e := s.Profile(&a, id); e != nil {
		return model.Person{}, e
	}
	chats, e := s.evidenceChats(&a, evidence)
	if e != nil {
		return model.Person{}, e
	}
	var p model.Person
	if e = s.Hub.Repo.Get("people", id, &p); e != nil {
		return p, e
	}
	before := p
	v := Pronouns(text)
	if v == "" {
		return p, model.ErrInvalid
	}
	proposeDetail(&p, "pronouns", "", v, a.ID)
	for i := range p.DetailSuggestions {
		if p.DetailSuggestions[i].Field == "pronouns" && p.DetailSuggestions[i].Value == v && p.DetailSuggestions[i].Source == a.ID {
			p.DetailSuggestions[i].CreatedAt = s.Hub.Now().UTC()
			p.DetailSuggestions[i].EvidenceMessageIDs = evidence
			p.DetailSuggestions[i].EvidenceChats = chats
		}
	}
	p.UpdatedAt = s.Hub.Now().UTC()
	c := Change{ID: dbutil.ID(), At: p.UpdatedAt, Source: a.ID, Kind: "person", SubjectID: id, Before: &before, After: &p}
	if e = s.Hub.Repo.Apply(hub.Mutation{Bucket: "people", ID: id, Value: p}, hub.Mutation{Bucket: "changes", ID: c.ID, Value: c}); e != nil {
		return p, e
	}
	profile, e := s.Profile(&a, id)
	return profile.Person, e
}
