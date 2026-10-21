package storage

import (
	"context"
	"io"
)

// Store 只暴露业务需要的对象操作，S3 驱动可使用流式响应，无需实现本地文件的随机读取。
type Store interface {
	Put(context.Context, string, io.Reader) error
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}
