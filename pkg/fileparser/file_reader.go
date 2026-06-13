package fileparser

import (
	"io"
	"os"
)

// openFile 打开本地文件
func openFile(path string) (io.ReadCloser, error) {
	return os.Open(path)
}
