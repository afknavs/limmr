package api

import (
	"encoding/json"
	"limmr/internal/model"
	"net/http"
	"strconv"
	"unicode/utf8"
)

func pageBounds(r *http.Request, n, def int) (int, int, string, error) {
	limit := integer(r, "limit", def)
	if limit < 1 {
		return 0, 0, "", model.ErrInvalid
	}
	limit = min(limit, 100)
	start := 0
	var e error
	if v := r.URL.Query().Get("cursor"); v != "" {
		start, e = strconv.Atoi(v)
		if e != nil || start < 0 || start > n {
			return 0, 0, "", model.ErrInvalid
		}
	}
	end := min(start+limit, n)
	next := ""
	if end < n {
		next = strconv.Itoa(end)
	}
	return start, end, next, nil
}

// fitPage bounds bytes as well as row count. Large records remain on detail routes.
func fitPage[T any](xs []T, start, end int) int {
	size := 32
	for i := start; i < end; i++ {
		b, _ := json.Marshal(xs[i])
		if i > start && size+len(b) > 90000 {
			return i
		}
		size += len(b) + 1
	}
	return end
}
func shortField(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
