package live

import (
	"os"
	"time"
)

// fileStamp 是文件指纹：修改时间加大小。
//
// 只比修改时间会漏掉「同一时刻写入等长内容」的变化；多带一个大小字段
// 几乎不增加成本，却能挡住绝大多数漏判。
type fileStamp struct {
	modTime time.Time
	size    int64
	exists  bool
}

// readStamp 读取文件指纹。
//
// stat 失败一律按「不存在」处理：真正的错误留给加载过程去说明，
// 这里只负责回答「文件看起来变了没有」。
func readStamp(path string) fileStamp {
	info, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{modTime: info.ModTime(), size: info.Size(), exists: true}
}
