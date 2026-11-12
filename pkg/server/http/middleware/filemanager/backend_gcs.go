package filemanager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/iterator"
)

// GCS keeps the files in a Google Cloud Storage bucket. Credentials come from
// the environment (Application Default Credentials, workload identity on GKE);
// STORAGE_EMULATOR_HOST points the client to an emulator.
type GCS struct {
	// Bucket is the name of the bucket. Required.
	Bucket string `cfg:"bucket"`
	// Prefix puts every object below this folder of the bucket, e.g. "files".
	Prefix string `cfg:"prefix"`
}

// gcsBackend maps the folder tree onto object names. A bucket has no real
// folders: a folder exists while an object is below its name, and an empty
// folder is kept by a zero byte marker object "<folder>/".
//
// Writes are atomic, an object appears when its upload completes. Moving a
// folder copies and deletes every object below it, so a failure can leave
// objects at both places.
type gcsBackend struct {
	bucket *storage.BucketHandle
	prefix string
}

func newGCSBackend(bucket *storage.BucketHandle, prefix string) *gcsBackend {
	return &gcsBackend{
		bucket: bucket,
		prefix: strings.Trim(path.Clean("/"+prefix), "/"),
	}
}

// key is the object name of a file.
func (b *gcsBackend) key(name string) string {
	if name == "." {
		name = ""
	}

	return strings.TrimPrefix(path.Join(b.prefix, name), "/")
}

// dirKey is the object name prefix of the content of a folder.
func (b *gcsBackend) dirKey(name string) string {
	if k := b.key(name); k != "" {
		return k + "/"
	}

	return ""
}

func (b *gcsBackend) Open(ctx context.Context, name string) (io.ReadSeekCloser, fs.FileInfo, error) {
	attrs, err := b.bucket.Object(b.key(name)).Attrs(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) && b.isDir(ctx, name) {
			return emptyFile{strings.NewReader("")}, gcsInfo{name: path.Base(name), dir: true}, nil
		}

		return nil, nil, gcsError(err)
	}

	return &gcsReader{
		ctx: ctx,
		// every read stays on the version that was opened
		obj:  b.bucket.Object(attrs.Name).Generation(attrs.Generation),
		size: attrs.Size,
	}, fileInfo(attrs), nil
}

func (b *gcsBackend) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	if name == "." {
		return gcsInfo{name: ".", dir: true}, nil
	}

	attrs, err := b.bucket.Object(b.key(name)).Attrs(ctx)
	if err == nil {
		return fileInfo(attrs), nil
	}

	if !errors.Is(err, storage.ErrObjectNotExist) {
		return nil, gcsError(err)
	}

	if b.isDir(ctx, name) {
		return gcsInfo{name: path.Base(name), dir: true}, nil
	}

	return nil, fs.ErrNotExist
}

func (b *gcsBackend) Lstat(ctx context.Context, name string) (fs.FileInfo, error) {
	return b.Stat(ctx, name)
}

// isDir reports whether at least one object, or the marker, is below name.
func (b *gcsBackend) isDir(ctx context.Context, name string) bool {
	prefix := b.dirKey(name)
	if prefix == "" {
		return true
	}

	it := b.bucket.Objects(ctx, &storage.Query{Prefix: prefix})
	it.PageInfo().MaxSize = 1

	_, err := it.Next()

	return err == nil
}

func (b *gcsBackend) ReadDir(ctx context.Context, name string) ([]fs.FileInfo, error) {
	prefix := b.dirKey(name)

	q := &storage.Query{Prefix: prefix, Delimiter: "/"}
	if err := q.SetAttrSelection([]string{"Name", "Size", "Updated"}); err != nil {
		return nil, err
	}

	var infos []fs.FileInfo

	it := b.bucket.Objects(ctx, q)
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}

		if err != nil {
			return nil, gcsError(err)
		}

		if attrs.Prefix != "" {
			infos = append(infos, gcsInfo{name: strings.TrimSuffix(strings.TrimPrefix(attrs.Prefix, prefix), "/"), dir: true})

			continue
		}

		// the marker of the folder itself
		if attrs.Name == prefix {
			continue
		}

		infos = append(infos, fileInfo(attrs))
	}

	return infos, nil
}

func (b *gcsBackend) WriteFile(ctx context.Context, name string, body io.Reader) error {
	if err := b.checkParents(ctx, name); err != nil {
		return err
	}

	// cancelling the context aborts the upload, nothing is committed
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	w := b.bucket.Object(b.key(name)).NewWriter(ctx)
	w.ContentType = contentType(name)

	if _, err := io.Copy(w, body); err != nil {
		cancel()
		_ = w.Close()

		return err
	}

	return gcsError(w.Close())
}

func (b *gcsBackend) MkdirAll(ctx context.Context, name string) error {
	if name == "." {
		return nil
	}

	if err := b.checkParents(ctx, name); err != nil {
		return err
	}

	if _, err := b.bucket.Object(b.key(name)).Attrs(ctx); err == nil {
		return fmt.Errorf("%w: %s is a file", fs.ErrExist, name)
	}

	w := b.bucket.Object(b.dirKey(name)).If(storage.Conditions{DoesNotExist: true}).NewWriter(ctx)
	if err := w.Close(); err != nil && !isStatus(err, http.StatusPreconditionFailed) {
		return gcsError(err)
	}

	return nil
}

