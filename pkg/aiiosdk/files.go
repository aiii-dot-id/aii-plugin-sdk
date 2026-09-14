package aiiosdk

import (
	"encoding/base64"
	"errors"
	"fmt"
)

// .
// .
// .
// .
// .
// .
// .
// .
// .
var Files FilesClient

// .
type FilesClient struct{}

// .
const PrivateRoot = "private"

// .
// .
type FileEntry struct {
	Name     string
	Dir      bool
	Size     int64
	Modified string
	Symlink  bool
}

func target(root, path string) map[string]any {
	return map[string]any{"root": root, "path": path}
}

// .
// .
func (FilesClient) List(root, path string) ([]FileEntry, bool, error) {
	res, err := InvokeCall("fs.list", target(root, path), nil)
	if err != nil {
		return nil, false, err
	}
	or := Object(res.OperationResult)
	truncated, _ := or.Bool("truncated")
	var out []FileEntry
	raw := or.Raw("entries")
	if len(raw) == 0 {
		return nil, truncated, nil
	}
	items, ok := ObjectArray(raw)
	if !ok {
		return nil, truncated, fmt.Errorf("aiiosdk: fs.list carries no entries array")
	}
	for _, e := range items {
		var fe FileEntry
		fe.Name, _ = e.String("name")
		fe.Dir, _ = e.Bool("dir")
		fe.Size, _ = e.Int("size")
		fe.Modified, _ = e.String("modified")
		fe.Symlink, _ = e.Bool("symlink")
		out = append(out, fe)
	}
	return out, truncated, nil
}

// .
// .
// .
func (FilesClient) Read(root, path string, offset int64, length int) (data []byte, eof bool, size int64, err error) {
	args := map[string]any{}
	if offset > 0 {
		args["offset"] = offset
	}
	if length > 0 {
		args["length"] = length
	}
	res, err := InvokeCall("fs.read", target(root, path), args)
	if err != nil {
		return nil, false, 0, err
	}
	or := Object(res.OperationResult)
	if b64, ok := or.String("data_b64"); ok {
		data, _ = base64.StdEncoding.DecodeString(b64)
	}
	eof, _ = or.Bool("eof")
	size, _ = or.Int("size")
	return data, eof, size, nil
}

// .
func (c FilesClient) ReadAll(root, path string, limit int) ([]byte, error) {
	if limit <= 0 {
		limit = 8 << 20
	}
	var out []byte
	var offset int64
	for {
		data, eof, _, err := c.Read(root, path, offset, 0)
		if err != nil {
			return out, err
		}
		out = append(out, data...)
		offset += int64(len(data))
		if len(out) > limit {
			return out, fmt.Errorf("aiiosdk: %s:%s exceeds the %d-byte limit this plugin set", root, path, limit)
		}
		if eof || len(data) == 0 {
			return out, nil
		}
	}
}

// .
// .
// .
func (FilesClient) Write(root, path string, data []byte) error {
	_, err := InvokeCall("fs.write", target(root, path), map[string]any{"data_b64": base64.StdEncoding.EncodeToString(data)})
	return err
}

// .
func (FilesClient) Append(root, path string, data []byte) error {
	_, err := InvokeCall("fs.write", target(root, path), map[string]any{"data_b64": base64.StdEncoding.EncodeToString(data), "append": true})
	return err
}

// .
// .
func (FilesClient) Delete(root, path string) (bool, error) {
	res, err := InvokeCall("fs.delete", target(root, path), nil)
	if err != nil {
		return false, err
	}
	deleted, _ := Object(res.OperationResult).Bool("deleted")
	return deleted, nil
}

// .
// .
// .
// .
// .
type PublishResult struct {
	Size       int64
	SHA256     string
	Replaced   bool
	Durable    bool
	Durability string
}

// .
// .
// .
// .
// .
// .
// .
func (FilesClient) Publish(root, path string, data []byte, expectedSHA256 string) (PublishResult, error) {
	args := map[string]any{"data_b64": base64.StdEncoding.EncodeToString(data)}
	if expectedSHA256 != "" {
		args["expected_sha256"] = expectedSHA256
	}
	return publish(root, path, args)
}

// .
func (FilesClient) PublishNew(root, path string, data []byte) (PublishResult, error) {
	return publish(root, path, map[string]any{"data_b64": base64.StdEncoding.EncodeToString(data), "expected_absent": true})
}

// .
// .
// .
// .
// .
// .
func (FilesClient) PublishStaged(root, path, stagedPath, sha256Hex, expectedSHA256 string) (PublishResult, error) {
	args := map[string]any{"from": stagedPath, "sha256": sha256Hex}
	if expectedSHA256 != "" {
		args["expected_sha256"] = expectedSHA256
	}
	return publish(root, path, args)
}

func publish(root, path string, args map[string]any) (PublishResult, error) {
	res, err := InvokeCall("fs.publish", target(root, path), args)
	if err != nil {
		return PublishResult{}, err
	}
	obj := Object(res.OperationResult)
	size, _ := obj.Int("size")
	sum, _ := obj.String("sha256")
	replaced, _ := obj.Bool("replaced")
	durable, _ := obj.Bool("durable")
	durability, _ := obj.String("durability")
	return PublishResult{Size: size, SHA256: sum, Replaced: replaced, Durable: durable, Durability: durability}, nil
}

// .
// .
// .
func (FilesClient) Digest(root, path string) (sha256Hex string, size int64, exists bool, err error) {
	res, err := InvokeCall("fs.read", target(root, path), map[string]any{"length": 1, "digest": true})
	if err != nil {
		var oe *OperationError
		if errors.As(err, &oe) && oe.Reason == "FS_NOT_FOUND" {
			return "", 0, false, nil
		}
		return "", 0, false, err
	}
	obj := Object(res.OperationResult)
	sum, _ := obj.String("sha256")
	size, _ = obj.Int("size")
	return sum, size, true, nil
}
