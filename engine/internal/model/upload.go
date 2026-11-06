package model

import "context"

type UploadProgress struct {
	Phase string `json:"phase"`
	Bytes int64  `json:"uploaded_bytes"`
	Total int64  `json:"total_bytes"`
}
type uploadKey struct{}

func WithUploadProgress(ctx context.Context, f func(UploadProgress) error) context.Context {
	return context.WithValue(ctx, uploadKey{}, f)
}
func ReportUpload(ctx context.Context, p UploadProgress) error {
	if f, ok := ctx.Value(uploadKey{}).(func(UploadProgress) error); ok {
		return f(p)
	}
	return nil
}