// checkParents refuses a name below a file, a bucket would accept it.
func (b *gcsBackend) checkParents(ctx context.Context, name string) error {
	for dir := path.Dir(name); dir != "." && dir != "/"; dir = path.Dir(dir) {
		_, err := b.bucket.Object(b.key(dir)).Attrs(ctx)
		if err == nil {
			return fmt.Errorf("%w: %s is a file", fs.ErrExist, dir)
		}

		if !errors.Is(err, storage.ErrObjectNotExist) {
			return gcsError(err)
		}
	}

	return nil
}

func (b *gcsBackend) RemoveAll(ctx context.Context, name string) error {
	if err := b.bucket.Object(b.key(name)).Delete(ctx); err != nil && !errors.Is(err, storage.ErrObjectNotExist) {
		return gcsError(err)
	}

	return b.eachBelow(ctx, name, func(key string) error {
		if err := b.bucket.Object(key).Delete(ctx); err != nil && !errors.Is(err, storage.ErrObjectNotExist) {
			return gcsError(err)
		}

		return nil
	})
}

func (b *gcsBackend) Rename(ctx context.Context, from, to string) error {
	if err := b.checkParents(ctx, to); err != nil {
		return err
	}

	fromKey := b.key(from)
	if _, err := b.bucket.Object(fromKey).Attrs(ctx); err == nil {
		return b.move(ctx, fromKey, b.key(to))
	} else if !errors.Is(err, storage.ErrObjectNotExist) {
		return gcsError(err)
	}

	fromPrefix, toPrefix := b.dirKey(from), b.dirKey(to)

	return b.eachBelow(ctx, from, func(key string) error {
		return b.move(ctx, key, toPrefix+strings.TrimPrefix(key, fromPrefix))
	})
}

func (b *gcsBackend) move(ctx context.Context, from, to string) error {
	if _, err := b.bucket.Object(to).CopierFrom(b.bucket.Object(from)).Run(ctx); err != nil {
		return gcsError(err)
	}

	if err := b.bucket.Object(from).Delete(ctx); err != nil && !errors.Is(err, storage.ErrObjectNotExist) {
		return gcsError(err)
	}

	return nil
}

// eachBelow calls fn with the key of every object below the folder name.
// The keys are collected first, fn may change the listing.
func (b *gcsBackend) eachBelow(ctx context.Context, name string, fn func(key string) error) error {
	q := &storage.Query{Prefix: b.dirKey(name)}
	if err := q.SetAttrSelection([]string{"Name"}); err != nil {
		return err
	}

	var keys []string

	it := b.bucket.Objects(ctx, q)
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}

		if err != nil {
			return gcsError(err)
		}

		keys = append(keys, attrs.Name)
	}

	for _, key := range keys {
		if err := fn(key); err != nil {
			return err
		}
	}

	return nil
}

// gcsReader reads an object; Seek only moves the offset and the next Read
// opens a range reader there, so http.ServeContent's size probe costs nothing.
type gcsReader struct {
	ctx    context.Context //nolint:containedctx // the reader lives within the request
	obj    *storage.ObjectHandle
	reader io.ReadCloser
	pos    int64
	size   int64
}

func (r *gcsReader) Read(p []byte) (int, error) {
	if r.pos >= r.size {
		return 0, io.EOF
	}

	if r.reader == nil {
		reader, err := r.obj.NewRangeReader(r.ctx, r.pos, -1)
		if err != nil {
			return 0, gcsError(err)
		}

		r.reader = reader
	}

	n, err := r.reader.Read(p)
	r.pos += int64(n)

	return n, err
}

func (r *gcsReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekCurrent:
		offset += r.pos
	case io.SeekEnd:
		offset += r.size
	case io.SeekStart:
	default:
		return 0, errors.New("seek: invalid whence")
	}

	if offset < 0 {
		return 0, errors.New("seek: negative position")
	}

	if offset != r.pos {
		_ = r.Close()
		r.pos = offset
	}

	return offset, nil
}

func (r *gcsReader) Close() error {
	if r.reader == nil {
		return nil
	}

	err := r.reader.Close()
	r.reader = nil

	return err
}

type emptyFile struct{ *strings.Reader }

func (emptyFile) Close() error { return nil }

type gcsInfo struct {
	name    string
	size    int64
	modTime time.Time
	dir     bool
}

func fileInfo(attrs *storage.ObjectAttrs) gcsInfo {
	return gcsInfo{name: path.Base(attrs.Name), size: attrs.Size, modTime: attrs.Updated}
}

func (i gcsInfo) Name() string       { return i.name }
func (i gcsInfo) Size() int64        { return i.size }
func (i gcsInfo) ModTime() time.Time { return i.modTime }
func (i gcsInfo) IsDir() bool        { return i.dir }
func (i gcsInfo) Sys() any           { return nil }

func (i gcsInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o755
	}

	return 0o644
}

func isStatus(err error, code int) bool {
	var apiErr *googleapi.Error

	return errors.As(err, &apiErr) && apiErr.Code == code
}

// gcsError maps bucket errors onto fs errors, so writeFSError picks the status.
func gcsError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, storage.ErrObjectNotExist), errors.Is(err, storage.ErrBucketNotExist), isStatus(err, http.StatusNotFound):
		return fmt.Errorf("%w: %w", fs.ErrNotExist, err)
	case isStatus(err, http.StatusForbidden), isStatus(err, http.StatusUnauthorized):
		return fmt.Errorf("%w: %w", fs.ErrPermission, err)
	case isStatus(err, http.StatusPreconditionFailed):
		return fmt.Errorf("%w: %w", fs.ErrExist, err)
	default:
		return err
	}
}
